package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/config"
)

func testHTTPRuntime(t *testing.T) *httpRuntime {
	t.Helper()
	runtime, err := newHTTPRuntime(&config.Config{StreamMaxDurationSecs: 1800, ShutdownTimeoutSecs: 120})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestHTTPRuntimeLifetimeCoversSupportedOperations(t *testing.T) {
	runtime := testHTTPRuntime(t)
	if runtime.requestLifetime != 1920*time.Second || runtime.drainTimeout != 2040*time.Second {
		t.Fatalf("unexpected default lifetime/drain: %s/%s", runtime.requestLifetime, runtime.drainTimeout)
	}
	long, err := newHTTPRuntime(&config.Config{StreamMaxDurationSecs: 3600, ShutdownTimeoutSecs: 120})
	if err != nil || long.requestLifetime != 3720*time.Second || long.drainTimeout != 3840*time.Second {
		t.Fatalf("long stream was truncated: %+v %v", long, err)
	}
	if runtime.instance == long.instance || !strings.HasPrefix(runtime.instance, "uri_") {
		t.Fatal("instance identity must be unique per process, not per artifact")
	}
}

func TestHTTPRuntimeDrainKeepsStreamAndFinalizationAlive(t *testing.T) {
	runtime := testHTTPRuntime(t)
	continueStream := make(chan struct{})
	finalizing := make(chan struct{})
	finishFinalization := make(chan struct{})
	var dispatches, settlements atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK
		if runtime.status().State == "draining" {
			status = http.StatusServiceUnavailable
		}
		w.WriteHeader(status)
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		<-continueStream
		if err := r.Context().Err(); err != nil {
			t.Errorf("drain canceled accepted stream: %v", err)
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		_ = http.NewResponseController(w).Flush()
		close(finalizing)
		<-finishFinalization
		settlements.Add(1)
	})
	server := httptest.NewServer(runtime.handler(mux))
	defer server.Close()
	response, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatalf("stream did not flush: %q %v", line, err)
	}
	runtime.beginDrain()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	drained := make(chan error, 1)
	go func() { drained <- runtime.wait(ctx) }()
	health, err := server.Client().Get(server.URL + "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	_ = health.Body.Close()
	if health.StatusCode != http.StatusServiceUnavailable {
		t.Fatal("draining process remained ready")
	}
	refused, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = refused.Body.Close()
	if refused.StatusCode != http.StatusServiceUnavailable || dispatches.Load() != 1 {
		t.Fatal("new request crossed admission fence")
	}
	close(continueStream)
	<-finalizing
	select {
	case err := <-drained:
		t.Fatalf("drain returned before handler finalization: %v", err)
	default:
	}
	close(finishFinalization)
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(rest), "data: [DONE]") {
		t.Fatalf("stream truncated: %q %v", rest, err)
	}
	if settlements.Load() != 1 || dispatches.Load() != 1 || runtime.status().ActiveRequests != 0 {
		t.Fatalf("handler responsibility leaked: %+v", runtime.status())
	}
	// Repeated signals must not panic or reopen admission.
	runtime.beginDrain()
}

func TestHTTPRuntimeFenceRaceAccountsAllAcceptedHandlers(t *testing.T) {
	runtime := testHTTPRuntime(t)
	var accepted atomic.Int32
	handler := runtime.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { accepted.Add(1) }))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		}()
	}
	close(start)
	runtime.beginDrain()
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.wait(ctx); err != nil || runtime.status().ActiveRequests != 0 {
		t.Fatalf("fence race leaked responsibility: %v %+v", err, runtime.status())
	}
	before := accepted.Load()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if accepted.Load() != before {
		t.Fatal("handler admitted after completed fence")
	}
}

func TestHTTPRuntimeRequestDeadlineAndForcedDrainAreExplicit(t *testing.T) {
	runtime := testHTTPRuntime(t)
	runtime.requestLifetime = 15 * time.Millisecond
	var observed error
	handler := runtime.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		observed = r.Context().Err()
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if !errors.Is(observed, context.DeadlineExceeded) {
		t.Fatalf("accepted lifetime has no deadline: %v", observed)
	}
	runtime.requestLifetime = time.Minute
	entered := make(chan struct{})
	finished := make(chan struct{})
	handler = runtime.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(finished)
	}))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	<-entered
	// A deadline shorter than the reserved cleanup grace deliberately exercises
	// forced drain. It must report failure even when cancellation then succeeds.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := runtime.drainHTTP(ctx, &http.Server{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("forced cancellation reported successful continuity: %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("forced drain did not cancel/wait for owned handler")
	}
}
