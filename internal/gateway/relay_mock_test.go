package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

// --- Mock BillingProvider ---

type mockBilling struct {
	queryModelPriceFn func(ctx context.Context, orgID int64, model string) (float64, float64, error)
	calculateCostFn   func(ctx context.Context, model string, promptTokens, completionTokens int) (int, error)
	freezeFn          func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error)
	settleFn          func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error)
	cancelFn          func(ctx context.Context, requestID string, orgID int64) error
}

func (m *mockBilling) QueryModelPriceForOrg(ctx context.Context, orgID int64, model string) (float64, float64, error) {
	if m.queryModelPriceFn != nil {
		return m.queryModelPriceFn(ctx, orgID, model)
	}
	return 2, 10, nil
}

func (m *mockBilling) CalculateCost(ctx context.Context, model string, promptTokens, completionTokens int) (int, error) {
	if m.calculateCostFn != nil {
		return m.calculateCostFn(ctx, model, promptTokens, completionTokens)
	}
	return 100, nil
}

func (m *mockBilling) Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
	if m.freezeFn != nil {
		return m.freezeFn(ctx, req)
	}
	return &billing.FreezeResult{FreezeID: 1, Amount: 100, NewBalance: 900, Success: true}, nil
}

func (m *mockBilling) Settle(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
	if m.settleFn != nil {
		return m.settleFn(ctx, req)
	}
	return &billing.SettleResult{SettleID: 1, Refund: 10, NewBalance: 910, Success: true}, nil
}

func (m *mockBilling) Cancel(ctx context.Context, requestID string, orgID int64) error {
	if m.cancelFn != nil {
		return m.cancelFn(ctx, requestID, orgID)
	}
	return nil
}

// --- Helper to create Relay with mocks ---

func newTestRelay(db *testutil.MockDB, b *mockBilling) *Relay {
	return &Relay{
		db:                   db,
		router:               NewRouter(db),
		billing:              b,
		traceEventsEnabled:   true,
		traceEventSampleRate: 1,
	}
}

// --- HandleModels tests ---

func TestHandleModels_WithAllowedModels(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "gpt-4o,gpt-3.5-turbo"
					return nil
				},
			}
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(data))
	}
}

func TestHandleModels_FallbackToModelPrices(t *testing.T) {
	callCount := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					callCount++
					return fmt.Errorf("no key found")
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "gpt-4o"
						return nil
					},
					func(dest ...any) error {
						*dest[0].(*string) = "claude-3"
						return nil
					},
				},
			}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(data))
	}
}

func TestHandleModels_FallbackQueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("no key")
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleModels(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleModels_EmptyAllowedModels(t *testing.T) {
	// When allowed_models is empty, falls back to model_prices
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "" // empty allowed_models
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "gpt-4o"
						return nil
					},
				},
			}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- calculateCost tests ---

func TestCalculateCost_Success(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (float64, float64, error) {
			return 5, 15, nil // 5 cents per 1K input, 15 cents per 1K output
		},
	})

	cost, err := relay.calculateCost(context.Background(), 1, "gpt-4o", 1000, 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// cost = (1000 * 5 + 500 * 15) / 1000 = (5000 + 7500) / 1000 = 12.5 → 12
	if cost != 12 {
		t.Fatalf("expected cost=12, got %d", cost)
	}
}

func TestCalculateCost_MinimumCost(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (float64, float64, error) {
			return 1, 1, nil
		},
	})

	cost, err := relay.calculateCost(context.Background(), 1, "gpt-4o", 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// cost = (1*1 + 1*1) / 1000 = 0.002 → min 1
	if cost != 1 {
		t.Fatalf("expected min cost=1, got %d", cost)
	}
}

func TestCalculateCost_ReturnsErrorOnPriceLookupFailure(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (float64, float64, error) {
			return 0, 0, fmt.Errorf("db error")
		},
	})

	cost, err := relay.calculateCost(context.Background(), 1, "gpt-4o", 1000, 500)
	if err == nil {
		t.Fatal("expected price lookup error")
	}
	if cost != 0 {
		t.Fatalf("expected cost=0 on error, got %d", cost)
	}
}

// --- recordRequest tests ---

