//go:build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/capbilling"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/policybilling"
	"github.com/uniroute/unirouter/internal/terminalhook"
	"github.com/uniroute/unirouter/internal/testutil"
)

type contentPerformancePhaseKey struct{}
type contentPerformancePhaseEvent struct {
	Name      string `json:"name"`
	ElapsedNS int64  `json:"elapsed_ns"`
	Result    string `json:"result"`
}
type contentPerformancePhase struct {
	native   *contentPerformanceNativeEntry
	mu       sync.Mutex
	start    time.Time
	marker   string
	events   []contentPerformancePhaseEvent
	overflow bool
}

// The original driver's Idempotency-Key already carries this public synthetic
// marker. Never read the body or retain arbitrary caller headers for timing.
func contentPerformanceMarker(raw string) string {
	parts := strings.Split(raw, "-")
	if len(parts) != 3 || len(parts[0]) != 2 || parts[0][0] != 'r' || parts[0][1] < '0' || parts[0][1] > '6' {
		return ""
	}
	maximum := 2999
	switch parts[1] {
	case "warmup":
		maximum = 19
	case "plain", "stream":
	default:
		return ""
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil || n < 0 || n > maximum || strconv.Itoa(n) != parts[2] {
		return ""
	}
	return raw
}

func (p *contentPerformancePhase) mark(name, result string) {
	if p == nil {
		return
	}
	switch name {
	case "handler_start", "handler_end", "request_cancelled", "authentication_complete", "authenticated_capture_start", "authenticated_capture_end", "provider_get_conn", "provider_got_conn", "context_connect_start", "context_connect_done", "provider_wrote_request", "provider_first_response_byte":
	case "billing_freeze_start", "billing_freeze_end", "core_billing_freeze_start", "core_billing_freeze_end",
		"policy_full_reserve_start", "policy_full_reserve_end", "policy_unscoped_rate_reserve_start", "policy_unscoped_rate_reserve_end",
		"policy_rate_reserve_start", "policy_rate_reserve_end", "policy_execution_reserve_start", "policy_execution_reserve_end",
		"policy_model_execution_reserve_start", "policy_model_execution_reserve_end", "policy_invalid_reserve_start", "policy_invalid_reserve_end",
		"terminal_hook_policy_register_start", "terminal_hook_policy_register_end", "terminal_hook_agent_register_start", "terminal_hook_agent_register_end",
		"terminal_hook_other_register_start", "terminal_hook_other_register_end":
	default:
		return
	}
	switch result {
	case "none", "context_cancelled", "deadline_exceeded", "other_redacted", "new_connection", "reused_connection":
	default:
		result = "other_redacted"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 64 {
		p.overflow = true
		return
	}
	p.events = append(p.events, contentPerformancePhaseEvent{name, time.Since(p.start).Nanoseconds(), result})
	p.native.mark(name, result)
}
func (p *contentPerformancePhase) snapshot() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := map[string]any{"marker": p.marker, "clock": "monotonic_since_handler_start", "events": append([]contentPerformancePhaseEvent{}, p.events...), "overflow": p.overflow}
	if p.native != nil {
		result["admission_decisions"] = p.native.decisions()
	}
	return result
}
func contentPerformancePhaseFrom(ctx context.Context) *contentPerformancePhase {
	p, _ := ctx.Value(contentPerformancePhaseKey{}).(*contentPerformancePhase)
	return p
}
func (p *contentPerformancePhase) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn: func(string) { p.mark("provider_get_conn", "none") },
		GotConn: func(info httptrace.GotConnInfo) {
			result := "new_connection"
			if info.Reused {
				result = "reused_connection"
			}
			p.mark("provider_got_conn", result)
		},
		// WithClientTrace also installs nettrace hooks used by any net.Dialer
		// inheriting this context (including DB/cache), not just HTTP.
		ConnectStart: func(string, string) { p.mark("context_connect_start", "none") },
		ConnectDone:  func(_, _ string, err error) { p.mark("context_connect_done", contentPerformanceErrorClass(err)) },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			p.mark("provider_wrote_request", contentPerformanceErrorClass(info.Err))
		},
		GotFirstResponseByte: func() { p.mark("provider_first_response_byte", "none") },
	}
}

// Embedding retains the existing optional capture interfaces and failure
// diagnostics. Only the authenticated capture gets coarse before/after marks.
type contentPerformanceCapture struct{ sourceCaptureDiagnostics }

