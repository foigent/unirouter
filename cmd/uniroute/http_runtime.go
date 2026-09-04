package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const runtimeCleanupGrace = 120 * time.Second

// httpRuntime owns only this process's accepted HTTP handlers. Ingress
// withdrawal and fleet compatibility are separate orchestration authorities.
type httpRuntime struct {
	mu              sync.Mutex
	instance        string
	draining        bool
	next            uint64
	requests        map[uint64]context.CancelFunc
	drained         chan struct{}
	requestLifetime time.Duration
	drainTimeout    time.Duration
	schemaDigest    string
}

type httpRuntimeStatus struct {
	Instance               string `json:"instance"`
	State                  string `json:"state"`
	ActiveRequests         int    `json:"active_requests"`
	RequestLifetimeSeconds int64  `json:"request_lifetime_seconds"`
	DrainTimeoutSeconds    int64  `json:"drain_timeout_seconds"`
	SchemaLedgerDigest     string `json:"schema_ledger_digest"`
}

func newHTTPRuntime(cfg *config.Config) (*httpRuntime, error) {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return nil, fmt.Errorf("runtime instance identity: %w", err)
	}
	// Keep the provider's shorter per-operation deadlines. This outer bound
	// includes admission/body handling and finalization, and cannot truncate a
	// currently implemented long reasoning/processing-tier contract.
	providerSeconds := max(300, cfg.StreamMaxDurationSecs)
	for _, profile := range modelprofile.All() {
		if reasoning, ok := modelprofile.ResolveReasoningContract(profile.ReasoningProfile); ok {
			providerSeconds = max(providerSeconds, reasoning.ActiveNonStreamTimeoutSeconds)
		}
		if tier, ok := modelprofile.ResolveProcessingTierPolicy(profile.ProcessingTierPolicy); ok {
			providerSeconds = max(providerSeconds, tier.OmittedTimeoutSeconds)
			for _, value := range tier.RequestValues {
				providerSeconds = max(providerSeconds, value.RequestTimeoutSeconds)
			}
		}
	}
	requestLifetime := time.Duration(providerSeconds)*time.Second + runtimeCleanupGrace
	return &httpRuntime{
		instance: "uri_" + hex.EncodeToString(identity[:]),
		requests: make(map[uint64]context.CancelFunc), drained: make(chan struct{}),
		requestLifetime: requestLifetime,
		drainTimeout:    max(requestLifetime+runtimeCleanupGrace, time.Duration(cfg.ShutdownTimeoutSecs)*time.Second),
	}, nil
}

func (runtime *httpRuntime) status() httpRuntimeStatus {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	state := "serving"
	if runtime.draining {
		state = "draining"
	}
	return httpRuntimeStatus{
		Instance: runtime.instance, State: state, ActiveRequests: len(runtime.requests),
		RequestLifetimeSeconds: int64(runtime.requestLifetime / time.Second),
		DrainTimeoutSeconds:    int64(runtime.drainTimeout / time.Second),
		SchemaLedgerDigest:     runtime.schemaDigest,
	}
}

func (runtime *httpRuntime) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health remains inspectable while accepted requests finish. Do not
		// exempt an arbitrary /health/ prefix or any mutation method.
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			(r.URL.Path == "/health/live" || r.URL.Path == "/health/ready") {
			next.ServeHTTP(w, r)
			return
		}
		runtime.mu.Lock()
		if runtime.draining {
			runtime.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Connection", "close")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"type":"server_error","code":"runtime_draining","message":"Runtime is draining"}}`))
			return
		}
		runtime.next++
		id := runtime.next
		ctx, cancel := context.WithTimeout(r.Context(), runtime.requestLifetime)
		runtime.requests[id] = cancel
		runtime.mu.Unlock()
		defer func() {
			cancel()
			runtime.mu.Lock()
			defer runtime.mu.Unlock()
			delete(runtime.requests, id)
			if runtime.draining && len(runtime.requests) == 0 {
				close(runtime.drained)
			}
		}()
		// Preserve the exact ResponseWriter, including Flusher and response
		// controller deadline support. No buffering goroutine outlives accounting.
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (runtime *httpRuntime) beginDrain() {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.draining {
		return
	}
	runtime.draining = true
	if len(runtime.requests) == 0 {
		close(runtime.drained)
	}
}

func (runtime *httpRuntime) wait(ctx context.Context) error {
	select {
	case <-runtime.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (runtime *httpRuntime) cancelRequests() {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	for _, cancel := range runtime.requests {
		cancel()
	}
}

// drainHTTP keeps dependency workers alive and health readable while handlers
// finish. The caller owns the remaining cleanup grace and stops dependencies
// only after this returns. Ingress must already have stopped forwarding here.
func (runtime *httpRuntime) drainHTTP(ctx context.Context, server *http.Server) error {
	runtime.beginDrain()
	deadline, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("HTTP drain requires a bounded deadline")
	}
	handlerCtx, cancel := context.WithDeadline(ctx, deadline.Add(-runtimeCleanupGrace))
	defer cancel()
	waitErr := runtime.wait(handlerCtx)
	if waitErr != nil {
		runtime.cancelRequests()
		closeErr := server.Close()
		// Closing sockets cancels ordinary HTTP operations. Await detached
		// finalization still owned by these handlers within the remaining grace.
		return errors.Join(fmt.Errorf("accepted request drain deadline: %w", waitErr), closeErr, runtime.wait(ctx))
	}
	return server.Shutdown(ctx)
}

func writeRuntimeHealth(w http.ResponseWriter, status int, state, reason string, runtime *httpRuntime) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"status": state, "runtime": runtime.status()}
	if reason != "" {
		body["error"] = reason
	} else if status == http.StatusOK {
		body["db"], body["redis"] = "ok", "ok"
	}
	_ = json.NewEncoder(w).Encode(body)
}