func TestRecordRequest_Success(t *testing.T) {
	execCalled := false
	var capturedArgs []any
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO call_logs") {
				return pgconn.CommandTag{}, nil
			}
			execCalled = true
			capturedArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	ownerUserID := int64(42)
	ctx := context.WithValue(context.Background(), middleware.CtxProjectID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{OwnerUserID: &ownerUserID, KeyType: "personal"})
	relay.recordRequest(ctx, "req-001", 1, 1, 1, "openai", "ch1", "gpt-4o", "gpt-4o", "127.0.0.1", 100, 50, 10, 200, 123, 0, false, metering.UsageSourceUpstream, 180)

	if !execCalled {
		t.Fatal("expected exec to be called")
	}
	if len(capturedArgs) != 22 {
		t.Fatalf("expected 22 insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[2] != int64(7) {
		t.Fatalf("expected project_id arg 7, got %v", capturedArgs[2])
	}
	if capturedArgs[4] != ownerUserID {
		t.Fatalf("expected owner_user_id arg 42, got %v", capturedArgs[4])
	}
	if capturedArgs[5] != "personal" {
		t.Fatalf("expected key_type arg personal, got %v", capturedArgs[5])
	}
	if capturedArgs[15] != 123 {
		t.Fatalf("expected latency_ms arg 123, got %v", capturedArgs[15])
	}
	if capturedArgs[20] != 180 {
		t.Fatalf("expected estimated_tokens arg 180, got %v", capturedArgs[20])
	}
	if capturedArgs[21] != 1.0 {
		t.Fatalf("expected upstream usage confidence 1.0, got %v", capturedArgs[21])
	}
}

func TestRecordTraceEvent_SanitizesMetadataAndSnapshotsOwner(t *testing.T) {
	var capturedArgs []any
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO request_trace_events") {
				return pgconn.CommandTag{}, nil
			}
			capturedArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	ownerUserID := int64(42)
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxProjectID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(9))
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{OwnerUserID: &ownerUserID, KeyType: "personal"})
	route := &RouteResult{
		Channel:     models.Channel{Provider: "openai", Name: "primary"},
		ChannelKey:  models.ChannelKey{ID: 11},
		MappedModel: "gpt-4o",
	}
	relay.recordTraceEvent(ctx, "req-trace", "provider.response_accepted", "provider", "info", "company-chat", "company-chat", route, http.StatusOK, 25, map[string]interface{}{
		"estimated_tokens": 100,
		"api_key":          "sk-secret",
		"request_body":     `{"messages":[{"content":"secret"}]}`,
	})

	if len(capturedArgs) != 16 {
		t.Fatalf("expected 16 trace insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[2] != int64(7) || capturedArgs[3] != int64(9) || capturedArgs[4] != ownerUserID || capturedArgs[5] != "personal" {
		t.Fatalf("unexpected project/key/owner args: %#v", capturedArgs[2:6])
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(capturedArgs[15].(string)), &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["estimated_tokens"] != float64(100) {
		t.Fatalf("expected estimated_tokens metadata, got %#v", metadata)
	}
	if _, ok := metadata["api_key"]; ok {
		t.Fatalf("metadata leaked api_key: %#v", metadata)
	}
	if _, ok := metadata["request_body"]; ok {
		t.Fatalf("metadata leaked request_body: %#v", metadata)
	}
}

func TestRecordRequest_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	// Should not panic on error (just logs)
	relay.recordRequest(context.Background(), "req-001", 1, 1, 1, "openai", "ch1", "gpt-4o", "gpt-4o", "127.0.0.1", 100, 50, 10, 200, 123, 0, false, metering.UsageSourceUpstream, 180)
}

// --- checkBudgetAlert tests ---

func TestCheckBudgetAlert_NilAlertSvc(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})
	relay.alertSvc = nil

	// Should not panic
	relay.checkBudgetAlert(context.Background())
}

// --- SetAlertService tests ---

func TestSetAlertService(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})
	relay.SetAlertService(nil, "https://webhook.example.com", 5000)

	if relay.webhookURL != "https://webhook.example.com" {
		t.Fatalf("expected webhookURL, got %s", relay.webhookURL)
	}
	if relay.alertThresh != 5000 {
		t.Fatalf("expected alertThresh=5000, got %d", relay.alertThresh)
	}
}

func TestSetTraceEvents(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})

	relay.SetTraceEvents(false, 0.25)
	if relay.shouldRecordTraceEvent("req-001") {
		t.Fatal("expected trace recording disabled")
	}

	relay.SetTraceEvents(true, 1.5)
	if relay.traceEventSampleRate != 1 {
		t.Fatalf("expected sample rate capped to 1, got %f", relay.traceEventSampleRate)
	}
	if !relay.shouldRecordTraceEvent("req-001") {
		t.Fatal("expected trace recording enabled at full sample rate")
	}

	relay.SetTraceEvents(true, -1)
	if relay.traceEventSampleRate != 0 {
		t.Fatalf("expected negative sample rate capped to 0, got %f", relay.traceEventSampleRate)
	}
	if relay.shouldRecordTraceEvent("req-001") {
		t.Fatal("expected sample rate 0 to skip recording")
	}
}

