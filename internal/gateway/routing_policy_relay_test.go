package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimeconfig"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestHandleChatCompletion_UsesEffectiveRetryBudgetAndTracePolicy(t *testing.T) {
	tests := []struct {
		name           string
		maxRetries     int
		expectedCalls  []int
		expectedStatus int
		expectedSettle int
		expectedCancel int
	}{
		{name: "no retry", maxRetries: 0, expectedCalls: []int{1, 0, 0}, expectedStatus: http.StatusBadGateway, expectedCancel: 1},
		{name: "one retry", maxRetries: 1, expectedCalls: []int{1, 1, 0}, expectedStatus: http.StatusBadGateway, expectedCancel: 1},
		{name: "two retries", maxRetries: 2, expectedCalls: []int{1, 1, 1}, expectedStatus: http.StatusOK, expectedSettle: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := make([]int, 3)
			servers := make([]*httptest.Server, 0, 3)
			for index := 0; index < 3; index++ {
				index := index
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls[index]++
					w.Header().Set("Content-Type", "application/json")
					if index < 2 {
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = w.Write([]byte(`{"error":{"message":"temporary"}}`))
						return
					}
					_, _ = w.Write([]byte(`{"id":"chatcmpl-policy","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
				}))
				servers = append(servers, server)
				defer server.Close()
			}

			encKey := []byte("0123456789abcdef01234567")
			encryptedKey, _ := auth.EncryptKey([]byte("sk-policy"), encKey)
			var routeMetadata map[string]interface{}
			db := &testutil.MockDB{
				QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return &testutil.MockRows{Rows: []func(dest ...any) error{
						chatCompletionRouteRow(1, "policy-primary", "openai", servers[0].URL, encryptedKey, 1),
						chatCompletionRouteRow(2, "policy-secondary", "azure", servers[1].URL, encryptedKey, 2),
						chatCompletionRouteRow(3, "policy-tertiary", "openai", servers[2].URL, encryptedKey, 3),
					}}, nil
				},
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*int) = 1
						return nil
					}}
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if routeMetadata == nil && strings.Contains(sql, "INSERT INTO request_trace_events") && len(args) == 39 && args[8] == "routing.route_selected" {
						if err := json.Unmarshal([]byte(args[17].(string)), &routeMetadata); err != nil {
							t.Fatalf("decode route metadata: %v", err)
						}
					}
					return pgconn.CommandTag{}, nil
				},
			}
			policy := routingpolicy.Default()
			policy.MaxRetryCount = test.maxRetries
			policy.CircuitBreakerThreshold = 9
			policy.CircuitBreakerRecoverSeconds = 77
			policy.CircuitBreakerHalfOpenPct = 33
			policy.RateLimitCooldownSeconds = 44
			policy.AuthFailureCooldownSeconds = 88
			policy.KeyFailureThreshold = 6
			router, err := NewRouterWithPolicy(db, policy)
			if err != nil {
				t.Fatalf("new router: %v", err)
			}

			settleCalls := 0
			cancelCalls := 0
			b := &mockBilling{
				freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
				},
				settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					settleCalls++
					return &billing.SettleResult{SettleID: 1, Success: true}, nil
				},
				cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
					cancelCalls++
					return nil
				},
			}
			relay := NewRelay(db, nil, router, b, nil, encKey)

			body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
			ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
			ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-routing-policy")
			ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, Status: "active"})
			rec := httptest.NewRecorder()

			relay.HandleChatCompletion(rec, req.WithContext(ctx))

			if rec.Code != test.expectedStatus || settleCalls != test.expectedSettle || cancelCalls != test.expectedCancel {
				t.Fatalf("unexpected terminal state: status=%d settle=%d cancel=%d body=%s", rec.Code, settleCalls, cancelCalls, rec.Body.String())
			}
			for index := range calls {
				if calls[index] != test.expectedCalls[index] {
					t.Fatalf("attempt %d calls=%d want=%d (all=%v)", index+1, calls[index], test.expectedCalls[index], calls)
				}
			}
			if routeMetadata["effective_config_version"] != runtimeconfig.Version || routeMetadata["routing_config_version"] != routingpolicy.Version ||
				routeMetadata["routing_policy_version"] != routingpolicy.SelectionVersion || routeMetadata["routing_snapshot_source"] != "live" ||
				routeMetadata["routing_snapshot_version"] == "" || routeMetadata["routing_candidate_count"] != float64(3) || routeMetadata["routing_excluded_count"] != float64(0) ||
				routeMetadata["routing_priority"] != float64(1) || routeMetadata["routing_weight"] != float64(100) || routeMetadata["routing_selection_reason"] != "priority_failover" ||
				routeMetadata["max_retry_count"] != float64(test.maxRetries) ||
				routeMetadata["circuit_breaker_threshold"] != float64(9) || routeMetadata["circuit_breaker_recover_seconds"] != float64(77) ||
				routeMetadata["circuit_breaker_half_open_percent"] != float64(33) || routeMetadata["rate_limit_cooldown_seconds"] != float64(44) ||
				routeMetadata["auth_failure_cooldown_seconds"] != float64(88) || routeMetadata["provider_key_failure_threshold"] != float64(6) {
				t.Fatalf("effective routing policy missing from trace: %#v", routeMetadata)
			}
		})
	}
}