func (c contentPerformanceCapture) CaptureAuthenticated(ctx context.Context, snapshot executioncontext.Snapshot) error {
	p := contentPerformancePhaseFrom(ctx)
	p.mark("authenticated_capture_start", "none")
	err := c.sourceCaptureDiagnostics.CaptureAuthenticated(ctx, snapshot)
	p.mark("authenticated_capture_end", contentPerformanceErrorClass(err))
	return err
}

func contentPerformanceObserveHTTP(requests *contentPerformanceRequests, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.GetRequestIDFromContext(r.Context())
		p := &contentPerformancePhase{start: time.Now(), marker: contentPerformanceMarker(r.Header.Get(middleware.IdempotencyKeyHeader))}
		requestCtx, entry := requests.native.begin(r.Context(), id, p.marker)
		p.native = entry
		p.mark("handler_start", "none")
		requests.mu.Lock()
		if requests.phases == nil {
			requests.phases = map[string]*contentPerformancePhase{}
		}
		if _, duplicate := requests.phases[id]; duplicate {
			requests.invalid = true
		} else {
			requests.phases[id] = p
		}
		requests.mu.Unlock()
		ctx := httptrace.WithClientTrace(context.WithValue(requestCtx, contentPerformancePhaseKey{}, p), p.trace())
		cancelObserved := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			defer close(cancelObserved)
			p.mark("request_cancelled", contentPerformanceErrorClass(ctx.Err()))
		})
		capture := &contentPerformanceResponse{ResponseWriter: w}
		defer func() {
			// Join a started callback before the census can be serialized. Stopping
			// an unstarted callback avoids reporting normal server teardown as a
			// cancellation during the measured handler.
			if !stop() {
				<-cancelObserved
			} else if ctx.Err() != nil {
				p.mark("request_cancelled", contentPerformanceErrorClass(ctx.Err()))
			}
			p.mark("handler_end", contentPerformanceErrorClass(ctx.Err()))
			written := capture.status != 0
			if !written {
				capture.status = http.StatusOK
			} // Original finance classification.
			requests.recordHTTP(id, capture.status, written)
		}()
		next.ServeHTTP(contentPerformanceWrapResponse(capture), r.WithContext(ctx))
	})
}

func TestContentPerformancePhasesOffline(t *testing.T) {
	t.Run("marker_whitelist", func(t *testing.T) {
		for _, marker := range []string{"r0-warmup-0", "r6-warmup-19", "r0-plain-2999", "r5-stream-123"} {
			require.Equal(t, marker, contentPerformanceMarker(marker))
		}
		for _, marker := range []string{"secret\ncredential=value", "r0-plain-3000", "r0-warmup-20", "r7-plain-1", "r0-plain-01", "r0-plain-+1", "r0-plain--1", "r0-other-0", "r0-plain-1|secret"} {
			require.Empty(t, contentPerformanceMarker(marker))
		}
	})
	t.Run("cancel_without_headers_retains_join_and_finance_classification", func(t *testing.T) {
		requests := &contentPerformanceRequests{seen: map[string]bool{}, http: map[string]int{}}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		handler := middleware.OperationIdentityWithSecret(prd2E2EJWTSecret)(contentPerformanceObserveHTTP(requests, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			p := contentPerformancePhaseFrom(r.Context())
			p.mark("authentication_complete", "none")
			requests.record(middleware.GetRequestIDFromContext(r.Context()), true)
			cancel()
			<-r.Context().Done()
		})))
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
		r.Header.Set(middleware.IdempotencyKeyHeader, "r0-plain-185")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		ids, err := requests.snapshot()
		require.NoError(t, err)
		require.Len(t, ids, 1)
		row := requests.evidence()[0]
		require.Equal(t, ids[0], row["operation_id"])
		require.Equal(t, 200, row["http_status"])
		require.Equal(t, false, row["final_header_written"])
		timing := row["timing"].(map[string]any)
		require.Equal(t, "r0-plain-185", timing["marker"])
		events := timing["events"].([]contentPerformancePhaseEvent)
		require.Equal(t, "handler_start", events[0].Name)
		require.Equal(t, "handler_end", events[len(events)-1].Name)
		// The cancellation callback can start before or after next returns.
		// A completed handler still exposes its cancelled context either way.
		require.Equal(t, "context_cancelled", events[len(events)-1].Result)
		foundCancellation := false
		for _, event := range events {
			if event.Name == "request_cancelled" {
				foundCancellation = true
			}
		}
		require.True(t, foundCancellation)
		for i := 1; i < len(events); i++ {
			require.GreaterOrEqual(t, events[i].ElapsedNS, events[i-1].ElapsedNS)
		}
	})
	t.Run("real_httptrace_preserves_client_and_context", func(t *testing.T) {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "synthetic") }))
		defer provider.Close()
		requests := &contentPerformanceRequests{seen: map[string]bool{}, http: map[string]int{}}
		client := provider.Client()
		client.Timeout = time.Second
		defer client.CloseIdleConnections()
		handler := middleware.OperationIdentityWithSecret(prd2E2EJWTSecret)(contentPerformanceObserveHTTP(requests, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			outbound, err := http.NewRequestWithContext(r.Context(), http.MethodGet, provider.URL, nil)
			require.NoError(t, err)
			response, err := client.Do(outbound.Clone(outbound.Context()))
			require.NoError(t, err)
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			w.WriteHeader(204)
		})))
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		r.Header.Set(middleware.IdempotencyKeyHeader, "r0-plain-1")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		row := requests.evidence()[0]
		require.Equal(t, true, row["final_header_written"])
		require.Equal(t, 204, row["http_status"])
		events := row["timing"].(map[string]any)["events"].([]contentPerformancePhaseEvent)
		names := []string{}
		for _, event := range events {
			names = append(names, event.Name)
		}
		for _, name := range []string{"provider_get_conn", "provider_got_conn", "provider_wrote_request", "provider_first_response_byte"} {
			require.Contains(t, names, name)
		}
		require.NotContains(t, names, "request_cancelled")
		require.Equal(t, time.Second, client.Timeout)
	})
	t.Run("concurrent_callbacks_are_bounded_and_redacted", func(t *testing.T) {
		p := &contentPerformancePhase{start: time.Now()}
		trace := p.trace()
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				trace.ConnectDone("secret-network", "secret-address", errors.New("secret-error"))
			}()
		}
		wg.Wait()
		p.mark("secret-phase", "secret-result")
		snapshot := p.snapshot()
		require.Equal(t, true, snapshot["overflow"])
		events := snapshot["events"].([]contentPerformancePhaseEvent)
		require.Len(t, events, 64)
		for i := 1; i < len(events); i++ {
			require.GreaterOrEqual(t, events[i].ElapsedNS, events[i-1].ElapsedNS)
		}
		wire, err := json.Marshal(snapshot)
		require.NoError(t, err)
		require.NotContains(t, string(wire), "secret")
	})
}

