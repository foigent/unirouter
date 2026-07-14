package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F08ProbeLeaseContentionReturnsRetryable503WithoutDispatch(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalls++
	}))
	defer upstream.Close()

	now := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second)
	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, err := auth.EncryptKey([]byte("sk-probe"), encKey)
	if err != nil {
		t.Fatal(err)
	}
	db := &testutil.MockDB{
		QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return routePlanRows(routePlanRow{
				id: 1, name: "half-open", provider: "openai", baseURL: upstream.URL,
				priority: 10, weight: 100, channelStatus: "active", circuitState: "half_open",
				halfOpenUntil: &past, degradationThreshold: 2, keyID: 11,
				keySecret: encryptedKey, keyStatus: "active", dbNow: now,
			}), nil
		},
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
		},
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	policy := routingpolicy.Default()
	policy.CircuitBreakerHalfOpenPct = 100
	router, err := NewRouterWithPolicy(db, policy)
	if err != nil {
		t.Fatal(err)
	}
	router.now = func() time.Time { return now }
	cancelCalls := 0
	settleCalls := 0
	relay := NewRelay(db, nil, router, &mockBilling{
		freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
		},
		cancelFn: func(context.Context, string, int64) error {
			cancelCalls++
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
	}, nil, encKey)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-route-plan","messages":[{"role":"user","content":"hello"}]}`))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "op_00000000000000000000000000000008")
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, Status: "active"})
	rec := httptest.NewRecorder()
	relay.HandleChatCompletion(rec, req.WithContext(ctx))

	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("expected retryable 503, got status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}
	if upstreamCalls != 0 || cancelCalls != 1 || settleCalls != 0 {
		t.Fatalf("no-dispatch invariant failed: upstream=%d cancel=%d settle=%d", upstreamCalls, cancelCalls, settleCalls)
	}
}
