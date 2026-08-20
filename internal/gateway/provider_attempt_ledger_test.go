package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

func p6f40bChatRequest(requestID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`,
	))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, requestID)
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, Status: "active"})
	return req.WithContext(ctx)
}

func p6f40bRelayDB(upstreamURL, encryptedKey string) *testutil.MockDB {
	return &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				chatCompletionRouteRow(1, "p6f40b-channel", "openai", upstreamURL, encryptedKey, 1),
			}}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
}

func TestP6F40BProviderAttemptLedgerFailurePreventsDispatch(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, err := auth.EncryptKey([]byte("sk-p6f40b"), encKey)
	if err != nil {
		t.Fatalf("encrypt provider key: %v", err)
	}
	db := p6f40bRelayDB(upstream.URL, encryptedKey)
	beginCalls := 0
	cancelCalls := 0
	var attemptID string
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
		},
		beginAttemptFn: func(ctx context.Context, req billing.ProviderAttemptStart) error {
			beginCalls++
			if attemptID == "" {
				attemptID = req.AttemptID
			} else if req.AttemptID != attemptID {
				t.Fatalf("bounded ledger retries changed attempt identity: first=%q next=%q", attemptID, req.AttemptID)
			}
			return fmt.Errorf("ledger unavailable")
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalls++
			return nil
		},
	}
	relay := NewRelay(db, nil, NewRouter(db), b, nil, encKey)
	installLegacyHandlerTestRuntime(relay)
	recorder := httptest.NewRecorder()

	relay.HandleChatCompletion(recorder, p6f40bChatRequest("op_p6f40b_begin_fail"))

	if recorder.Code != http.StatusServiceUnavailable || upstreamCalls.Load() != 0 || beginCalls != billingSettleMaxAttempts || cancelCalls != 1 {
		t.Fatalf("pre-dispatch ledger barrier failed: status=%d upstream=%d begin=%d cancel=%d body=%s", recorder.Code, upstreamCalls.Load(), beginCalls, cancelCalls, recorder.Body.String())
	}
}

func TestP6F40BAcceptedResponseLedgerFailureWithholdsOutputWithoutCancel(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-p6f40b","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, err := auth.EncryptKey([]byte("sk-p6f40b"), encKey)
	if err != nil {
		t.Fatalf("encrypt provider key: %v", err)
	}
	db := p6f40bRelayDB(upstream.URL, encryptedKey)
	completeCalls := 0
	settleCalls := 0
	cancelCalls := 0
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
		},
		completeAttemptFn: func(ctx context.Context, req billing.ProviderAttemptCompletion) error {
			completeCalls++
			return fmt.Errorf("ledger completion unavailable")
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
	relay := NewRelay(db, nil, NewRouter(db), b, nil, encKey)
	installLegacyHandlerTestRuntime(relay)
	recorder := httptest.NewRecorder()

	relay.HandleChatCompletion(recorder, p6f40bChatRequest("op_p6f40b_complete_fail"))

	if recorder.Code != http.StatusInternalServerError || upstreamCalls.Load() != 1 || completeCalls != billingSettleMaxAttempts || settleCalls != 0 || cancelCalls != 0 {
		t.Fatalf("accepted-response ledger failure invariant failed: status=%d upstream=%d complete=%d settle=%d cancel=%d body=%s", recorder.Code, upstreamCalls.Load(), completeCalls, settleCalls, cancelCalls, recorder.Body.String())
	}
}