// Embed concrete services, retaining ALL exported methods, including optional
// rate-admission and TerminalStateTTL interfaces. Observe only synchronous calls;
// do not replace contexts, create owners, retry, or infer success from nil error.
type contentPerformanceBilling struct{ *policybilling.Billing }
type contentPerformanceCoreBilling struct{ *billing.Billing }
type contentPerformancePolicyRuntime struct{ *limitpolicy.Service }
type contentPerformanceTerminalHooks struct{ *terminalhook.Service }

var (
	_ gateway.BillingProvider     = contentPerformanceBilling{}
	_ capbilling.Provider         = contentPerformanceCoreBilling{}
	_ policybilling.Runtime       = contentPerformancePolicyRuntime{}
	_ policybilling.TerminalHooks = contentPerformanceTerminalHooks{}
	_ capbilling.TerminalHooks    = contentPerformanceTerminalHooks{}
)

func (b contentPerformanceBilling) Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
	// Relay calls Freeze immediately after recordPayloadPolicyDecision returns.
	// This is the next observable boundary, not an outbox-commit timestamp:
	// the concrete outbox service has no transparent test wrapper interface.
	return contentPerformanceTimedFreeze(ctx, req, "billing_freeze", b.Billing.Freeze)
}

func (b contentPerformanceCoreBilling) Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
	return contentPerformanceTimedFreeze(ctx, req, "core_billing_freeze", b.Billing.Freeze)
}

func contentPerformanceTimedFreeze(ctx context.Context, req billing.FreezeRequest, prefix string, call func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error)) (*billing.FreezeResult, error) {
	p := contentPerformancePhaseFrom(ctx)
	p.mark(prefix+"_start", "none")
	result, err := call(ctx, req)
	p.mark(prefix+"_end", contentPerformanceErrorClass(err))
	return result, err
}

