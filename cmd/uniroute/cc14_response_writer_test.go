package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// CC14 observes status without hiding the real streaming writer's capability.
// Keep this independent of the non-streaming P3 fixture.
func cc14ObserveResponse(w http.ResponseWriter) (http.ResponseWriter, *cc14ResponseWriter) {
	observed := &cc14ResponseWriter{ResponseWriter: w, status: http.StatusOK}
	if flusher, ok := w.(http.Flusher); ok {
		return &cc14FlushingResponseWriter{cc14ResponseWriter: observed, flusher: flusher}, observed
	}
	return observed, observed
}

type cc14ResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *cc14ResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	if status >= 200 {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *cc14ResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *cc14ResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type cc14FlushingResponseWriter struct {
	*cc14ResponseWriter
	flusher http.Flusher
}

func (w *cc14FlushingResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.flusher.Flush()
}

// A plain interface embedding deliberately exposes no optional capabilities.
type cc14PlainResponseWriter struct{ http.ResponseWriter }

func TestCC14ResponseWriterPreservesOptionalFlusherAndStatus(t *testing.T) {
	underlying := httptest.NewRecorder()
	plain := &cc14PlainResponseWriter{ResponseWriter: underlying}
	writer, observed := cc14ObserveResponse(plain)
	if _, ok := writer.(http.Flusher); ok {
		t.Fatal("non-flushing writer gained a false Flusher capability")
	}
	if observed.Unwrap() != plain {
		t.Fatal("ResponseController unwrap lost underlying writer")
	}
	writer.Header().Set("X-Request-ID", "op_synthetic")
	writer.WriteHeader(http.StatusServiceUnavailable)
	writer.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(writer, "error"); err != nil {
		t.Fatal(err)
	}
	if observed.status != http.StatusServiceUnavailable || underlying.Code != observed.status || observed.Header().Get("X-Request-ID") != "op_synthetic" {
		t.Fatal("status or operation identity observation changed")
	}
	flushing := httptest.NewRecorder()
	writer, observed = cc14ObserveResponse(flushing)
	flusher, ok := writer.(http.Flusher)
	if !ok {
		t.Fatal("underlying Flusher was hidden")
	}
	flusher.Flush() // implicit 200 must become the observed final status.
	writer.WriteHeader(http.StatusInternalServerError)
	if !flushing.Flushed || observed.status != http.StatusOK || flushing.Code != http.StatusOK {
		t.Fatal("flush did not preserve the first final status")
	}
}

func TestCC14ResponseWriterStreamsBeforeHandlerReturnsAndReachesEOF(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			release := make(chan struct{})
			var releaseOnce sync.Once
			finish := func() { releaseOnce.Do(func() { close(release) }) }
			type result struct {
				status    int
				requestID string
				err       error
			}
			done := make(chan result, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writer, observed := cc14ObserveResponse(w)
				var handlerErr error
				defer func() { done <- result{observed.status, observed.Header().Get("X-Request-ID"), handlerErr} }()
				writer.Header().Set("Content-Type", "text/event-stream")
				writer.Header().Set("X-Request-ID", "op_cc14_synthetic")
				if status != http.StatusOK {
					writer.WriteHeader(status)
				}
				flusher, ok := writer.(http.Flusher)
				if !ok {
					handlerErr = fmt.Errorf("real HTTP Flusher was hidden")
					http.Error(writer, handlerErr.Error(), 500)
					return
				}
				if _, handlerErr = io.WriteString(writer, "data: first\n\n"); handlerErr != nil {
					return
				}
				flusher.Flush()
				select {
				case <-release:
				case <-r.Context().Done():
					handlerErr = r.Context().Err()
					return
				}
				_, handlerErr = io.WriteString(writer, "data: [DONE]\n\n")
			}))
			defer func() { finish(); server.Close() }()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != status {
				t.Fatalf("status=%d, want %d", response.StatusCode, status)
			}
			first := make([]byte, len("data: first\n\n"))
			if _, err := io.ReadFull(response.Body, first); err != nil {
				t.Fatalf("early flushed frame: %v", err)
			}
			if string(first) != "data: first\n\n" {
				t.Fatalf("first frame=%q", first)
			}
			select {
			case <-done:
				t.Fatal("handler returned before client released the completion barrier")
			default:
			}
			finish() // Only reading the first frame allows the server to write the tail and return.
			tail, err := io.ReadAll(response.Body)
			if err != nil || string(tail) != "data: [DONE]\n\n" {
				t.Fatalf("tail=%q error=%v", tail, err)
			}
			if n, err := response.Body.Read(make([]byte, 1)); n != 0 || err != io.EOF {
				t.Fatalf("terminal read=(%d,%v), want EOF", n, err)
			}
			got := <-done
			if got.err != nil || got.status != status || got.requestID != "op_cc14_synthetic" {
				t.Fatalf("observation=%+v", got)
			}
		})
	}
}