// --- NewRelay tests ---

func TestNewRelay(t *testing.T) {
	db := &testutil.MockDB{}
	b := &mockBilling{}
	relay := NewRelay(db, nil, nil, b, nil, []byte("0123456789abcdef01234567"))
	if relay == nil {
		t.Fatal("NewRelay should not return nil")
	}
}

// --- HandleChatCompletion basic tests ---

func TestHandleChatCompletion_NoModel(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})

	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletion_NoMessages(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, Amount: 100, NewBalance: 900, Success: true}, nil
		},
	})

	body := `{"model":"gpt-4o"}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	// No messages is valid (handler doesn't check), but no route → 503
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletion_InvalidJSON(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletion_NoRoute(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil // no routes
		},
	}
	relay := newTestRelay(db, &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, Amount: 100, NewBalance: 900, Success: true}, nil
		},
	})

	body := `{"model":"nonexistent","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletion_FreezeInsufficientBalance(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{Success: false, ErrorCode: "insufficient_balance", ErrorDetail: "balance too low"}, nil
		},
	})

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletion_FreezeError(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return nil, fmt.Errorf("billing service down")
		},
	})

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- HandleEmbeddings basic tests ---

func TestHandleEmbeddings_NoModel(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})

	body := `{"input":"hello world"}`
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleEmbeddings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleEmbeddings_InvalidJSON(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})

	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleEmbeddings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleEmbeddings_FreezeInsufficientBalance(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{Success: false, ErrorCode: "insufficient_balance"}, nil
		},
	})

	body := `{"model":"text-embedding-3-small","input":"hello world"}`
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleEmbeddings(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleEmbeddings_NoRoute(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, Amount: 100, NewBalance: 900, Success: true}, nil
		},
	})

	body := `{"model":"text-embedding-3-small","input":"hello world"}`
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleEmbeddings(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Suppress unused import
var _ = pgtype.Int4{}

// --- relayNonStreamRequest tests ---

func TestRelayNonStreamRequest_Success(t *testing.T) {
	settleCalled := false
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (float64, float64, error) {
			return 5, 15, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalled = true
			return &billing.SettleResult{SettleID: 1, Refund: 5, NewBalance: 995, Success: true}, nil
		},
	}
	relay := newTestRelay(db, b)

	// Mock upstream response
	respBody := `{"id":"chatcmpl-123","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"Hi!"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"},
	}

	rec := httptest.NewRecorder()
	relay.relayNonStreamRequest(context.Background(), rec, resp, route, "req-001", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{RequestID: "req-001", OrgID: 1}), time.Now(), nil, 1200)

	if !settleCalled {
		t.Fatal("expected settle to be called")
	}
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Check _uniroute extension field
	var resp2 map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp2)
	if _, ok := resp2["_uniroute"]; !ok {
		t.Fatal("expected _uniroute in response")
	}
}

func TestRelayNonStreamRequest_SettleRetrySuccess(t *testing.T) {
	settleCalls := 0
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			if settleCalls < billingSettleMaxAttempts {
				return nil, fmt.Errorf("temporary settle failure")
			}
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			t.Fatal("cancel should not be called after retry success")
			return nil
		},
	}
	relay := newTestRelay(db, b)

	respBody := `{"usage":{"prompt_tokens":10,"completion_tokens":5}}`
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Header:     http.Header{},
	}
	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"},
	}

	rec := httptest.NewRecorder()
	relay.relayNonStreamRequest(context.Background(), rec, resp, route, "req-retry", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{RequestID: "req-retry", OrgID: 1}), time.Now(), nil, 1200)

	if settleCalls != billingSettleMaxAttempts {
		t.Fatalf("expected %d settle attempts, got %d", billingSettleMaxAttempts, settleCalls)
	}
	if rec.Code != 200 {
		t.Fatalf("expected 200 after retry success, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRelayNonStreamRequest_SettleError(t *testing.T) {
	settleCalls := 0
	cancelCalled := false
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return nil, fmt.Errorf("settle failed")
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalled = true
			if requestID != "req-002" || orgID != 1 {
				t.Fatalf("unexpected cancel args request_id=%s org_id=%d", requestID, orgID)
			}
			return nil
		},
	}
	relay := newTestRelay(db, b)

	respBody := `{"usage":{"prompt_tokens":10,"completion_tokens":5}}`
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Header:     http.Header{},
	}

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"},
	}

	rec := httptest.NewRecorder()
	relay.relayNonStreamRequest(context.Background(), rec, resp, route, "req-002", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{RequestID: "req-002", OrgID: 1}), time.Now(), nil, 1200)

	if settleCalls != billingSettleMaxAttempts {
		t.Fatalf("expected %d settle attempts, got %d", billingSettleMaxAttempts, settleCalls)
	}
	if !cancelCalled {
		t.Fatal("expected cancel to release frozen amount after settle failure")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "billing_error.settle_failed") {
		t.Fatalf("expected settle failure code in response, got %s", rec.Body.String())
	}
}

// --- relayStreamRequest tests ---

func TestRelayStreamRequest_NormalFlow(t *testing.T) {
	settleCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 10000 // balance
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (float64, float64, error) {
			return 5, 15, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalled = true
			if req.PromptTokens != 10 {
				t.Errorf("expected promptTokens=10, got %d", req.PromptTokens)
			}
			if req.CompletionTokens != 8 {
				t.Errorf("expected completionTokens=8, got %d", req.CompletionTokens)
			}
			return &billing.SettleResult{SettleID: 1, Refund: 0, NewBalance: 9990, Success: true}, nil
		},
	}
	relay := newTestRelay(db, b)

	// Construct SSE stream from "upstream"
	sseStream := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
		"data: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":8,\"total_tokens\":18}}\n\n" +
		"data: [DONE]\n\n"

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sseStream)),
		Header:     http.Header{},
	}

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"},
	}

	rec := httptest.NewRecorder()
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-1", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "Hi"}}, logger.New(logger.Fields{RequestID: "req-stream-1", OrgID: 1}), time.Now(), 1000, nil)

	if !settleCalled {
		t.Fatal("expected settle to be called")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatal("expected [DONE] in output")
	}
	if strings.Count(body, "data: [DONE]") != 1 {
		t.Fatalf("expected exactly 1 [DONE] marker, got %d", strings.Count(body, "data: [DONE]"))
	}
	unirouteIndex := strings.Index(body, "\"_uniroute\"")
	doneIndex := strings.Index(body, "data: [DONE]")
	if unirouteIndex == -1 {
		t.Fatal("expected _uniroute in output")
	}
	if doneIndex == -1 || unirouteIndex > doneIndex {
		t.Fatal("expected _uniroute before [DONE]")
	}
	if !strings.Contains(body, "\"usage\"") {
		t.Fatal("expected usage in final extension chunk")
	}
	// Check SSE headers
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestRelayStreamRequest_NoUpstreamUsage(t *testing.T) {
	settleCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 10000
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalled = true
			// When no upstream usage, tokens are estimated
			if req.UsageSource != metering.UsageSourceCounterV2 {
				t.Errorf("expected usageSource=counter_v2, got %s", req.UsageSource)
			}
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
	}
	relay := newTestRelay(db, b)

	// SSE stream WITHOUT usage chunk
	sseStream := "data: {\"choices\":[{\"delta\":{\"content\":\"Hi there\"}}]}\n\n" +
		"data: [DONE]\n\n"

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sseStream)),
	}

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"},
	}

	rec := httptest.NewRecorder()
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-2", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "Hello"}}, logger.New(logger.Fields{RequestID: "req-stream-2", OrgID: 1}), time.Now(), 1000, nil)

	if !settleCalled {
		t.Fatal("expected settle to be called")
	}
}

func TestRelayStreamRequest_AnthropicNativeMissingUsageFallsBack(t *testing.T) {
	settleCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 10000
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalled = true
			if req.UsageSource != metering.UsageSourceCounterV2 {
				t.Fatalf("expected usageSource=counter_v2 for missing Anthropic stream usage, got %s", req.UsageSource)
			}
			if req.PromptTokens <= 0 || req.CompletionTokens <= 0 {
				t.Fatalf("expected fallback prompt/completion tokens, got %+v", req)
			}
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
	}
	relay := newTestRelay(db, b)

	sseStream := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"fallback text\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(sseStream)),
	}
	route := &RouteResult{
		Channel:     models.Channel{ID: 1, Name: "anthropic", Provider: "anthropic", AdapterType: adapters.TypeAnthropicNative},
		MappedModel: "claude-3-5-sonnet-20241022",
	}

	rec := httptest.NewRecorder()
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-ant-missing-usage", 1, 1, "company-chat", "company-chat", "127.0.0.1", []Message{{Role: "user", Content: "Hello"}}, logger.New(logger.Fields{RequestID: "req-ant-missing-usage", OrgID: 1}), time.Now(), 1000, adapters.NewAnthropicNative())

	if !settleCalled {
		t.Fatal("expected settle to be called")
	}
	body := rec.Body.String()
	if strings.Contains(body, "message_start") || strings.Contains(body, "content_block_delta") {
		t.Fatalf("expected Anthropic events to be normalized away, got %s", body)
	}
	if !strings.Contains(body, `"content":"fallback text"`) {
		t.Fatalf("expected normalized fallback content chunk, got %s", body)
	}
}

func TestRelayStreamRequest_SettleError(t *testing.T) {
	settleCalls := 0
	cancelCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 10000
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return nil, fmt.Errorf("settle failed")
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalled = true
			if requestID != "req-stream-3" || orgID != 1 {
				t.Fatalf("unexpected cancel args request_id=%s org_id=%d", requestID, orgID)
			}
			return nil
		},
	}
	relay := newTestRelay(db, b)

	sseStream := "data: {\"choices\":[{\"delta\":{\"content\":\"test\"}}]}\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sseStream)),
	}

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"},
	}

	rec := httptest.NewRecorder()
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-3", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-stream-3", OrgID: 1}), time.Now(), 1000, nil)

	if settleCalls != billingSettleMaxAttempts {
		t.Fatalf("expected %d settle attempts, got %d", billingSettleMaxAttempts, settleCalls)
	}
	if !cancelCalled {
		t.Fatal("expected cancel to release frozen amount after stream settle failure")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatal("expected [DONE] even after settle error")
	}
	if !strings.Contains(body, `"billing_status":"settle_failed_cancelled"`) {
		t.Fatalf("expected settle failure billing status, got %s", body)
	}
	if !strings.Contains(body, `"charged":0`) {
		t.Fatalf("expected charged cost to be zero after cancelled settle failure, got %s", body)
	}
}

func TestRelayStreamRequest_UpstreamUsageSource(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 10000
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			if req.UsageSource != metering.UsageSourceUpstream {
				t.Errorf("expected usageSource=upstream, got %s", req.UsageSource)
			}
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
	}
	relay := newTestRelay(db, b)

	sseStream := "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n" +
		"data: {\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\n" +
		"data: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sseStream)),
	}

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"},
	}

	rec := httptest.NewRecorder()
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-4", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-stream-4", OrgID: 1}), time.Now(), 1000, nil)
}

func TestRelayStreamRequest_EmptyStream(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 10000
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
	}
	relay := newTestRelay(db, b)

	// Empty stream with just [DONE]
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
	}

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"},
	}

	rec := httptest.NewRecorder()
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-5", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-stream-5", OrgID: 1}), time.Now(), 1000, nil)

	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatal("expected [DONE] in output")
	}
}

func TestRelayStreamRequest_UnirouteExtension(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 10000
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
	}
	relay := newTestRelay(db, b)

	sseStream := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sseStream)),
	}

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"},
	}

	rec := httptest.NewRecorder()
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-6", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-stream-6", OrgID: 1}), time.Now(), 1000, nil)

	body := rec.Body.String()
	// Extension data (request_id, cost) should be present in output
	if !strings.Contains(body, "\"_uniroute\"") {
		t.Fatal("expected _uniroute in output")
	}
	if !strings.Contains(body, "\"cost\"") {
		t.Fatal("expected cost in output")
	}
	if !strings.Contains(body, "\"usage\"") {
		t.Fatal("expected usage in output")
	}
	if strings.Count(body, "data: [DONE]") != 1 {
		t.Fatalf("expected exactly 1 [DONE] marker, got %d", strings.Count(body, "data: [DONE]"))
	}
	unirouteIndex := strings.Index(body, "\"_uniroute\"")
	doneIndex := strings.Index(body, "data: [DONE]")
	if doneIndex == -1 || unirouteIndex == -1 || unirouteIndex > doneIndex {
		t.Fatal("expected _uniroute before [DONE]")
	}
}

// --- HandleChatCompletion with mock upstream ---

func TestUpstreamEndpoint_DoesNotDoubleAppendV1(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		endpoint string
		want     string
	}{
		{
			name:     "base without v1",
			baseURL:  "https://provider.example.com",
			endpoint: "/v1/chat/completions",
			want:     "https://provider.example.com/v1/chat/completions",
		},
		{
			name:     "base with v1",
			baseURL:  "https://provider.example.com/v1",
			endpoint: "/v1/chat/completions",
			want:     "https://provider.example.com/v1/chat/completions",
		},
		{
			name:     "base with v1 trailing slash",
			baseURL:  "https://provider.example.com/v1/",
			endpoint: "/v1/embeddings",
			want:     "https://provider.example.com/v1/embeddings",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upstreamEndpoint(tt.baseURL, tt.endpoint); got != tt.want {
				t.Fatalf("upstreamEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHandleChatCompletion_Success_NonStream(t *testing.T) {
	// Mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"role": "assistant", "content": "Hi!"}}},
			"usage":   map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	defer upstream.Close()

	// Mock DB: RouteAll needs channel query + key update + record insert
	callCount := 0
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			callCount++
			// RouteAll query
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1                               // ch.ID
						*dest[1].(*string) = "test-ch"                      // ch.Name
						*dest[2].(*string) = "openai"                       // ch.Provider
						*dest[3].(*string) = upstream.URL                   // ch.BaseURL
						*dest[4].(*string) = "gpt-4o"                       // ch.Models
						*dest[5].(*json.RawMessage) = json.RawMessage(`{}`) // ch.ModelMapping
						*dest[6].(*string) = "openai_compatible"            // ch.AdapterType
						*dest[7].(*json.RawMessage) = json.RawMessage(`{}`) // ch.AdapterConfig
						*dest[8].(*int) = 128000                            // ch.MaxContext
						*dest[9].(*int) = 4096                              // ch.DefaultMaxOutput
						*dest[10].(*int) = 100                              // ch.Priority
						*dest[11].(*string) = "active"                      // ch.Status
						*dest[12].(*string) = "healthy"                     // ch.CircuitState
						*dest[13].(**time.Time) = nil                       // ch.HalfOpenUntil
						*dest[14].(*int) = 0                                // ch.FailCount
						*dest[15].(*int64) = 1                              // ck.ID
						*dest[16].(*string) = "sk-encrypted"                // ck.KeyEnc
						*dest[17].(*string) = "active"                      // ck.Status
						*dest[18].(**time.Time) = nil                       // ck.CooldownUntil
						return nil
					},
				},
			}, nil
		},
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					// For recordRequest or other QueryRow calls
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}

	// Mock billing
	freezeResult := &billing.FreezeResult{FreezeID: 1, Amount: 100, NewBalance: 900, Success: true}
	settleResult := &billing.SettleResult{SettleID: 1, Refund: 5, NewBalance: 905, Success: true}
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return freezeResult, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			return settleResult, nil
		},
	}

	// Create relay with encryption key that matches "sk-encrypted"
	encKey := []byte("0123456789abcdef01234567") // 24 bytes for AES
	// Encrypt the API key
	encryptedKey, _ := auth.EncryptKey([]byte("sk-test-key"), encKey)

	// Re-create the mock to return the encrypted key
	db.QueryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return &testutil.MockRows{
			Rows: []func(dest ...any) error{
				func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*string) = "test-ch"
					*dest[2].(*string) = "openai"
					*dest[3].(*string) = upstream.URL
					*dest[4].(*string) = "gpt-4o"
					*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
					*dest[6].(*string) = "openai_compatible"
					*dest[7].(*json.RawMessage) = json.RawMessage(`{}`)
					*dest[8].(*int) = 128000
					*dest[9].(*int) = 4096
					*dest[10].(*int) = 100
					*dest[11].(*string) = "active"
					*dest[12].(*string) = "healthy"
					*dest[13].(**time.Time) = nil
					*dest[14].(*int) = 0
					*dest[15].(*int64) = 1
					*dest[16].(*string) = encryptedKey
					*dest[17].(*string) = "active"
					*dest[18].(**time.Time) = nil
					return nil
				},
			},
		}, nil
	}

	relay := &Relay{
		db:      db,
		router:  NewRouter(db),
		billing: b,
		encKey:  encKey,
	}

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-e2e-1")
	apiKeyObj := &models.ApiKey{ID: 1, OrgID: 1, Status: "active"}
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, apiKeyObj)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["id"] != "chatcmpl-123" {
		t.Fatalf("expected chatcmpl-123, got %v", resp["id"])
	}
}

func TestHandleChatCompletion_AnthropicNativeNonStream(t *testing.T) {
	var upstreamPath string
	var upstreamModel string
	var upstreamSystem string
	var upstreamAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		upstreamAPIKey = r.Header.Get("x-api-key")
		var body struct {
			Model    string `json:"model"`
			System   string `json:"system"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		upstreamModel = body.Model
		upstreamSystem = body.System
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" || body.Messages[0].Content != "hello" {
			t.Fatalf("unexpected anthropic messages: %+v", body.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id":"msg_123",
			"type":"message",
			"role":"assistant",
			"model":"claude-3-5-sonnet-20241022",
			"content":[{"type":"text","text":"Hi from Anthropic"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":5}
		}`))
	}))
	defer upstream.Close()

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, err := auth.EncryptKey([]byte("sk-ant-test"), encKey)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	var settleReq billing.SettleRequest
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "anthropic-primary"
						*dest[2].(*string) = "anthropic"
						*dest[3].(*string) = upstream.URL
						*dest[4].(*string) = "company-chat"
						*dest[5].(*json.RawMessage) = json.RawMessage(`{"company-chat":"claude-3-5-sonnet-20241022"}`)
						*dest[6].(*string) = "anthropic_native"
						*dest[7].(*json.RawMessage) = json.RawMessage(`{"anthropic_version":"2023-06-01"}`)
						*dest[8].(*int) = 200000
						*dest[9].(*int) = 4096
						*dest[10].(*int) = 1
						*dest[11].(*string) = "active"
						*dest[12].(*string) = "healthy"
						*dest[13].(**time.Time) = nil
						*dest[14].(*int) = 0
						*dest[15].(*int64) = 1
						*dest[16].(*string) = encryptedKey
						*dest[17].(*string) = "active"
						*dest[18].(**time.Time) = nil
						return nil
					},
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, Amount: 100, NewBalance: 900, Success: true}, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleReq = req
			return &billing.SettleResult{SettleID: 1, Refund: 0, NewBalance: 900, Success: true}, nil
		},
	}
	relay := &Relay{db: db, router: NewRouter(db), billing: b, encKey: encKey}

	body := `{"model":"company-chat","messages":[{"role":"system","content":"Be direct."},{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-anthropic-1")
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, Status: "active"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if upstreamPath != "/v1/messages" {
		t.Fatalf("expected anthropic /v1/messages, got %s", upstreamPath)
	}
	if upstreamAPIKey != "sk-ant-test" {
		t.Fatalf("expected x-api-key from decrypted key, got %q", upstreamAPIKey)
	}
	if upstreamModel != "claude-3-5-sonnet-20241022" {
		t.Fatalf("expected mapped upstream model, got %q", upstreamModel)
	}
	if upstreamSystem != "Be direct." {
		t.Fatalf("expected system prompt conversion, got %q", upstreamSystem)
	}
	if settleReq.PromptTokens != 10 || settleReq.CompletionTokens != 5 || settleReq.UsageSource != metering.UsageSourceUpstream {
		t.Fatalf("unexpected settle request: %+v", settleReq)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["object"] != "chat.completion" || resp["id"] != "msg_123" {
		t.Fatalf("expected OpenAI-compatible normalized body, got %v", resp)
	}
	choices := resp["choices"].([]interface{})
	message := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	if message["content"] != "Hi from Anthropic" {
		t.Fatalf("unexpected assistant content: %v", message["content"])
	}
	if _, ok := resp["_uniroute"].(map[string]interface{}); !ok {
		t.Fatalf("expected _uniroute extension, got %v", resp)
	}
}

func TestHandleChatCompletion_AnthropicNativeStream(t *testing.T) {
	var upstreamPath string
	var upstreamStream bool
	var upstreamAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		upstreamAPIKey = r.Header.Get("x-api-key")
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		upstreamStream = body.Stream
		if body.Model != "claude-3-5-sonnet-20241022" {
			t.Fatalf("expected mapped anthropic model, got %q", body.Model)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(
			"event: message_start\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"model\":\"claude-3-5-sonnet-20241022\",\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\n\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" there\"}}\n\n" +
				"event: message_delta\n" +
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":6}}\n\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n\n",
		))
	}))
	defer upstream.Close()

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, err := auth.EncryptKey([]byte("sk-ant-stream"), encKey)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	var settleReq billing.SettleRequest
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "anthropic-stream"
						*dest[2].(*string) = "anthropic"
						*dest[3].(*string) = upstream.URL
						*dest[4].(*string) = "company-chat"
						*dest[5].(*json.RawMessage) = json.RawMessage(`{"company-chat":"claude-3-5-sonnet-20241022"}`)
						*dest[6].(*string) = "anthropic_native"
						*dest[7].(*json.RawMessage) = json.RawMessage(`{"anthropic_version":"2023-06-01"}`)
						*dest[8].(*int) = 200000
						*dest[9].(*int) = 4096
						*dest[10].(*int) = 1
						*dest[11].(*string) = "active"
						*dest[12].(*string) = "healthy"
						*dest[13].(**time.Time) = nil
						*dest[14].(*int) = 0
						*dest[15].(*int64) = 1
						*dest[16].(*string) = encryptedKey
						*dest[17].(*string) = "active"
						*dest[18].(**time.Time) = nil
						return nil
					},
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, Amount: 100, NewBalance: 900, Success: true}, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleReq = req
			return &billing.SettleResult{SettleID: 1, Refund: 0, NewBalance: 900, Success: true}, nil
		},
	}
	relay := &Relay{db: db, router: NewRouter(db), billing: b, encKey: encKey}

	body := `{"model":"company-chat","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-anthropic-stream")
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, Status: "active"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if upstreamPath != "/v1/messages" {
		t.Fatalf("expected anthropic /v1/messages, got %s", upstreamPath)
	}
	if !upstreamStream {
		t.Fatal("expected upstream anthropic stream=true")
	}
	if upstreamAPIKey != "sk-ant-stream" {
		t.Fatalf("expected x-api-key from decrypted key, got %q", upstreamAPIKey)
	}
	if settleReq.PromptTokens != 11 || settleReq.CompletionTokens != 6 || settleReq.UsageSource != metering.UsageSourceUpstream {
		t.Fatalf("unexpected settle request: %+v", settleReq)
	}

	responseBody := rec.Body.String()
	if strings.Contains(responseBody, "message_start") || strings.Contains(responseBody, "content_block_delta") {
		t.Fatalf("expected Anthropic event types to be normalized away, got %s", responseBody)
	}
	if !strings.Contains(responseBody, `"object":"chat.completion.chunk"`) {
		t.Fatalf("expected OpenAI-compatible stream chunks, got %s", responseBody)
	}
	if !strings.Contains(responseBody, `"role":"assistant"`) || !strings.Contains(responseBody, `"content":"Hi"`) || !strings.Contains(responseBody, `"content":" there"`) {
		t.Fatalf("expected translated role/content chunks, got %s", responseBody)
	}
	if !strings.Contains(responseBody, `"_uniroute"`) {
		t.Fatalf("expected _uniroute final chunk, got %s", responseBody)
	}
	if strings.Count(responseBody, "data: [DONE]") != 1 {
		t.Fatalf("expected exactly one [DONE], got %d in %s", strings.Count(responseBody, "data: [DONE]"), responseBody)
	}
	unirouteIndex := strings.Index(responseBody, `"_uniroute"`)
	doneIndex := strings.Index(responseBody, "data: [DONE]")
	if doneIndex == -1 || unirouteIndex == -1 || unirouteIndex > doneIndex {
		t.Fatalf("expected _uniroute before [DONE], got %s", responseBody)
	}
}

func TestHandleChatCompletion_UpstreamError_AllFail(t *testing.T) {
	// Mock upstream that returns 500
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer upstream.Close()

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, _ := auth.EncryptKey([]byte("sk-test-key"), encKey)

	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "test-ch"
						*dest[2].(*string) = "openai"
						*dest[3].(*string) = upstream.URL
						*dest[4].(*string) = "gpt-4o"
						*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[6].(*string) = "openai_compatible"
						*dest[7].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[8].(*int) = 128000
						*dest[9].(*int) = 4096
						*dest[10].(*int) = 100
						*dest[11].(*string) = "active"
						*dest[12].(*string) = "healthy"
						*dest[13].(**time.Time) = nil
						*dest[14].(*int) = 0
						*dest[15].(*int64) = 1
						*dest[16].(*string) = encryptedKey
						*dest[17].(*string) = "active"
						*dest[18].(**time.Time) = nil
						return nil
					},
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}

	cancelCalled := false
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, Amount: 100, Success: true}, nil
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalled = true
			return nil
		},
	}

	relay := &Relay{
		db:      db,
		router:  NewRouter(db),
		billing: b,
		encKey:  encKey,
	}

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-e2e-2")
	apiKeyObj := &models.ApiKey{ID: 1, OrgID: 1, Status: "active"}
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, apiKeyObj)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
	if !cancelCalled {
		t.Fatal("expected Cancel to be called after all channels failed")
	}
}

func TestHandleChatCompletion_ModelNotAllowed(t *testing.T) {
	encKey := []byte("0123456789abcdef01234567")
	db := &testutil.MockDB{}
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, Amount: 100, Success: true}, nil
		},
	}

	relay := &Relay{db: db, router: NewRouter(db), billing: b, encKey: encKey}

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-e2e-3")
	// API key with restricted models
	apiKeyObj := &models.ApiKey{ID: 1, OrgID: 1, Status: "active", AllowedModels: "claude-3,gpt-3.5-turbo"}
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, apiKeyObj)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletion_SensitiveContent(t *testing.T) {
	encKey := []byte("0123456789abcdef01234567")
	f := filter.NewDFAFilter([]string{"赌博", "色情"})
	db := &testutil.MockDB{}
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, Amount: 100, Success: true}, nil
		},
	}

	relay := &Relay{db: db, router: NewRouter(db), billing: b, encKey: encKey, filter: f}

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"我想赌博"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-e2e-4")
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, Status: "active"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleChatCompletion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