func (s contentPerformancePolicyRuntime) Reserve(ctx context.Context, req limitpolicy.AdmissionRequest) (*limitpolicy.Reservation, error) {
	prefix := "policy_invalid_reserve"
	switch req.Phase {
	case "", limitpolicy.AdmissionPhaseFull:
		prefix = "policy_full_reserve"
	case limitpolicy.AdmissionPhaseUnscopedRate:
		prefix = "policy_unscoped_rate_reserve"
	case limitpolicy.AdmissionPhaseRate:
		prefix = "policy_rate_reserve"
	case limitpolicy.AdmissionPhaseExecution:
		prefix = "policy_execution_reserve"
	case limitpolicy.AdmissionPhaseModelExecution:
		prefix = "policy_model_execution_reserve"
	}
	p := contentPerformancePhaseFrom(ctx)
	p.mark(prefix+"_start", "none")
	if p != nil {
		p.native.bind(req.Subject.OrgID)
	}
	result, err := s.Service.Reserve(ctx, req)
	if p != nil {
		p.native.collect(ctx)
	}
	p.mark(prefix+"_end", contentPerformanceErrorClass(err))
	return result, err
}

func (s contentPerformanceTerminalHooks) Register(ctx context.Context, hookType, operationID string, orgID int64, reservation any, expiresAt time.Time) error {
	prefix := "terminal_hook_other_register"
	switch hookType {
	case terminalhook.HookLimitPolicy:
		prefix = "terminal_hook_policy_register"
	case terminalhook.HookAgentAncestor:
		prefix = "terminal_hook_agent_register"
	}
	p := contentPerformancePhaseFrom(ctx)
	p.mark(prefix+"_start", "none")
	err := s.Service.Register(ctx, hookType, operationID, orgID, reservation, expiresAt)
	p.mark(prefix+"_end", contentPerformanceErrorClass(err))
	return err
}

func TestContentPerformanceFreezePhasesOffline(t *testing.T) {
	t.Run("all_concrete_method_sets_retained", func(t *testing.T) {
		for _, pair := range [][2]any{
			{(*policybilling.Billing)(nil), contentPerformanceBilling{}},
			{(*billing.Billing)(nil), contentPerformanceCoreBilling{}},
			{(*limitpolicy.Service)(nil), contentPerformancePolicyRuntime{}},
			{(*terminalhook.Service)(nil), contentPerformanceTerminalHooks{}},
		} {
			original, wrapped := reflect.TypeOf(pair[0]), reflect.TypeOf(pair[1])
			for i := 0; i < original.NumMethod(); i++ {
				method := original.Method(i)
				actual, ok := wrapped.MethodByName(method.Name)
				require.True(t, ok, "%v lost %s", wrapped, method.Name)
				require.Equal(t, method.Type.NumIn(), actual.Type.NumIn(), method.Name)
				require.Equal(t, method.Type.NumOut(), actual.Type.NumOut(), method.Name)
				for j := 1; j < method.Type.NumIn(); j++ {
					require.Equal(t, method.Type.In(j), actual.Type.In(j), method.Name)
				}
				for j := 0; j < method.Type.NumOut(); j++ {
					require.Equal(t, method.Type.Out(j), actual.Type.Out(j), method.Name)
				}
			}
		}
	})
	newPhase := func() (*contentPerformancePhase, context.Context) {
		p := &contentPerformancePhase{start: time.Now(), marker: "r0-plain-17"}
		return p, context.WithValue(t.Context(), contentPerformancePhaseKey{}, p)
	}
	assertPair := func(p *contentPerformancePhase, prefix, result string) {
		t.Helper()
		events := p.snapshot()["events"].([]contentPerformancePhaseEvent)
		require.Len(t, events, 2)
		require.Equal(t, prefix+"_start", events[0].Name)
		require.Equal(t, prefix+"_end", events[1].Name)
		require.Equal(t, result, events[1].Result)
		require.GreaterOrEqual(t, events[1].ElapsedNS, events[0].ElapsedNS)
	}
	for _, scenario := range []string{"success", "denial", "result_and_error", "cancelled", "without_phase"} {
		t.Run("freeze_transparency_"+scenario, func(t *testing.T) {
			p, ctx := newPhase()
			if scenario == "without_phase" {
				ctx = t.Context()
			}
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			var fault error
			if scenario == "result_and_error" {
				fault = errors.New("private backend failure")
			}
			if scenario == "cancelled" {
				cancel()
				fault = ctx.Err()
			}
			expected := &billing.FreezeResult{Success: scenario == "success", CalculatedCostMicros: 1234, ErrorCode: "unchanged"}
			req := billing.FreezeRequest{RequestID: "operation", OrgID: 7, APIKeyID: 13, Model: "company-chat", EstimatedInputTokens: 99, ExecutionSnapshotHash: "exact-input"}
			for _, prefix := range []string{"billing_freeze", "core_billing_freeze"} {
				p.events = nil
				calls := 0
				result, err := contentPerformanceTimedFreeze(ctx, req, prefix, func(actual context.Context, actualReq billing.FreezeRequest) (*billing.FreezeResult, error) {
					require.Same(t, ctx, actual)
					require.Equal(t, req, actualReq)
					if scenario != "without_phase" {
						require.Len(t, p.snapshot()["events"], 1)
					}
					calls++
					return expected, fault
				})
				require.Equal(t, 1, calls)
				require.Same(t, expected, result)
				if fault == nil {
					require.NoError(t, err)
				} else {
					require.Same(t, fault, err)
				}
				if scenario == "without_phase" {
					require.Empty(t, p.events)
				} else {
					assertPair(p, prefix, contentPerformanceErrorClass(fault))
				}
				wire, err := json.Marshal(p.snapshot())
				require.NoError(t, err)
				require.NotContains(t, string(wire), "private backend")
			}
		})
	}
	t.Run("core_calls_original_service", func(t *testing.T) {
		p, ctx := newPhase()
		core := billing.NewBilling(nil, nil)
		result, err := (contentPerformanceCoreBilling{core}).Freeze(ctx, billing.FreezeRequest{})
		require.Nil(t, result)
		require.EqualError(t, err, "billing database is unavailable")
		assertPair(p, "core_billing_freeze", "other_redacted")
	})
	t.Run("outer_denial_remains_nil_error_denial", func(t *testing.T) {
		p, ctx := newPhase()
		shared := policybilling.New(nil, nil)
		original, originalErr := shared.Freeze(ctx, billing.FreezeRequest{})
		result, err := (contentPerformanceBilling{shared}).Freeze(ctx, billing.FreezeRequest{})
		require.Equal(t, original, result)
		require.Equal(t, originalErr, err)
		require.False(t, result.Success)
		assertPair(p, "billing_freeze", "none")
	})
	for _, scenario := range []struct{ phase, prefix string }{
		{"", "full"}, {limitpolicy.AdmissionPhaseFull, "full"}, {limitpolicy.AdmissionPhaseUnscopedRate, "unscoped_rate"},
		{limitpolicy.AdmissionPhaseRate, "rate"}, {limitpolicy.AdmissionPhaseExecution, "execution"},
		{limitpolicy.AdmissionPhaseModelExecution, "model_execution"}, {"private invalid phase", "invalid"},
	} {
		t.Run("reserve_"+scenario.phase, func(t *testing.T) {
			p, ctx := newPhase()
			service := &limitpolicy.Service{}
			req := limitpolicy.AdmissionRequest{Phase: scenario.phase}
			original, originalErr := service.Reserve(ctx, req)
			result, err := (contentPerformancePolicyRuntime{service}).Reserve(ctx, req)
			require.Same(t, originalErr, err)
			require.Equal(t, original, result)
			assertPair(p, "policy_"+scenario.prefix+"_reserve", "other_redacted")
		})
	}
	for _, hookType := range []string{terminalhook.HookLimitPolicy, terminalhook.HookAgentAncestor} {
		t.Run("hook_"+hookType, func(t *testing.T) {
			p, ctx := newPhase()
			fault := errors.New("private register failure")
			expires := time.Now().Add(time.Minute)
			calls, rollbacks := 0, 0
			tx := &testutil.MockTx{ExecFn: func(actual context.Context, query string, args ...any) (pgconn.CommandTag, error) {
				require.Same(t, ctx, actual)
				require.Equal(t, []any{"operation", int64(7), hookType}, args[:3])
				require.JSONEq(t, `{"value":3}`, args[3].(string))
				require.Equal(t, expires.UTC(), args[5])
				calls++
				return pgconn.CommandTag{}, fault
			}, RollbackFn: func(actual context.Context) error { require.Same(t, ctx, actual); rollbacks++; return nil }}
			pool := &testutil.MockDB{BeginFn: func(actual context.Context) (pgx.Tx, error) { require.Same(t, ctx, actual); return tx, nil }}
			service, err := terminalhook.New(pool, &outbox.Service{})
			require.NoError(t, err)
			err = (contentPerformanceTerminalHooks{service}).Register(ctx, hookType, "operation", 7, map[string]int{"value": 3}, expires)
			require.ErrorIs(t, err, fault)
			require.Equal(t, 1, calls)
			require.Equal(t, 1, rollbacks)
			prefix := "terminal_hook_policy_register"
			if hookType == terminalhook.HookAgentAncestor {
				prefix = "terminal_hook_agent_register"
			}
			assertPair(p, prefix, "other_redacted")
		})
	}
}
