package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/capbilling"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/traceutil"
)

// --- Mock BillingProvider ---

type mockBilling struct {
	queryModelPriceFn      func(ctx context.Context, orgID int64, model string) (int64, int64, error)
	calculateCostFn        func(ctx context.Context, orgID int64, model string, promptTokens, completionTokens int) (int64, error)
	freezeFn               func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error)
	extendFn               func(ctx context.Context, req billing.ExtendReservationRequest) (*billing.ExtendReservationResult, error)
	beginStreamFn          func(ctx context.Context, start billing.StreamDeliveryStart) error
	renewStreamFn          func(ctx context.Context, start billing.StreamDeliveryStart) error
	checkpointFn           func(ctx context.Context, checkpoint billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error)
	finishStreamFn         func(ctx context.Context, start billing.StreamDeliveryStart, state, reason string) error
	beginAttemptFn         func(ctx context.Context, req billing.ProviderAttemptStart) error
	completeAttemptFn      func(ctx context.Context, req billing.ProviderAttemptCompletion) error
	prepareDeliveryFn      func(ctx context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error)
	prepareCacheDeliveryFn func(ctx context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error)
	holdOutputFn           func(ctx context.Context, req billing.SettleRequest, reason string) error
	recordDeliveryFn       func(ctx context.Context, event billing.DeliveryEvent) error
	recordCacheDeliveryFn  func(ctx context.Context, event billing.ResponseCacheDeliveryEvent) error
	markOutputFn           func(ctx context.Context, requestID string, orgID int64) error
	persistUsageFn         func(ctx context.Context, req billing.SettleRequest) error
	settleFn               func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error)
	cancelFn               func(ctx context.Context, requestID string, orgID int64) error
}

func (m *mockBilling) PrepareResponseCacheDelivery(ctx context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
	if m.prepareCacheDeliveryFn != nil {
		return m.prepareCacheDeliveryFn(ctx, req)
	}
	return &billing.PrepareDeliveryResult{Funded: true, Preview: billing.SettleResult{Success: true}}, nil
}

func (m *mockBilling) BeginProviderAttempt(ctx context.Context, req billing.ProviderAttemptStart) error {
	if m.beginAttemptFn != nil {
		return m.beginAttemptFn(ctx, req)
	}
	return nil
}

func (m *mockBilling) CompleteProviderAttempt(ctx context.Context, req billing.ProviderAttemptCompletion) error {
	if m.completeAttemptFn != nil {
		return m.completeAttemptFn(ctx, req)
	}
	return nil
}

func (m *mockBilling) PrepareNonStreamDelivery(ctx context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
	if m.prepareDeliveryFn != nil {
		return m.prepareDeliveryFn(ctx, req)
	}
	return &billing.PrepareDeliveryResult{
		Funded:  true,
		Preview: billing.SettleResult{ActualCostMicros: 1, ChargedCostMicros: 1, Success: true},
	}, nil
}

func (m *mockBilling) HoldNonStreamOutput(ctx context.Context, req billing.SettleRequest, reason string) error {
	if m.holdOutputFn != nil {
		return m.holdOutputFn(ctx, req, reason)
	}
	return nil
}

func (m *mockBilling) RecordDelivery(ctx context.Context, event billing.DeliveryEvent) error {
	if m.recordDeliveryFn != nil {
		return m.recordDeliveryFn(ctx, event)
	}
	return nil
}

func (m *mockBilling) RecordResponseCacheDelivery(ctx context.Context, event billing.ResponseCacheDeliveryEvent) error {
	if m.recordCacheDeliveryFn != nil {
		return m.recordCacheDeliveryFn(ctx, event)
	}
	return nil
}

func (m *mockBilling) MarkBillableOutput(ctx context.Context, requestID string, orgID int64) error {
	if m.markOutputFn != nil {
		return m.markOutputFn(ctx, requestID, orgID)
	}
	return nil
}

func (m *mockBilling) PersistSettlementUsage(ctx context.Context, req billing.SettleRequest) error {
	if m.persistUsageFn != nil {
		return m.persistUsageFn(ctx, req)
	}
	return nil
}

func (m *mockBilling) QueryModelPriceMicrosForOrg(ctx context.Context, orgID int64, model string) (int64, int64, error) {
	if m.queryModelPriceFn != nil {
		return m.queryModelPriceFn(ctx, orgID, model)
	}
	return 20_000_000, 100_000_000, nil
}

func (m *mockBilling) CalculateCostMicrosForOrg(ctx context.Context, orgID int64, model string, promptTokens, completionTokens int) (int64, error) {
	if m.calculateCostFn != nil {
		return m.calculateCostFn(ctx, orgID, model, promptTokens, completionTokens)
	}
	inputPrice, outputPrice, err := m.QueryModelPriceMicrosForOrg(ctx, orgID, model)
	if err != nil {
		return 0, err
	}
	calculation, err := money.Calculate(
		money.Component{Tokens: int64(promptTokens), PriceMicrosPerMillion: inputPrice},
		money.Component{Tokens: int64(completionTokens), PriceMicrosPerMillion: outputPrice},
	)
	return calculation.TotalMicros, err
}

func (m *mockBilling) Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
	if m.freezeFn != nil {
		return m.freezeFn(ctx, req)
	}
	return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, NewBalanceMicros: 900, Success: true}, nil
}

func (m *mockBilling) ExtendReservation(ctx context.Context, req billing.ExtendReservationRequest) (*billing.ExtendReservationResult, error) {
	if m.extendFn != nil {
		return m.extendFn(ctx, req)
	}
	return &billing.ExtendReservationResult{
		Success: true, Sequence: req.Sequence,
		AddedCompletionTokens:  req.AdditionalCompletionTokens,
		TargetCompletionTokens: req.TargetCompletionTokens,
		AmountMicros:           1,
	}, nil
}

func (m *mockBilling) BeginStreamDelivery(ctx context.Context, start billing.StreamDeliveryStart) error {
	if m.beginStreamFn != nil {
		return m.beginStreamFn(ctx, start)
	}
	// Preserve the observable sub-events older relay tests asserted while the
	// production implementation now commits marker, receipt and lease atomically.
	if m.markOutputFn != nil {
		if err := m.markOutputFn(ctx, start.RequestID, start.OrgID); err != nil {
			return err
		}
	}
	if m.recordDeliveryFn != nil {
		fence := start.Fence()
		return m.recordDeliveryFn(ctx, billing.DeliveryEvent{
			RequestID: start.RequestID, AttemptID: start.AttemptID, OrgID: start.OrgID,
			Mode: billing.DeliveryModeStream, Outcome: billing.DeliveryOutcomeWriteStarted,
			HTTPStatus: http.StatusOK, StreamFence: &fence,
		})
	}
	return nil
}

func (m *mockBilling) RenewStreamDelivery(ctx context.Context, start billing.StreamDeliveryStart) error {
	if m.renewStreamFn != nil {
		return m.renewStreamFn(ctx, start)
	}
	return nil
}

func (m *mockBilling) CheckpointStreamDelivery(ctx context.Context, checkpoint billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error) {
	if m.checkpointFn != nil {
		return m.checkpointFn(ctx, checkpoint)
	}
	return &billing.StreamCheckpointResult{Sequence: 1}, nil
}

func (m *mockBilling) FinishStreamDelivery(ctx context.Context, start billing.StreamDeliveryStart, state, reason string) error {
	if m.finishStreamFn != nil {
		return m.finishStreamFn(ctx, start, state, reason)
	}
	return nil
}

func (m *mockBilling) Settle(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
	var result *billing.SettleResult
	var err error
	if m.settleFn != nil {
		result, err = m.settleFn(ctx, req)
	} else {
		result = &billing.SettleResult{SettleID: 1, RefundMicros: 10, NewBalanceMicros: 910, Success: true}
	}
	if err != nil || result == nil || !result.Success {
		return result, err
	}
	if result.ActualCostMicros == 0 && (req.PromptTokens > 0 || req.CompletionTokens > 0) {
		result.ActualCostMicros, _ = m.CalculateCostMicrosForOrg(ctx, req.OrgID, "", req.PromptTokens, req.CompletionTokens)
	}
	if result.ChargedCostMicros == 0 {
		result.ChargedCostMicros = result.ActualCostMicros
	}
	return result, nil
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
			t.Fatal("CtxAPIKey authority must not be queried again")
			return nil
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			require.Contains(t, sql, "WITH candidates AS")
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error { *dest[0].(*string) = "gpt-4o"; return nil },
				func(dest ...any) error { *dest[0].(*string) = "gpt-3.5-turbo"; return nil },
			}}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, AllowedModels: "gpt-4o,gpt-3.5-turbo"})
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
	queryCount := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			t.Fatal("CtxAPIKey authority must not be queried again")
			return nil
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCount++
			if strings.Contains(sql, "FROM model_prices") {
				return &testutil.MockRows{
					Rows: []func(dest ...any) error{
						func(dest ...any) error { *dest[0].(*string) = "gpt-4o"; return nil },
						func(dest ...any) error { *dest[0].(*string) = "claude-3"; return nil },
					},
				}, nil
			}
			require.Contains(t, sql, "WITH candidates AS")
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error { *dest[0].(*string) = "gpt-4o"; return nil },
					func(dest ...any) error { *dest[0].(*string) = "claude-3"; return nil },
				},
			}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1})
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
	require.Equal(t, 2, queryCount, "fallback must use one candidate query and one fixed-cost governance query")
}

func TestHandleModels_FallbackQueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			t.Fatal("CtxAPIKey authority must not be queried again")
			return nil
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1})
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
			t.Fatal("CtxAPIKey authority must not be queried again")
			return nil
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "FROM model_prices") {
				require.Contains(t, sql, "WITH candidates AS")
			}
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
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleModels_ProjectModelSetExcludesModelsDeniedAtCallAdmission(t *testing.T) {
	projectID := int64(7)
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			t.Fatal("CtxAPIKey authority and batch governance must not use QueryRow")
			return nil
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "FROM model_prices") {
				return &testutil.MockRows{Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "DeepSeek-V4-Flash"
						return nil
					},
					func(dest ...any) error {
						*dest[0].(*string) = "kimi-k2.6"
						return nil
					},
				}}, nil
			}
			require.Contains(t, sql, "WITH candidates AS")
			require.Equal(t, int64(1), args[0])
			require.Equal(t, projectID, args[1])
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error {
					*dest[0].(*string) = "DeepSeek-V4-Flash"
					return nil
				},
			}}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxProjectID, projectID)
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, ProjectID: &projectID})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleModels(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []map[string]interface{}{{
		"id": "DeepSeek-V4-Flash", "object": "model", "owned_by": "uniroute",
	}}, resp.Data)
}

func TestHandleModels_ProjectModelSetIntersectsExplicitKeyAllowlist(t *testing.T) {
	projectID := int64(7)
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			t.Fatal("CtxAPIKey authority and batch governance must not use QueryRow")
			return nil
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			require.Contains(t, sql, "WITH candidates AS")
			require.NotContains(t, sql, "FROM model_prices")
			require.Equal(t, []string{"DeepSeek-V4-Flash", "kimi-k2.6"}, args[2])
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error {
					*dest[0].(*string) = "DeepSeek-V4-Flash"
					return nil
				},
			}}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxProjectID, projectID)
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{
		ID: 1, OrgID: 1, ProjectID: &projectID, AllowedModels: "DeepSeek-V4-Flash,kimi-k2.6",
	})
	rec := httptest.NewRecorder()
	relay.HandleModels(rec, req.WithContext(ctx))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []map[string]interface{}{{
		"id": "DeepSeek-V4-Flash", "object": "model", "owned_by": "uniroute",
	}}, resp.Data)
}

func TestHandleModels_NoProjectExcludesRetiredModel(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			t.Fatal("CtxAPIKey authority and batch governance must not use QueryRow")
			return nil
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			require.Contains(t, sql, "WITH candidates AS")
			require.Equal(t, int64(1), args[0])
			require.Equal(t, int64(0), args[1])
			require.Equal(t, []string{"active-model", "retired-model"}, args[2])
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error {
					*dest[0].(*string) = "active-model"
					return nil
				},
			}}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{
		ID: 1, OrgID: 1, AllowedModels: "active-model,retired-model",
	})
	rec := httptest.NewRecorder()

	relay.HandleModels(rec, req.WithContext(ctx))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []map[string]interface{}{{
		"id": "active-model", "object": "model", "owned_by": "uniroute",
	}}, resp.Data)
}

func TestHandleModels_GovernanceErrorReturns500(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			t.Fatal("CtxAPIKey authority and batch governance must not use QueryRow")
			return nil
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			require.Contains(t, sql, "WITH candidates AS")
			return nil, errors.New("governance unavailable")
		},
	}
	relay := newTestRelay(db, &mockBilling{})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxAPIKey, &models.ApiKey{
		ID: 1, OrgID: 1, AllowedModels: "active-model",
	})
	rec := httptest.NewRecorder()

	relay.HandleModels(rec, req.WithContext(ctx))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "server_error.internal_error")
	require.NotContains(t, rec.Body.String(), "active-model")
}

func TestHandleModels_MissingContextAuthorityQueryFailureDoesNotExpandToFallback(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				return errors.New("authority unavailable")
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			t.Fatal("authority failure must not fall back to the global model list")
			return nil, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	rec := httptest.NewRecorder()

	relay.HandleModels(rec, req.WithContext(ctx))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Failed to resolve API key models")
}

// --- calculateCost tests ---

func TestCalculateCost_Success(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (int64, int64, error) {
			return 50_000_000, 150_000_000, nil // 5 and 15 cents per 1K tokens
		},
	})

	cost, err := relay.calculateCost(context.Background(), 1, "gpt-4o", 1000, 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != 125_000 {
		t.Fatalf("expected cost=125000 micros, got %d", cost)
	}
}

func TestCalculateCost_MinimumCost(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (int64, int64, error) {
			return 10_000_000, 10_000_000, nil
		},
	})

	cost, err := relay.calculateCost(context.Background(), 1, "gpt-4o", 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != 20 {
		t.Fatalf("expected exact cost=20 micros, got %d", cost)
	}
}

func TestCalculateCost_ReturnsErrorOnPriceLookupFailure(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (int64, int64, error) {
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
	relay.recordRequest(ctx, "req-001", 1, 1, 1, "openai", "ch1", "gpt-4o", "gpt-4o", "127.0.0.1", 100, 50, 10, 200, 123, 0, false, metering.UsageSourceUpstream, 180, "upstream-req-001")

	if !execCalled {
		t.Fatal("expected exec to be called")
	}
	if len(capturedArgs) != callLogInsertArgCount {
		t.Fatalf("expected %d insert args, got %d", callLogInsertArgCount, len(capturedArgs))
	}
	if capturedArgs[callLogArgProjectID] != int64(7) {
		t.Fatalf("expected project_id arg 7, got %v", capturedArgs[callLogArgProjectID])
	}
	if capturedArgs[callLogArgOwnerUserID] != ownerUserID {
		t.Fatalf("expected owner_user_id arg 42, got %v", capturedArgs[callLogArgOwnerUserID])
	}
	if capturedArgs[callLogArgKeyType] != "personal" {
		t.Fatalf("expected key_type arg personal, got %v", capturedArgs[callLogArgKeyType])
	}
	if capturedArgs[callLogArgLatencyMS] != 123 {
		t.Fatalf("expected latency_ms arg 123, got %v", capturedArgs[callLogArgLatencyMS])
	}
	if capturedArgs[callLogArgEstimatedTokens] != 180 {
		t.Fatalf("expected estimated_tokens arg 180, got %v", capturedArgs[callLogArgEstimatedTokens])
	}
	if capturedArgs[callLogArgUsageConfidence] != 1.0 {
		t.Fatalf("expected upstream usage confidence 1.0, got %v", capturedArgs[callLogArgUsageConfidence])
	}
	if capturedArgs[callLogArgTraceID] != "req-001" || capturedArgs[callLogArgSessionID] != nil || capturedArgs[callLogArgSchemaVersion] != evidenceSchemaVersion || capturedArgs[callLogArgCaptureMode] != evidenceCaptureModeNative {
		t.Fatalf("unexpected call-log evidence args: %#v", capturedArgs[callLogArgTraceID:callLogArgCaptureMode+1])
	}
	if capturedArgs[callLogArgUpstreamRequestID] != "upstream-req-001" {
		t.Fatalf("expected upstream_request_id, got %#v", capturedArgs[callLogArgUpstreamRequestID])
	}
	if capturedArgs[callLogArgWorkflowID] != nil || capturedArgs[callLogArgAgentID] != nil || capturedArgs[callLogArgAttemptID] != nil {
		t.Fatalf("unexpected workflow/agent/attempt args: %#v", capturedArgs[callLogArgWorkflowID:callLogArgAttemptID+1])
	}
	if capturedArgs[callLogArgActorType] != "human" || capturedArgs[callLogArgActorRef] != "user:42" || capturedArgs[callLogArgOnBehalfOf] != nil || capturedArgs[callLogArgPayloadCaptureMode] != evidencePayloadMetadata {
		t.Fatalf("unexpected call-log actor/payload args: %#v", capturedArgs[callLogArgActorType:callLogArgPayloadCaptureMode+1])
	}
	if hash, ok := capturedArgs[callLogArgSourceEventHash].(string); !ok || len(hash) != 64 {
		t.Fatalf("expected source_event_hash, got %#v", capturedArgs[callLogArgSourceEventHash])
	}
}

func TestRecordRequest_UsesConfiguredPayloadCaptureMode(t *testing.T) {
	var capturedArgs []any
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO call_logs") {
				return pgconn.CommandTag{}, nil
			}
			capturedArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})
	relay.SetPayloadCaptureMode(traceutil.PayloadCaptureModeDisabled)

	relay.recordRequest(context.Background(), "req-disabled", 1, 1, 1, "openai", "ch1", "gpt-4o", "gpt-4o", "127.0.0.1", 100, 50, 10, 200, 123, 0, false, metering.UsageSourceUpstream, 180, "")

	if len(capturedArgs) != callLogInsertArgCount {
		t.Fatalf("expected %d insert args, got %d", callLogInsertArgCount, len(capturedArgs))
	}
	if capturedArgs[callLogArgPayloadCaptureMode] != traceutil.PayloadCaptureModeDisabled {
		t.Fatalf("expected disabled payload capture mode, got %#v", capturedArgs[callLogArgPayloadCaptureMode])
	}
}

func TestRecordRequest_UsesTrustedEvidenceContext(t *testing.T) {
	var capturedArgs []any
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO call_logs") {
				return pgconn.CommandTag{}, nil
			}
			capturedArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})
	ownerUserID := int64(42)
	ctx := context.WithValue(context.Background(), middleware.CtxAPIKey, &models.ApiKey{OwnerUserID: &ownerUserID, KeyType: "personal"})
	ctx = traceutil.ContextWithEvidence(ctx, traceutil.EvidenceContext{
		TraceID:    "trace-internal-1",
		SessionID:  "session-42",
		WorkflowID: "workflow-42",
		AgentID:    "agent-42",
		ActorType:  "agent",
		ActorRef:   "agent:planner",
		OnBehalfOf: "user:42",
	})
	ctx = contextWithAttemptID(ctx, "att_test")

	relay.recordRequest(ctx, "req-context", 1, 1, 1, "openai", "ch1", "gpt-4o", "gpt-4o", "127.0.0.1", 100, 50, 10, 200, 123, 0, false, metering.UsageSourceUpstream, 180, "")

	if len(capturedArgs) != callLogInsertArgCount {
		t.Fatalf("expected %d insert args, got %d", callLogInsertArgCount, len(capturedArgs))
	}
	if capturedArgs[callLogArgTraceID] != "trace-internal-1" || capturedArgs[callLogArgSessionID] != "session-42" {
		t.Fatalf("expected trusted trace/session args, got %#v", capturedArgs[callLogArgTraceID:callLogArgSessionID+1])
	}
	if capturedArgs[callLogArgWorkflowID] != "workflow-42" || capturedArgs[callLogArgAgentID] != "agent-42" || capturedArgs[callLogArgAttemptID] != "att_test" {
		t.Fatalf("expected workflow/agent/attempt args: %#v", capturedArgs[callLogArgWorkflowID:callLogArgAttemptID+1])
	}
	if capturedArgs[callLogArgActorType] != "agent" || capturedArgs[callLogArgActorRef] != "agent:planner" || capturedArgs[callLogArgOnBehalfOf] != "user:42" {
		t.Fatalf("expected trusted actor/delegation args, got %#v", capturedArgs[callLogArgActorType:callLogArgOnBehalfOf+1])
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
	relay.SetPayloadCaptureMode(traceutil.PayloadCaptureModeDisabled)

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
	}, "upstream-trace-001")

	if len(capturedArgs) != 39 {
		t.Fatalf("expected 39 trace insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[4] != int64(7) || capturedArgs[5] != int64(9) || capturedArgs[6] != ownerUserID || capturedArgs[7] != "personal" {
		t.Fatalf("unexpected project/key/owner args: %#v", capturedArgs[4:8])
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(capturedArgs[17].(string)), &metadata); err != nil {
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
	if capturedArgs[18] != "req-trace" {
		t.Fatalf("expected trace_id to use request_id, got %#v", capturedArgs[18])
	}
	if spanID, ok := capturedArgs[19].(string); !ok || len(spanID) != 16 {
		t.Fatalf("expected generated span_id, got %#v", capturedArgs[19])
	}
	if capturedArgs[20] != nil || capturedArgs[21] != evidenceSchemaVersion || capturedArgs[22] != "provider" || capturedArgs[23] != evidenceCaptureModeNative {
		t.Fatalf("unexpected trace structural args: %#v", capturedArgs[20:24])
	}
	if capturedArgs[24] != "redacted" {
		t.Fatalf("expected redacted capture_gap, got %#v", capturedArgs[24])
	}
	if capturedArgs[26] != "upstream-trace-001" {
		t.Fatalf("expected upstream_request_id, got %#v", capturedArgs[26])
	}
	if capturedArgs[27] != "provider" || capturedArgs[28] != "selected" || capturedArgs[29] != "provider.response_accepted" {
		t.Fatalf("unexpected trace decision args: %#v", capturedArgs[27:30])
	}
	if capturedArgs[30] != "human" || capturedArgs[31] != "user:42" || capturedArgs[32] != nil || capturedArgs[33] != nil {
		t.Fatalf("unexpected trace actor/session args: %#v", capturedArgs[30:34])
	}
	if capturedArgs[34] != nil || capturedArgs[35] != nil || capturedArgs[36] != nil || capturedArgs[37] != traceutil.PayloadCaptureModeDisabled {
		t.Fatalf("unexpected trace workflow/agent/attempt/payload args: %#v", capturedArgs[34:38])
	}
	if hash, ok := capturedArgs[38].(string); !ok || len(hash) != 64 {
		t.Fatalf("expected trace source_event_hash, got %#v", capturedArgs[38])
	}
}

func TestRecordTraceEvent_ProviderResponseMissingUpstreamRequestIDCaptureGap(t *testing.T) {
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
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	route := &RouteResult{
		Channel:     models.Channel{Provider: "openai", Name: "primary"},
		ChannelKey:  models.ChannelKey{ID: 11},
		MappedModel: "gpt-4o",
	}

	relay.recordTraceEvent(ctx, "req-trace", "provider.response_accepted", "provider", "info", "company-chat", "company-chat", route, http.StatusOK, 25, map[string]interface{}{}, "")

	if len(capturedArgs) != 39 {
		t.Fatalf("expected 39 trace insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[24] != evidenceCaptureGapProvider {
		t.Fatalf("expected provider_not_returned capture_gap, got %#v", capturedArgs[24])
	}
	if capturedArgs[26] != nil {
		t.Fatalf("expected nil upstream_request_id, got %#v", capturedArgs[26])
	}
	if capturedArgs[27] != "provider" || capturedArgs[28] != "selected" {
		t.Fatalf("unexpected provider decision args: %#v", capturedArgs[27:29])
	}
	if hash, ok := capturedArgs[38].(string); !ok || len(hash) != 64 {
		t.Fatalf("expected trace source_event_hash, got %#v", capturedArgs[38])
	}
}

func TestRecordTraceEvent_ProviderResponseRedactionGapTakesPriority(t *testing.T) {
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
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	route := &RouteResult{
		Channel:     models.Channel{Provider: "openai", Name: "primary"},
		ChannelKey:  models.ChannelKey{ID: 11},
		MappedModel: "gpt-4o",
	}

	relay.recordTraceEvent(ctx, "req-trace", "provider.response_accepted", "provider", "info", "company-chat", "company-chat", route, http.StatusOK, 25, map[string]interface{}{
		"api_key": "sk-secret",
	}, "")

	if len(capturedArgs) != 39 {
		t.Fatalf("expected 39 trace insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[24] != "redacted" {
		t.Fatalf("expected redacted capture_gap to take priority, got %#v", capturedArgs[24])
	}
	if capturedArgs[26] != nil {
		t.Fatalf("expected nil upstream_request_id, got %#v", capturedArgs[26])
	}
}

func TestRecordTraceEvent_ProviderFailureMissingUpstreamRequestIDCaptureGap(t *testing.T) {
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
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	route := &RouteResult{
		Channel:     models.Channel{Provider: "openai", Name: "primary"},
		ChannelKey:  models.ChannelKey{ID: 11},
		MappedModel: "gpt-4o",
	}

	relay.recordTraceEvent(ctx, "req-trace", "provider.request_failed", "provider", "warning", "company-chat", "company-chat", route, 0, 25, map[string]interface{}{
		"failure_kind": string(adapters.FailureChannel),
	})

	if len(capturedArgs) != 39 {
		t.Fatalf("expected 39 trace insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[24] != evidenceCaptureGapSource {
		t.Fatalf("expected not_available_from_source capture_gap, got %#v", capturedArgs[24])
	}
	if capturedArgs[26] != nil {
		t.Fatalf("expected nil upstream_request_id, got %#v", capturedArgs[26])
	}
	if capturedArgs[27] != "provider" || capturedArgs[28] != "failed" || capturedArgs[29] != string(adapters.FailureChannel) {
		t.Fatalf("unexpected provider decision args: %#v", capturedArgs[27:30])
	}
	if hash, ok := capturedArgs[38].(string); !ok || len(hash) != 64 {
		t.Fatalf("expected trace source_event_hash, got %#v", capturedArgs[38])
	}
}

func TestRecordTraceEvent_ProviderFailureRedactionGapTakesPriority(t *testing.T) {
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
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	route := &RouteResult{
		Channel:     models.Channel{Provider: "openai", Name: "primary"},
		ChannelKey:  models.ChannelKey{ID: 11},
		MappedModel: "gpt-4o",
	}

	relay.recordTraceEvent(ctx, "req-trace", "provider.request_build_failed", "provider", "error", "company-chat", "company-chat", route, 0, 25, map[string]interface{}{
		"failure_kind": "request_build",
		"api_key":      "sk-secret",
	})

	if len(capturedArgs) != 39 {
		t.Fatalf("expected 39 trace insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[24] != "redacted" {
		t.Fatalf("expected redacted capture_gap to take priority, got %#v", capturedArgs[24])
	}
	if capturedArgs[26] != nil {
		t.Fatalf("expected nil upstream_request_id, got %#v", capturedArgs[26])
	}
	if capturedArgs[27] != "provider" || capturedArgs[28] != "failed" || capturedArgs[29] != "request_build" {
		t.Fatalf("unexpected provider decision args: %#v", capturedArgs[27:30])
	}
}

func TestRecordTraceEvent_ModelNotAllowedCaptureGapAndDecision(t *testing.T) {
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
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))

	relay.recordTraceEvent(ctx, "req-auth", "auth.model_not_allowed", "auth", "warning", "gpt-4o", "company-chat", nil, http.StatusForbidden, 7, map[string]interface{}{
		"error_code": "auth_error.model_not_allowed",
	})

	if len(capturedArgs) != 39 {
		t.Fatalf("expected 39 trace insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[8] != "auth.model_not_allowed" || capturedArgs[9] != "auth" || capturedArgs[22] != "auth" {
		t.Fatalf("unexpected auth event args: %#v", capturedArgs[8:23])
	}
	if capturedArgs[24] != evidenceCaptureGapAuth {
		t.Fatalf("expected permission_denied capture_gap, got %#v", capturedArgs[24])
	}
	if capturedArgs[27] != "auth" || capturedArgs[28] != "rejected" || capturedArgs[29] != "auth_error.model_not_allowed" {
		t.Fatalf("unexpected auth decision args: %#v", capturedArgs[27:30])
	}
	if hash, ok := capturedArgs[38].(string); !ok || len(hash) != 64 {
		t.Fatalf("expected trace source_event_hash, got %#v", capturedArgs[38])
	}
}

func TestRecordTraceEvent_UsesTrustedEvidenceContext(t *testing.T) {
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
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = traceutil.ContextWithEvidence(ctx, traceutil.EvidenceContext{
		TraceID:      "trace-internal-1",
		ParentSpanID: "parent-span-1",
		SessionID:    "session-42",
		WorkflowID:   "workflow-42",
		AgentID:      "agent-42",
		ActorType:    "agent",
		ActorRef:     "agent:planner",
		OnBehalfOf:   "user:42",
	})

	relay.recordTraceEvent(ctx, "req-trace", "gateway.request.accepted", "gateway", "info", "company-chat", "company-chat", nil, http.StatusOK, 25, nil)

	if len(capturedArgs) != 39 {
		t.Fatalf("expected 39 trace insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[18] != "trace-internal-1" || capturedArgs[20] != "parent-span-1" {
		t.Fatalf("expected trusted trace/parent span args, got %#v", capturedArgs[18:21])
	}
	if capturedArgs[30] != "agent" || capturedArgs[31] != "agent:planner" || capturedArgs[32] != "user:42" || capturedArgs[33] != "session-42" {
		t.Fatalf("expected trusted actor/delegation/session args, got %#v", capturedArgs[30:34])
	}
	if capturedArgs[34] != "workflow-42" || capturedArgs[35] != "agent-42" {
		t.Fatalf("expected trusted workflow/agent args, got %#v", capturedArgs[34:36])
	}
}

func TestRecordTraceEvent_ParentsEventsToGatewayRootSpan(t *testing.T) {
	var inserts [][]any
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO request_trace_events") {
				return pgconn.CommandTag{}, nil
			}
			inserts = append(inserts, append([]any(nil), args...))
			return pgconn.CommandTag{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})
	relay.SetEvidenceContextTrustSecret("shared-secret-123456")
	header := http.Header{}
	header.Set(traceutil.EvidenceContextAuthHeader, "shared-secret-123456")
	header.Set(traceutil.EvidenceContextTraceIDHeader, "trace-chain-1")
	header.Set(traceutil.EvidenceContextParentSpanHeader, "incoming-parent-1")
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = relay.contextWithTrustedEvidence(ctx, header)

	relay.recordTraceEvent(ctx, "req-chain", "gateway.request.accepted", "gateway", "info", "company-chat", "company-chat", nil, http.StatusOK, 10, nil)
	relay.recordTraceEvent(ctx, "req-chain", "routing.route_selected", "routing", "info", "company-chat", "company-chat", nil, http.StatusOK, 20, nil)
	relay.recordTraceEvent(ctx, "req-chain", "routing.no_route", "routing", "error", "company-chat", "company-chat", nil, http.StatusServiceUnavailable, 30, nil)

	if len(inserts) != 3 {
		t.Fatalf("expected three trace inserts, got %d", len(inserts))
	}
	rootSpanID, ok := inserts[0][19].(string)
	if !ok || len(rootSpanID) != 16 {
		t.Fatalf("expected generated root span ID, got %#v", inserts[0][19])
	}
	routingSpanID, ok := inserts[1][19].(string)
	if !ok || len(routingSpanID) != 16 {
		t.Fatalf("expected generated routing span ID, got %#v", inserts[1][19])
	}
	if inserts[0][18] != "trace-chain-1" || inserts[0][20] != "incoming-parent-1" {
		t.Fatalf("expected first event to preserve trusted trace/parent, got %#v", inserts[0][18:21])
	}
	if inserts[1][18] != "trace-chain-1" || inserts[1][20] != rootSpanID {
		t.Fatalf("expected second event to parent to gateway root span, got %#v root=%s", inserts[1][18:21], rootSpanID)
	}
	if inserts[1][22] != "routing" || inserts[2][22] != "routing" {
		t.Fatalf("expected routing source components, got %#v and %#v", inserts[1][22], inserts[2][22])
	}
	if inserts[2][18] != "trace-chain-1" || inserts[2][20] != routingSpanID {
		t.Fatalf("expected repeated routing event to parent to routing component span, got %#v routing=%s", inserts[2][18:21], routingSpanID)
	}
}

func TestRecordPayloadPolicyDecision_DisabledMode(t *testing.T) {
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
	relay.SetPayloadCaptureMode(traceutil.PayloadCaptureModeDisabled)
	ctx := contextWithEvidenceSpanState(context.WithValue(context.Background(), middleware.CtxOrgID, int64(1)))

	relay.recordPayloadPolicyDecision(ctx, "req-payload", "company-chat", "company-chat", 12)

	if len(capturedArgs) != 39 {
		t.Fatalf("expected 39 trace insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[8] != "payload.policy.applied" || capturedArgs[9] != "payload_policy" || capturedArgs[22] != "payload_policy" {
		t.Fatalf("unexpected payload policy event args: %#v", capturedArgs[8:23])
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(capturedArgs[17].(string)), &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["payload_capture_mode"] != traceutil.PayloadCaptureModeDisabled || metadata["payload_policy_reason"] != "payload.disabled" {
		t.Fatalf("expected disabled payload policy metadata, got %#v", metadata)
	}
	if metadata["raw_payload_stored"] != false || metadata["payload_ref_present"] != false {
		t.Fatalf("expected raw/ref payload booleans false, got %#v", metadata)
	}
	if capturedArgs[24] != nil {
		t.Fatalf("expected no capture gap for payload policy metadata, got %#v", capturedArgs[24])
	}
	if capturedArgs[27] != "payload_capture" || capturedArgs[28] != "skipped" || capturedArgs[29] != "payload.disabled" {
		t.Fatalf("unexpected payload policy decision args: %#v", capturedArgs[27:30])
	}
	if capturedArgs[37] != traceutil.PayloadCaptureModeDisabled {
		t.Fatalf("expected disabled payload capture mode column, got %#v", capturedArgs[37])
	}
}

func TestRecordPayloadPolicyDecision_MetadataOnlyMode(t *testing.T) {
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
	ctx := contextWithEvidenceSpanState(context.WithValue(context.Background(), middleware.CtxOrgID, int64(1)))

	relay.recordPayloadPolicyDecision(ctx, "req-payload", "company-chat", "company-chat", 12)

	if len(capturedArgs) != 39 {
		t.Fatalf("expected 39 trace insert args, got %d", len(capturedArgs))
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(capturedArgs[17].(string)), &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["payload_capture_mode"] != traceutil.PayloadCaptureModeMetadataOnly || metadata["payload_policy_reason"] != "payload.metadata_only" {
		t.Fatalf("expected metadata-only payload policy metadata, got %#v", metadata)
	}
	if metadata["raw_payload_stored"] != false || metadata["payload_ref_present"] != false {
		t.Fatalf("expected raw/ref payload booleans false, got %#v", metadata)
	}
	if capturedArgs[27] != "payload_capture" || capturedArgs[28] != "allowed" || capturedArgs[29] != "payload.metadata_only" {
		t.Fatalf("unexpected payload policy decision args: %#v", capturedArgs[27:30])
	}
	if capturedArgs[37] != traceutil.PayloadCaptureModeMetadataOnly {
		t.Fatalf("expected metadata-only payload capture mode column, got %#v", capturedArgs[37])
	}
}

func TestRelayContextWithTrustedEvidence(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})
	relay.SetEvidenceContextTrustSecret("shared-secret-123456")
	header := http.Header{}
	header.Set(traceutil.EvidenceContextAuthHeader, "shared-secret-123456")
	header.Set(traceutil.EvidenceContextTraceIDHeader, "trace-internal-1")

	ctx := relay.contextWithTrustedEvidence(context.Background(), header)
	if got := traceutil.EvidenceFromContext(ctx); got.TraceID != "trace-internal-1" {
		t.Fatalf("expected trusted trace id, got %#v", got)
	}
	if state, _ := ctx.Value(evidenceSpanStateKey{}).(*evidenceSpanState); state == nil {
		t.Fatal("expected evidence span state on request context")
	}
}

func TestP6F17HundredChildOperationsShareTraceWithoutIdentityCollision(t *testing.T) {
	var inserts [][]any
	db := &testutil.MockDB{
		ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO request_trace_events") {
				inserts = append(inserts, append([]any(nil), args...))
			}
			return pgconn.CommandTag{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})
	operations := map[string]struct{}{}
	const sharedTraceID = "trace-agent-tree-100"
	const sharedWorkflowID = "workflow-agent-tree-100"

	for i := 0; i < 100; i++ {
		operationID, err := middleware.NewOperationID()
		if err != nil {
			t.Fatalf("generate operation identity: %v", err)
		}
		ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
		ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(11))
		ctx = context.WithValue(ctx, middleware.CtxRequestID, operationID)
		ctx = traceutil.ContextWithEvidence(ctx, traceutil.EvidenceContext{
			TraceID:      sharedTraceID,
			ParentSpanID: "parent-agent-supervisor",
			WorkflowID:   sharedWorkflowID,
			AgentID:      fmt.Sprintf("agent-child-%03d", i),
			ActorType:    "agent",
			ActorRef:     fmt.Sprintf("agent:child:%03d", i),
		})
		relay.recordTraceEvent(ctx, operationID, "gateway.request.accepted", "gateway", "info", "gpt-4o", "gpt-4o", nil, 0, i, nil)
	}

	if len(inserts) != 100 {
		t.Fatalf("expected 100 child trace rows, got %d", len(inserts))
	}
	for index, args := range inserts {
		operationID, _ := args[0].(string)
		if !strings.HasPrefix(operationID, "op_") {
			t.Fatalf("child %d missing server operation identity: %#v", index, args[0])
		}
		if _, exists := operations[operationID]; exists {
			t.Fatalf("duplicate child operation identity %q", operationID)
		}
		operations[operationID] = struct{}{}
		if args[18] != sharedTraceID || args[34] != sharedWorkflowID || args[35] != fmt.Sprintf("agent-child-%03d", index) {
			t.Fatalf("child %d identity chain mismatch: trace=%#v workflow=%#v agent=%#v", index, args[18], args[34], args[35])
		}
	}
}

func TestP6F17ProviderAttemptsKeepOperationAndUseDistinctAttemptIDs(t *testing.T) {
	var inserts [][]any
	db := &testutil.MockDB{
		ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO request_trace_events") {
				inserts = append(inserts, append([]any(nil), args...))
			}
			return pgconn.CommandTag{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{})
	base := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	base = context.WithValue(base, middleware.CtxRequestID, "op_00000000000000000000000000000001")
	for i := 0; i < 2; i++ {
		attemptCtx := contextWithAttemptID(base, newAttemptID())
		relay.recordTraceEvent(attemptCtx, "op_00000000000000000000000000000001", "provider.request_failed", "provider", "warning", "gpt-4o", "gpt-4o", nil, 0, i, map[string]interface{}{
			"failure_kind": string(adapters.FailureChannel),
		})
	}
	if len(inserts) != 2 {
		t.Fatalf("expected two provider attempt events, got %d", len(inserts))
	}
	firstAttempt, _ := inserts[0][36].(string)
	secondAttempt, _ := inserts[1][36].(string)
	if inserts[0][0] != inserts[1][0] || !strings.HasPrefix(firstAttempt, "att_") || !strings.HasPrefix(secondAttempt, "att_") || firstAttempt == secondAttempt {
		t.Fatalf("operation/attempt identity mismatch: op1=%#v op2=%#v attempt1=%#v attempt2=%#v", inserts[0][0], inserts[1][0], firstAttempt, secondAttempt)
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
	relay.recordRequest(context.Background(), "req-001", 1, 1, 1, "openai", "ch1", "gpt-4o", "gpt-4o", "127.0.0.1", 100, 50, 10, 200, 123, 0, false, metering.UsageSourceUpstream, 180, "")
}

// --- checkProjectBudgetAlert tests ---

func TestCheckProjectBudgetAlert_NilEventService(t *testing.T) {
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})
	relay.eventSvc = nil

	// Should not panic
	relay.checkProjectBudgetAlert(context.Background())
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

func TestUpstreamRequestIDFromHeader(t *testing.T) {
	header := http.Header{}
	header.Set("OpenAI-Request-ID", "  upstream-abc-123  ")

	if got := upstreamRequestIDFromHeader(header); got != "upstream-abc-123" {
		t.Fatalf("expected upstream request id, got %q", got)
	}

	header = http.Header{}
	header.Set("X-Request-Id", "bad value with spaces")
	if got := upstreamRequestIDFromHeader(header); got != "" {
		t.Fatalf("expected whitespace-containing request id to be rejected, got %q", got)
	}

	header = http.Header{}
	header.Set("Anthropic-Request-Id", "bad\nvalue")
	if got := upstreamRequestIDFromHeader(header); got != "" {
		t.Fatalf("expected control-character request id to be rejected, got %q", got)
	}

	header = http.Header{}
	header.Set("OpenAI-Request-ID", "上游请求")
	if got := upstreamRequestIDFromHeader(header); got != "" {
		t.Fatalf("expected non-ASCII request id to be rejected, got %q", got)
	}

	header = http.Header{}
	header.Set("OpenAI-Request-ID", strings.Repeat("a", 129))
	if got := upstreamRequestIDFromHeader(header); got != strings.Repeat("a", 128) {
		t.Fatalf("expected request id to be truncated to 128 bytes, got length %d", len(got))
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

func TestGatewayPropagatesTransportLifetimeToAgentConcurrencyLease(t *testing.T) {
	t.Run("stream chat", func(t *testing.T) {
		var captured time.Duration
		relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
			freezeFn: func(ctx context.Context, _ billing.FreezeRequest) (*billing.FreezeResult, error) {
				captured = agentcap.InvocationLeaseTTLFromContext(ctx)
				return &billing.FreezeResult{Success: false, ErrorCode: capbilling.ErrorCodeCostBudgetExceeded}, nil
			},
		})
		policy := relay.currentStreamPolicy()
		policy.MaxDurationSeconds = 15 * 60
		if err := relay.SetStreamPolicy(policy); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}`))
		ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
		ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
		ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-stream-lease")
		recorder := httptest.NewRecorder()
		relay.HandleChatCompletion(recorder, req.WithContext(ctx))
		if recorder.Code != http.StatusForbidden || captured != 15*time.Minute {
			t.Fatalf("status=%d lease=%s, want 403/15m", recorder.Code, captured)
		}
	})

	t.Run("embeddings", func(t *testing.T) {
		var captured time.Duration
		relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
			freezeFn: func(ctx context.Context, _ billing.FreezeRequest) (*billing.FreezeResult, error) {
				captured = agentcap.InvocationLeaseTTLFromContext(ctx)
				return &billing.FreezeResult{Success: false, ErrorCode: capbilling.ErrorCodeCostBudgetExceeded}, nil
			},
		})
		req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(`{"model":"text-embedding-3-small","input":"hello"}`))
		ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
		ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
		ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-embedding-lease")
		recorder := httptest.NewRecorder()
		relay.HandleEmbeddings(recorder, req.WithContext(ctx))
		if recorder.Code != http.StatusForbidden || captured != 120*time.Second {
			t.Fatalf("status=%d lease=%s, want 403/120s", recorder.Code, captured)
		}
	})
}

func TestHandleChatCompletion_NoMessages(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
	}
	relay := newTestRelay(db, &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, NewBalanceMicros: 900, Success: true}, nil
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
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, NewBalanceMicros: 900, Success: true}, nil
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

func TestModelAllowedForAPIKey(t *testing.T) {
	tests := []struct {
		name           string
		apiKey         *models.ApiKey
		model          string
		requestedModel string
		want           bool
	}{
		{name: "missing key context", want: true},
		{name: "empty restriction", apiKey: &models.ApiKey{}, model: "resolved", requestedModel: "alias", want: true},
		{name: "wildcard", apiKey: &models.ApiKey{AllowedModels: "*"}, model: "resolved", requestedModel: "alias", want: true},
		{name: "resolved model", apiKey: &models.ApiKey{AllowedModels: "other, resolved"}, model: "resolved", requestedModel: "alias", want: true},
		{name: "requested alias", apiKey: &models.ApiKey{AllowedModels: "other, alias"}, model: "resolved", requestedModel: "alias", want: true},
		{name: "not allowed", apiKey: &models.ApiKey{AllowedModels: "other"}, model: "resolved", requestedModel: "alias", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modelAllowedForAPIKey(tt.apiKey, tt.model, tt.requestedModel); got != tt.want {
				t.Fatalf("modelAllowedForAPIKey()=%t want=%t", got, tt.want)
			}
		})
	}
}

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

func TestHandleEmbeddings_ModelNotAllowed(t *testing.T) {
	var capturedArgs []any
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO request_trace_events") {
				capturedArgs = append([]any(nil), args...)
			}
			return pgconn.CommandTag{}, nil
		},
	}
	freezeCalled := false
	relay := newTestRelay(db, &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezeCalled = true
			return &billing.FreezeResult{Success: true}, nil
		},
	})

	body := `{"model":"text-embedding-3-small","input":"hello world"}`
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-embeddings-denied")
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{
		ID:            1,
		OrgID:         1,
		Status:        "active",
		AllowedModels: "text-embedding-ada-002",
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleEmbeddings(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"auth_error.model_not_allowed"`) {
		t.Fatalf("expected model permission error code, got %s", rec.Body.String())
	}
	if rec.Header().Get("X-Request-ID") != "req-embeddings-denied" {
		t.Fatalf("expected response request ID, got %q", rec.Header().Get("X-Request-ID"))
	}
	if freezeCalled {
		t.Fatal("expected model permission denial before billing freeze")
	}
	if len(capturedArgs) != 39 {
		t.Fatalf("expected model denial trace insert, got %d args", len(capturedArgs))
	}
	if capturedArgs[8] != "auth.model_not_allowed" || capturedArgs[9] != "auth" || capturedArgs[22] != "auth" {
		t.Fatalf("unexpected model denial trace args: %#v", capturedArgs[8:23])
	}
	if capturedArgs[24] != evidenceCaptureGapAuth {
		t.Fatalf("expected permission_denied capture_gap, got %#v", capturedArgs[24])
	}
	if capturedArgs[27] != "auth" || capturedArgs[28] != "rejected" || capturedArgs[29] != "auth_error.model_not_allowed" {
		t.Fatalf("unexpected model denial decision args: %#v", capturedArgs[27:30])
	}
}

func TestHandleEmbeddings_ModelAllowedByRequestedAlias(t *testing.T) {
	freezeCalled := false
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezeCalled = true
			return &billing.FreezeResult{Success: false, ErrorCode: "insufficient_balance"}, nil
		},
	})

	body := `{"model":"text-embedding-3-small","input":"hello world"}`
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-embeddings-alias")
	ctx = context.WithValue(ctx, middleware.CtxModelAlias, "company-embedding")
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{
		ID:            1,
		OrgID:         1,
		Status:        "active",
		AllowedModels: "company-embedding",
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	relay.HandleEmbeddings(rec, req)

	if !freezeCalled {
		t.Fatal("expected requested alias to pass model permission and reach billing freeze")
	}
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected existing billing response after alias permission, got %d: %s", rec.Code, rec.Body.String())
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
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, NewBalanceMicros: 900, Success: true}, nil
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
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (int64, int64, error) {
			return 50_000_000, 150_000_000, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalled = true
			return &billing.SettleResult{SettleID: 1, RefundMicros: 5, NewBalanceMicros: 995, Success: true}, nil
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
	requestCtx := contextWithAttemptID(context.Background(), "att_fallback_test")
	requestCtx = contextWithRouteAttempt(requestCtx, 2, 3)
	relay.relayNonStreamRequest(requestCtx, rec, resp, route, "req-001", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{RequestID: "req-001", OrgID: 1}), time.Now(), nil, 1200, 0, "")

	if !settleCalled {
		t.Fatal("expected settle to be called")
	}
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Check _uniroute extension field
	var resp2 map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp2)
	uniroute, ok := resp2["_uniroute"].(map[string]interface{})
	if !ok {
		t.Fatal("expected _uniroute in response")
	}
	if uniroute["fallback"] != true || uniroute["attempt_count"] != float64(2) || uniroute["route_count"] != float64(3) || uniroute["attempt_id"] != "att_fallback_test" {
		t.Fatalf("expected truthful fallback identity metadata, got %#v", uniroute)
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
	relay.relayNonStreamRequest(context.Background(), rec, resp, route, "req-retry", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{RequestID: "req-retry", OrgID: 1}), time.Now(), nil, 1200, 0, "")

	if settleCalls != billingSettleMaxAttempts {
		t.Fatalf("expected %d settle attempts, got %d", billingSettleMaxAttempts, settleCalls)
	}
	if rec.Code != 200 {
		t.Fatalf("expected 200 after retry success, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRelayNonStreamRequest_PostDeliverySettleErrorRetainsRecovery(t *testing.T) {
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
	relay.relayNonStreamRequest(context.Background(), rec, resp, route, "req-002", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{RequestID: "req-002", OrgID: 1}), time.Now(), nil, 1200, 0, "")

	if settleCalls != billingSettleMaxAttempts {
		t.Fatalf("expected %d settle attempts, got %d", billingSettleMaxAttempts, settleCalls)
	}
	if cancelCalled {
		t.Fatal("delivered output must retain settlement recovery instead of cancelling")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("local write already completed before transient settle failure: %d: %s", rec.Code, rec.Body.String())
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
		queryModelPriceFn: func(ctx context.Context, orgID int64, model string) (int64, int64, error) {
			return 50_000_000, 150_000_000, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalled = true
			if req.PromptTokens != 10 {
				t.Errorf("expected promptTokens=10, got %d", req.PromptTokens)
			}
			if req.CompletionTokens != 8 {
				t.Errorf("expected completionTokens=8, got %d", req.CompletionTokens)
			}
			return &billing.SettleResult{SettleID: 1, RefundMicros: 0, NewBalanceMicros: 9990, Success: true}, nil
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
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-1", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "Hi"}}, logger.New(logger.Fields{RequestID: "req-stream-1", OrgID: 1}), time.Now(), 1000, nil, "")

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
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-2", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "Hello"}}, logger.New(logger.Fields{RequestID: "req-stream-2", OrgID: 1}), time.Now(), 1000, nil, "")

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
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-ant-missing-usage", 1, 1, "company-chat", "company-chat", "127.0.0.1", []Message{{Role: "user", Content: "Hello"}}, logger.New(logger.Fields{RequestID: "req-ant-missing-usage", OrgID: 1}), time.Now(), 1000, adapters.NewAnthropicNative(), "")

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
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-3", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-stream-3", OrgID: 1}), time.Now(), 1000, nil, "")

	if settleCalls != billingSettleMaxAttempts {
		t.Fatalf("expected %d settle attempts, got %d", billingSettleMaxAttempts, settleCalls)
	}
	if cancelCalled {
		t.Fatal("billable stream output must remain pending and must not be cancelled")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatal("expected [DONE] even after settle error")
	}
	if !strings.Contains(body, `"billing_status":"settlement_pending_recovery"`) {
		t.Fatalf("expected settle failure billing status, got %s", body)
	}
	if !strings.Contains(body, `"charged_cost_micros":"0"`) {
		t.Fatalf("expected charged cost to remain pending until recovery, got %s", body)
	}
}

func TestP6F18BRelayStreamMarksOutputAndPersistsUsageBeforeSettlement(t *testing.T) {
	sequence := make([]string, 0, 3)
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 10000
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		markOutputFn: func(ctx context.Context, requestID string, orgID int64) error {
			sequence = append(sequence, "mark")
			return nil
		},
		persistUsageFn: func(ctx context.Context, req billing.SettleRequest) error {
			sequence = append(sequence, "persist")
			if req.PromptTokens != 12 || req.CompletionTokens != 4 {
				t.Fatalf("unexpected persisted usage: %+v", req)
			}
			return nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			sequence = append(sequence, "settle")
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 2, ChargedCostMicros: 2, Success: true}, nil
		},
	}
	relay := newTestRelay(db, b)
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"billable\"}}]}\n\n" +
		"data: {\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4}}\n\n" +
		"data: [DONE]\n\n"
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}

	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-p6f18b-order", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-p6f18b-order", OrgID: 1}), time.Now(), 1000, nil, "")

	if got, want := strings.Join(sequence, ","), "mark,persist,settle"; got != want {
		t.Fatalf("unexpected billing order: got %s want %s", got, want)
	}
	if !strings.Contains(rec.Body.String(), "billable") || !strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Fatalf("expected completed stream, got %s", rec.Body.String())
	}
}

func TestP6F18BRelayStreamWithholdsContentWhenOutputMarkerFails(t *testing.T) {
	cancelCalls := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 10000
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		markOutputFn: func(ctx context.Context, requestID string, orgID int64) error {
			return fmt.Errorf("database unavailable")
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalls++
			return nil
		},
		persistUsageFn: func(ctx context.Context, req billing.SettleRequest) error {
			t.Fatal("usage must not be persisted without an output marker")
			return nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			t.Fatal("settlement must not run without an output marker")
			return nil, nil
		},
	}
	relay := newTestRelay(db, b)
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"must-not-leak\"}}]}\n\ndata: [DONE]\n\n"))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}

	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-p6f18b-marker-fail", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-p6f18b-marker-fail", OrgID: 1}), time.Now(), 1000, nil, "")

	if cancelCalls != 1 {
		t.Fatalf("expected one safe pre-output cancellation, got %d", cancelCalls)
	}
	body := rec.Body.String()
	if strings.Contains(body, "must-not-leak") {
		t.Fatalf("model content crossed a failed billing marker: %s", body)
	}
	if !strings.Contains(body, "Unable to establish durable billing state") {
		t.Fatalf("expected durable billing error, got %s", body)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("failed stream must not advertise a completed terminal: %s", body)
	}
}

func TestRelayStreamNoSpaceDataUsesProtectedOutputPath(t *testing.T) {
	markCalls := 0
	cancelCalls := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 10000
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		markOutputFn: func(ctx context.Context, requestID string, orgID int64) error {
			markCalls++
			return fmt.Errorf("database unavailable")
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalls++
			return nil
		},
		persistUsageFn: func(ctx context.Context, req billing.SettleRequest) error {
			t.Fatal("usage must not be persisted without an output marker")
			return nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			t.Fatal("settlement must not run without an output marker")
			return nil, nil
		},
	}
	relay := newTestRelay(db, b)
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data:{\"choices\":[{\"delta\":{\"content\":\"must-not-leak\"}}]}\n\ndata:[DONE]\n\n"))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}

	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-sse-no-space-marker-fail", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-sse-no-space-marker-fail", OrgID: 1}), time.Now(), 1000, nil, "")

	if markCalls != 1 {
		t.Fatalf("no-space data field bypassed the protected output marker: calls=%d body=%s", markCalls, rec.Body.String())
	}
	if cancelCalls != 1 {
		t.Fatalf("expected one safe pre-output cancellation, got %d", cancelCalls)
	}
	body := rec.Body.String()
	if strings.Contains(body, "must-not-leak") {
		t.Fatalf("no-space model content crossed a failed billing marker: %s", body)
	}
	if !strings.Contains(body, "Unable to establish durable billing state") || strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected protected stream failure, got %s", body)
	}
}

func TestP6F18BRelayStreamDoesNotCancelWhenFinalUsagePersistenceFails(t *testing.T) {
	persistCalls := 0
	cancelCalls := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 10000
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		persistUsageFn: func(ctx context.Context, req billing.SettleRequest) error {
			persistCalls++
			return fmt.Errorf("database unavailable")
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalls++
			return nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			t.Fatal("settlement must not run before durable usage persistence")
			return nil, nil
		},
	}
	relay := newTestRelay(db, b)
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"delivered\"}}]}\n\ndata: [DONE]\n\n"))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}

	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-p6f18b-persist-fail", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-p6f18b-persist-fail", OrgID: 1}), time.Now(), 1000, nil, "")

	if persistCalls != billingSettleMaxAttempts {
		t.Fatalf("expected %d persistence attempts, got %d", billingSettleMaxAttempts, persistCalls)
	}
	if cancelCalls != 0 {
		t.Fatalf("delivered output must never be cancelled, got %d calls", cancelCalls)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "delivered") || !strings.Contains(body, "settlement_pending_usage_unpersisted") {
		t.Fatalf("expected delivered content and pending reconciliation error, got %s", body)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("usage persistence failure must not advertise a completed terminal: %s", body)
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
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-4", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-stream-4", OrgID: 1}), time.Now(), 1000, nil, "")
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
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-5", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-stream-5", OrgID: 1}), time.Now(), 1000, nil, "")

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
	relay.relayStreamRequest(context.Background(), rec, resp, route, "req-stream-6", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", nil, logger.New(logger.Fields{RequestID: "req-stream-6", OrgID: 1}), time.Now(), 1000, nil, "")

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

func TestP6F40BHandleChatCompletionProviderAttemptPrecedesNonStreamDispatch(t *testing.T) {
	var attemptBegan atomic.Bool
	var upstreamBeforeLedger atomic.Bool
	var started billing.ProviderAttemptStart
	var completed billing.ProviderAttemptCompletion
	var settleReq billing.SettleRequest
	// Mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !attemptBegan.Load() {
			upstreamBeforeLedger.Store(true)
		}
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
	freezeResult := &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, NewBalanceMicros: 900, Success: true}
	settleResult := &billing.SettleResult{SettleID: 1, RefundMicros: 5, NewBalanceMicros: 905, Success: true}
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return freezeResult, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleReq = req
			return settleResult, nil
		},
		beginAttemptFn: func(ctx context.Context, req billing.ProviderAttemptStart) error {
			started = req
			attemptBegan.Store(true)
			return nil
		},
		completeAttemptFn: func(ctx context.Context, req billing.ProviderAttemptCompletion) error {
			completed = req
			return nil
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
	if upstreamBeforeLedger.Load() {
		t.Fatal("provider dispatch reached the network before the durable attempt boundary")
	}
	if started.RequestID != "req-e2e-1" || started.AttemptID == "" || started.Endpoint != string(adapters.EndpointChatCompletions) || started.ChannelKeyID != 1 {
		t.Fatalf("unexpected provider attempt start: %+v", started)
	}
	if completed.AttemptID != started.AttemptID || completed.Outcome != billing.ProviderAttemptOutcomeResponseAccepted || completed.HTTPStatus != http.StatusOK {
		t.Fatalf("unexpected provider attempt completion: %+v", completed)
	}
	if settleReq.AttemptID != started.AttemptID {
		t.Fatalf("settlement lost provider attempt authority: start=%q settle=%q", started.AttemptID, settleReq.AttemptID)
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
			"usage":{"input_tokens":10,"cache_read_input_tokens":7,"cache_creation_input_tokens":3,"output_tokens":5}
		}`))
	}))
	defer upstream.Close()

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, err := auth.EncryptKey([]byte("sk-ant-test"), encKey)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	var settleReq billing.SettleRequest
	var callLogArgs []any
	var settledTraceMetadata map[string]interface{}
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
			if strings.Contains(sql, "INSERT INTO call_logs") {
				callLogArgs = append([]any(nil), args...)
			}
			if strings.Contains(sql, "INSERT INTO request_trace_events") && len(args) == 39 && args[8] == "billing.settle.succeeded" {
				if err := json.Unmarshal([]byte(args[17].(string)), &settledTraceMetadata); err != nil {
					t.Fatalf("decode settled trace metadata: %v", err)
				}
			}
			return pgconn.CommandTag{}, nil
		},
	}
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, NewBalanceMicros: 900, Success: true}, nil
		},
		prepareDeliveryFn: func(ctx context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			return &billing.PrepareDeliveryResult{Funded: true, Preview: billing.SettleResult{
				ActualCostMicros: 1, ChargedCostMicros: 1, Success: true,
				PricingVersion: "pv2_test", CacheReadRatio: 0.1, CacheCreationRatio: 1.25,
				EffectiveInputPriceMicrosPerMillion: 20_000_000, EffectiveCacheReadPriceMicrosPerMillion: 2_000_000,
				EffectiveCacheCreationPriceMicrosPerMillion: 25_000_000, EffectiveOutputPriceMicrosPerMillion: 100_000_000,
				CacheReadCostMicros: 14, CacheCreationCostMicros: 75,
			}}, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleReq = req
			return &billing.SettleResult{
				SettleID: 1, RefundMicros: 0, NewBalanceMicros: 900, Success: true,
				PricingVersion: "pv2_test", CacheReadRatio: 0.1, CacheCreationRatio: 1.25,
				EffectiveInputPriceMicrosPerMillion: 20_000_000, EffectiveCacheReadPriceMicrosPerMillion: 2_000_000,
				EffectiveCacheCreationPriceMicrosPerMillion: 25_000_000, EffectiveOutputPriceMicrosPerMillion: 100_000_000,
				CacheReadCostMicros: 14, CacheCreationCostMicros: 75,
			}, nil
		},
	}
	relay := &Relay{
		db: db, router: NewRouter(db), billing: b, encKey: encKey,
		traceEventsEnabled: true, traceEventSampleRate: 1,
	}

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
	if settleReq.PromptTokens != 20 || settleReq.UncachedPromptTokens != 10 || settleReq.CacheReadTokens != 7 || settleReq.CacheCreationTokens != 3 || settleReq.CompletionTokens != 5 || settleReq.UsageSource != metering.UsageSourceUpstream {
		t.Fatalf("unexpected settle request: %+v", settleReq)
	}
	if settleReq.CacheUsageSchema != adapters.CacheUsageSchemaAnthropicV1 || settleReq.CacheUsageStatus != adapters.CacheUsageStatusReadWriteReported {
		t.Fatalf("unexpected authoritative cache contract: %+v", settleReq)
	}
	if len(callLogArgs) != callLogInsertArgCount || callLogArgs[callLogArgPromptTokens] != 20 || callLogArgs[callLogArgUncachedTokens] != 10 || callLogArgs[callLogArgCacheReadTokens] != 7 || callLogArgs[callLogArgCacheCreateTokens] != 3 {
		t.Fatalf("cache partition was not persisted in call log: %#v", callLogArgs)
	}
	if callLogArgs[callLogArgPricingVersion] != "pv2_test" {
		t.Fatalf("pricing snapshot was not persisted in call log: %#v", callLogArgs)
	}
	if settledTraceMetadata["cache_usage_schema"] != adapters.CacheUsageSchemaAnthropicV1 ||
		settledTraceMetadata["cache_read_tokens"] != float64(7) ||
		settledTraceMetadata["cache_creation_tokens"] != float64(3) ||
		settledTraceMetadata["cache_read_ratio"] != 0.1 ||
		settledTraceMetadata["pricing_version"] != "pv2_test" {
		t.Fatalf("prompt-cache evidence was not retained in the sanitized settlement trace: %#v", settledTraceMetadata)
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
	extension := resp["_uniroute"].(map[string]interface{})
	if extension["cache_hit"] != true {
		t.Fatalf("expected authoritative cache hit, got %v", extension)
	}
	providerCache := extension["provider_prompt_cache"].(map[string]interface{})
	if providerCache["cache_read_tokens"] != float64(7) || providerCache["cache_creation_tokens"] != float64(3) || providerCache["cache_read_ratio"] != 0.1 {
		t.Fatalf("unexpected provider prompt-cache extension: %#v", providerCache)
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
				"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"model\":\"claude-3-5-sonnet-20241022\",\"usage\":{\"input_tokens\":11,\"cache_read_input_tokens\":4,\"cache_creation_input_tokens\":2,\"output_tokens\":0}}}\n\n" +
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
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, NewBalanceMicros: 900, Success: true}, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleReq = req
			return &billing.SettleResult{SettleID: 1, RefundMicros: 0, NewBalanceMicros: 900, PricingVersion: "pv2_stream", CacheReadRatio: 0.1, CacheCreationRatio: 1.25, Success: true}, nil
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
	if settleReq.PromptTokens != 17 || settleReq.UncachedPromptTokens != 11 || settleReq.CacheReadTokens != 4 || settleReq.CacheCreationTokens != 2 || settleReq.CompletionTokens != 6 || settleReq.UsageSource != metering.UsageSourceUpstream {
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
	if !strings.Contains(responseBody, `"provider_prompt_cache"`) || !strings.Contains(responseBody, `"cache_read_tokens":4`) || !strings.Contains(responseBody, `"cache_creation_tokens":2`) || !strings.Contains(responseBody, `"cache_hit":true`) {
		t.Fatalf("expected authoritative prompt-cache evidence in final chunk, got %s", responseBody)
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

func chatCompletionRouteRow(id int64, name, provider, baseURL, encryptedKey string, weight int) func(dest ...any) error {
	return func(dest ...any) error {
		*dest[0].(*int64) = id
		*dest[1].(*string) = name
		*dest[2].(*string) = provider
		*dest[3].(*string) = baseURL
		*dest[4].(*string) = "gpt-4o"
		*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
		*dest[6].(*string) = adapters.TypeOpenAICompatible
		*dest[7].(*json.RawMessage) = json.RawMessage(`{}`)
		*dest[8].(*int) = 128000
		*dest[9].(*int) = 4096
		*dest[10].(*int) = weight
		*dest[11].(*string) = "active"
		*dest[12].(*string) = "healthy"
		*dest[13].(**time.Time) = nil
		*dest[14].(*int) = 0
		*dest[15].(*int64) = id
		*dest[16].(*string) = encryptedKey
		*dest[17].(*string) = "active"
		*dest[18].(**time.Time) = nil
		return nil
	}
}

func TestHandleChatCompletion_RetryClassification(t *testing.T) {
	tests := []struct {
		name             string
		upstreamStatus   int
		expectedStatus   int
		expectedRetry    bool
		expectedDecision string
	}{
		{name: "client 400 stops and preserves status", upstreamStatus: http.StatusBadRequest, expectedStatus: http.StatusBadRequest, expectedDecision: "stop"},
		{name: "provider auth 401 stops with safe gateway error", upstreamStatus: http.StatusUnauthorized, expectedStatus: http.StatusBadGateway, expectedDecision: "stop"},
		{name: "unknown 501 stops with safe gateway error", upstreamStatus: http.StatusNotImplemented, expectedStatus: http.StatusBadGateway, expectedDecision: "stop"},
		{name: "rate limit 429 fails over", upstreamStatus: http.StatusTooManyRequests, expectedStatus: http.StatusOK, expectedRetry: true, expectedDecision: "retry"},
		{name: "transient 503 fails over", upstreamStatus: http.StatusServiceUnavailable, expectedStatus: http.StatusOK, expectedRetry: true, expectedDecision: "retry"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const secretMarker = "provider-secret-diagnostic"
			firstCalls := 0
			firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				firstCalls++
				w.Header().Set("X-Request-ID", "upstream-first")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.upstreamStatus)
				_, _ = w.Write([]byte(`{"error":{"message":"` + secretMarker + `"}}`))
			}))
			defer firstUpstream.Close()

			secondCalls := 0
			secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				secondCalls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"chatcmpl-retry","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			}))
			defer secondUpstream.Close()

			encKey := []byte("0123456789abcdef01234567")
			encryptedKey, err := auth.EncryptKey([]byte("sk-test-key"), encKey)
			if err != nil {
				t.Fatalf("encrypt key: %v", err)
			}

			var rejectedMetadata map[string]interface{}
			callLogCount := 0
			var failedCallArgs []any
			db := &testutil.MockDB{
				QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return &testutil.MockRows{Rows: []func(dest ...any) error{
						chatCompletionRouteRow(1, "primary", "openai", firstUpstream.URL, encryptedKey, 90),
						chatCompletionRouteRow(2, "secondary", "azure", secondUpstream.URL, encryptedKey, 100),
					}}, nil
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					switch {
					case strings.Contains(sql, "INSERT INTO request_trace_events") && len(args) == 39 && args[8] == "provider.response_rejected":
						if err := json.Unmarshal([]byte(args[17].(string)), &rejectedMetadata); err != nil {
							t.Fatalf("decode rejected trace metadata: %v", err)
						}
					case strings.Contains(sql, "INSERT INTO call_logs"):
						callLogCount++
						if status, _ := args[16].(int); status != http.StatusOK {
							failedCallArgs = append([]any(nil), args...)
						}
					}
					return pgconn.CommandTag{}, nil
				},
			}

			freezeCalls := 0
			settleCalls := 0
			cancelCalls := 0
			b := &mockBilling{
				freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
					freezeCalls++
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
			relay := NewRelay(db, nil, NewRouter(db), b, nil, encKey)

			body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
			ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
			ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-retry-classification")
			ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, Status: "active"})
			rec := httptest.NewRecorder()

			relay.HandleChatCompletion(rec, req.WithContext(ctx))

			if rec.Code != test.expectedStatus {
				t.Fatalf("expected status %d, got %d: %s", test.expectedStatus, rec.Code, rec.Body.String())
			}
			if firstCalls != 1 {
				t.Fatalf("expected one primary attempt, got %d", firstCalls)
			}
			if test.expectedRetry && secondCalls != 1 {
				t.Fatalf("expected one failover attempt, got %d", secondCalls)
			}
			if !test.expectedRetry && secondCalls != 0 {
				t.Fatalf("non-retryable failure reached the second channel %d times", secondCalls)
			}
			if freezeCalls != 1 {
				t.Fatalf("expected one billing freeze, got %d", freezeCalls)
			}
			if test.expectedRetry {
				if settleCalls != 1 || cancelCalls != 0 || callLogCount != 1 {
					t.Fatalf("retry success billing/log invariant failed: settle=%d cancel=%d call_logs=%d", settleCalls, cancelCalls, callLogCount)
				}
			} else {
				if settleCalls != 0 || cancelCalls != 1 || callLogCount != 1 {
					t.Fatalf("non-retryable billing/log invariant failed: settle=%d cancel=%d call_logs=%d", settleCalls, cancelCalls, callLogCount)
				}
				if len(failedCallArgs) != callLogInsertArgCount || failedCallArgs[callLogArgProvider] != "openai" || failedCallArgs[callLogArgStatusCode] != test.upstreamStatus {
					t.Fatalf("unexpected failed attempt log: %#v", failedCallArgs)
				}
				if strings.Contains(rec.Body.String(), secretMarker) {
					t.Fatalf("downstream response leaked provider error body: %s", rec.Body.String())
				}
			}
			if rejectedMetadata["retryable"] != test.expectedRetry || rejectedMetadata["retry_decision"] != test.expectedDecision {
				t.Fatalf("unexpected retry trace metadata: %#v", rejectedMetadata)
			}
		})
	}
}

func TestP6F40BHandleChatCompletionRejectedThenAmbiguousTransportStops(t *testing.T) {
	// The first upstream returns an HTTP failure with an upstream request ID.
	// The final route fails at the network layer so its call log must not retain
	// the previous route's status or upstream identity.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "upstream-first-attempt")
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer upstream.Close()
	deadUpstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadUpstreamURL := deadUpstream.URL
	deadUpstream.Close()

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, _ := auth.EncryptKey([]byte("sk-test-key"), encKey)
	var capturedCallArgs []any

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
						*dest[10].(*int) = 90
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
					func(dest ...any) error {
						*dest[0].(*int64) = 2
						*dest[1].(*string) = "second-ch"
						*dest[2].(*string) = "azure"
						*dest[3].(*string) = deadUpstreamURL
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
						*dest[15].(*int64) = 2
						*dest[16].(*string) = encryptedKey
						*dest[17].(*string) = "active"
						*dest[18].(**time.Time) = nil
						return nil
					},
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO call_logs") {
				capturedCallArgs = append([]any(nil), args...)
			}
			return pgconn.CommandTag{}, nil
		},
	}

	cancelCalled := false
	var completions []billing.ProviderAttemptCompletion
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalled = true
			return nil
		},
		completeAttemptFn: func(ctx context.Context, req billing.ProviderAttemptCompletion) error {
			completions = append(completions, req)
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
	if cancelCalled {
		t.Fatal("ambiguous transport outcome must not use ordinary cancellation")
	}
	if len(completions) != 2 || completions[0].Outcome != billing.ProviderAttemptOutcomeRejected || completions[1].Outcome != billing.ProviderAttemptOutcomeUnknown {
		t.Fatalf("expected rejected then financially ambiguous attempts, got %#v", completions)
	}
	if len(capturedCallArgs) != callLogInsertArgCount || capturedCallArgs[callLogArgProvider] != "azure" || capturedCallArgs[callLogArgChannel] != "second-ch" {
		t.Fatalf("final failure log mixed the first route with the final attempt: %#v", capturedCallArgs)
	}
	if capturedCallArgs[callLogArgStatusCode] != 0 || capturedCallArgs[callLogArgUpstreamRequestID] != nil {
		t.Fatalf("final network failure retained an earlier attempt status/upstream ID: status=%#v upstream=%#v", capturedCallArgs[callLogArgStatusCode], capturedCallArgs[callLogArgUpstreamRequestID])
	}
	if attemptID, _ := capturedCallArgs[callLogArgAttemptID].(string); !strings.HasPrefix(attemptID, "att_") {
		t.Fatalf("final failure log lost the final attempt identity: %#v", capturedCallArgs[callLogArgAttemptID])
	}
}

func TestHandleChatCompletion_ModelNotAllowed(t *testing.T) {
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
	freezeCalled := false
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezeCalled = true
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
		},
	}

	relay := newTestRelay(db, b)

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
	if freezeCalled {
		t.Fatal("expected model permission denial to return before billing freeze")
	}
	if len(capturedArgs) != 39 {
		t.Fatalf("expected model denial trace insert, got %d args", len(capturedArgs))
	}
	if capturedArgs[8] != "auth.model_not_allowed" || capturedArgs[9] != "auth" || capturedArgs[22] != "auth" {
		t.Fatalf("unexpected model denial trace args: %#v", capturedArgs[8:23])
	}
	if capturedArgs[24] != evidenceCaptureGapAuth {
		t.Fatalf("expected permission_denied capture_gap, got %#v", capturedArgs[24])
	}
	if capturedArgs[27] != "auth" || capturedArgs[28] != "rejected" || capturedArgs[29] != "auth_error.model_not_allowed" {
		t.Fatalf("unexpected model denial decision args: %#v", capturedArgs[27:30])
	}
}

func TestHandleChatCompletion_SensitiveContent(t *testing.T) {
	encKey := []byte("0123456789abcdef01234567")
	f := filter.NewDFAFilter([]string{"赌博", "色情"})
	db := &testutil.MockDB{}
	freezeCalled := false
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezeCalled = true
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
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
	if freezeCalled {
		t.Fatal("expected policy rejection to return before billing freeze or routing")
	}
}

func TestP6F10ChatRequestLimitRejectsBeforeBillingOrRouting(t *testing.T) {
	limits := payloadpolicy.DefaultLimits()
	limits.ChatRequestBytes = 64
	limits.EmbeddingsRequestBytes = 64
	limits.NonStreamResponseBytes = 64
	limits.NodeInFlightBytes = 128
	limits.OrgInFlightBytes = 64
	budget := payloadpolicy.MustNewBudget(limits)
	freezeCalled := false
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezeCalled = true
			return &billing.FreezeResult{Success: true}, nil
		},
	})
	relay.SetPayloadBudget(budget)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"this body is over sixty-four bytes"}]}`))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-p6f10-input-limit")
	rec := httptest.NewRecorder()
	relay.HandleChatCompletion(rec, req.WithContext(ctx))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", rec.Code, rec.Body.String())
	}
	if freezeCalled {
		t.Fatal("oversize input reached billing freeze")
	}
}

func TestP6F10NonStreamResponseLimitHoldsProviderLiabilityWithoutDelivery(t *testing.T) {
	limits := payloadpolicy.DefaultLimits()
	limits.ChatRequestBytes = 64
	limits.EmbeddingsRequestBytes = 64
	limits.NonStreamResponseBytes = 64
	limits.NodeInFlightBytes = 128
	limits.OrgInFlightBytes = 64
	budget := payloadpolicy.MustNewBudget(limits)
	var completion billing.ProviderAttemptCompletion
	deliveryPrepared := false
	b := &mockBilling{
		completeAttemptFn: func(_ context.Context, req billing.ProviderAttemptCompletion) error {
			completion = req
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			deliveryPrepared = true
			return nil, nil
		},
	}
	relay := newTestRelay(&testutil.MockDB{}, b)
	relay.SetPayloadBudget(budget)
	route := &RouteResult{Channel: models.Channel{ID: 1, Provider: "test", Name: "test"}, ChannelKey: models.ChannelKey{ID: 1}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", 65))),
	}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-p6f10-output-limit")
	rec := httptest.NewRecorder()
	relay.relayNonStreamRequest(ctx, rec, resp, route, "req-p6f10-output-limit", 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 1, 1, "upstream-1")

	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "upstream_response_too_large") {
		t.Fatalf("expected bounded 502, got %d body=%s", rec.Code, rec.Body.String())
	}
	if completion.Outcome != billing.ProviderAttemptOutcomeResponseUnusable || completion.FailureKind != "response_too_large" || completion.RetryAuthorized {
		t.Fatalf("unexpected provider liability completion: %+v", completion)
	}
	if deliveryPrepared {
		t.Fatal("oversize response reached customer delivery preparation")
	}
}

func TestP6F10FinalResponseEnvelopeLimitRetainsKnownProviderUsage(t *testing.T) {
	limits := payloadpolicy.DefaultLimits()
	limits.ChatRequestBytes = 512
	limits.EmbeddingsRequestBytes = 512
	limits.NonStreamResponseBytes = 512
	limits.NodeInFlightBytes = 1024
	limits.OrgInFlightBytes = 512
	var held billing.SettleRequest
	var holdReason string
	holdContextActive := false
	deliveryPrepared := false
	b := &mockBilling{
		holdOutputFn: func(holdCtx context.Context, req billing.SettleRequest, reason string) error {
			holdContextActive = holdCtx.Err() == nil
			held, holdReason = req, reason
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			deliveryPrepared = true
			return nil, nil
		},
	}
	relay := newTestRelay(&testutil.MockDB{}, b)
	relay.SetPayloadBudget(payloadpolicy.MustNewBudget(limits))
	route := &RouteResult{Channel: models.Channel{ID: 1, Provider: "test", Name: "test"}, ChannelKey: models.ChannelKey{ID: 1}}
	body := fmt.Sprintf(`{"id":"chatcmpl-envelope","choices":[{"message":{"role":"assistant","content":%q}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`, strings.Repeat("<", 100))
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	ctx := context.WithValue(requestCtx, middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-p6f10-final-envelope")
	ctx = contextWithRouteAttempt(ctx, 1, 1)
	ctx = contextWithAttemptID(ctx, "att-final-envelope")
	rec := httptest.NewRecorder()
	relay.relayNonStreamRequest(ctx, rec, resp, route, "req-p6f10-final-envelope", 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 2, 3, "upstream-envelope")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "upstream_response_too_large") {
		t.Fatalf("expected final-envelope 502, got %d body=%s", rec.Code, rec.Body.String())
	}
	if deliveryPrepared || !holdContextActive || holdReason != "response_too_large" || held.UsagePurpose != metering.UsagePurposeProviderEvidence || held.AttemptID != "att-final-envelope" {
		t.Fatalf("final-envelope limit lost financial authority: prepared=%t active_ctx=%t reason=%s hold=%+v", deliveryPrepared, holdContextActive, holdReason, held)
	}
}

func TestP6F10ResponseExtensionIsBoundedAndServerOwned(t *testing.T) {
	limits := payloadpolicy.DefaultLimits()
	limits.ChatRequestBytes = 1024
	limits.EmbeddingsRequestBytes = 1024
	limits.NonStreamResponseBytes = 1024
	limits.NodeInFlightBytes = 2048
	limits.OrgInFlightBytes = 1024
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{})
	relay.SetPayloadBudget(payloadpolicy.MustNewBudget(limits))
	response := map[string]interface{}{"id": "ok", "_uniroute": map[string]interface{}{"untrusted": true}}
	base, maxBytes, err := relay.marshalNonStreamResponseBase(response)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(base, []byte("_uniroute")) {
		t.Fatalf("upstream-controlled extension survived base normalization: %s", base)
	}
	payload, err := appendNonStreamResponseExtension(base, map[string]interface{}{"request_id": "server-owned"}, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(payload)) > maxBytes || !bytes.Contains(payload, []byte(`"request_id":"server-owned"`)) {
		t.Fatalf("invalid bounded response extension: size=%d max=%d body=%s", len(payload), maxBytes, payload)
	}
}

func TestP6F10DeliveryPreparationOutlivesClientCancellation(t *testing.T) {
	prepareContextActive := false
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		prepareDeliveryFn: func(ctx context.Context, _ billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			prepareContextActive = ctx.Err() == nil
			return &billing.PrepareDeliveryResult{Funded: true}, nil
		},
	})
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	prepared, err := relay.prepareNonStreamDeliveryWithTimeout(requestCtx, billing.SettleRequest{RequestID: "req-cancelled", OrgID: 1})
	if err != nil || prepared == nil || !prepared.Funded || !prepareContextActive {
		t.Fatalf("delivery preparation inherited client cancellation: prepared=%+v active=%t err=%v", prepared, prepareContextActive, err)
	}
}

func TestP6F10ProviderOutputHoldRetriesIdempotentlyAfterCancellation(t *testing.T) {
	calls := 0
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{
		holdOutputFn: func(ctx context.Context, _ billing.SettleRequest, _ string) error {
			if ctx.Err() != nil {
				t.Fatal("provider output hold inherited client cancellation")
			}
			calls++
			if calls < billingSettleMaxAttempts {
				return errors.New("transient database error")
			}
			return nil
		},
	})
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	err := relay.holdNonStreamOutputWithRetry(requestCtx, billing.SettleRequest{RequestID: "req-hold-retry", OrgID: 1}, "guardrail_withheld", nil)
	if err != nil || calls != billingSettleMaxAttempts {
		t.Fatalf("idempotent provider output hold did not converge: calls=%d err=%v", calls, err)
	}
}

func newP6F10GuardrailService(t *testing.T, policy guardrail.Policy) *guardrail.Service {
	t.Helper()
	database := &testutil.MockDB{QueryFn: func(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
		if strings.Contains(query, "HAVING COUNT(*)") {
			return &testutil.MockRows{}, nil
		}
		return &testutil.MockRows{Rows: []func(...any) error{func(dest ...any) error {
			*dest[0].(*int64) = policy.ID
			*dest[1].(**int64) = policy.OrgID
			*dest[2].(**int64) = policy.ProjectID
			*dest[3].(**int64) = policy.APIKeyID
			*dest[4].(*string) = policy.Name
			*dest[5].(*string) = policy.Model
			*dest[6].(*string) = policy.Stage
			*dest[7].(*string) = policy.DetectorRef
			*dest[8].(*string) = policy.Action
			*dest[9].(*string) = policy.Mode
			*dest[10].(*string) = policy.FailureMode
			*dest[11].(*bool) = policy.Mandatory
			*dest[12].(*json.RawMessage) = append(json.RawMessage(nil), policy.Config...)
			*dest[13].(*int64) = policy.Version
			*dest[14].(*time.Time) = time.Now().UTC()
			*dest[15].(*time.Time) = time.Now().UTC()
			return nil
		}}}, nil
	}}
	service, err := guardrail.NewService(database, nil, []byte("0123456789abcdef0123456789abcdef"), guardrail.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	return service
}

func TestP6F10InputBlockRunsBeforeBilling(t *testing.T) {
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 1, Name: "block secrets", Version: 1, Stage: guardrail.StageInput,
		DetectorRef: guardrail.DetectorPII, Action: guardrail.ActionBlock,
		Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureFailClosed,
		Mandatory: true, Config: json.RawMessage(`{"entities":["secret"]}`),
	})
	freezeCalled := false
	relay := newTestRelay(&testutil.MockDB{}, &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
		freezeCalled = true
		return &billing.FreezeResult{Success: true}, nil
	}})
	relay.SetGuardrail(service)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"token_abcdefghijkl"}]}`))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-p6f10-guardrail-input")
	rec := httptest.NewRecorder()
	relay.HandleChatCompletion(rec, req.WithContext(ctx))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "guardrail_error.input_blocked") {
		t.Fatalf("expected policy block, got %d body=%s", rec.Code, rec.Body.String())
	}
	if freezeCalled {
		t.Fatal("input policy block reached billing")
	}
}

func TestP6F10InputRedactionPreservesUnknownEnvelopeFields(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","temperature":0.25,"vendor_extension":{"keep":true},"messages":[{"role":"user","content":"alice@example.com"}]}`)
	req := ChatCompletionRequest{Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "alice@example.com"}}}
	result := guardrail.Result{Transformed: true, Texts: []guardrail.Text{{Path: "messages[0].content", Content: "[REDACTED]"}}}
	rewritten, err := applyChatGuardrailTransform(body, &req, result)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rewritten, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["temperature"] != 0.25 || payload["vendor_extension"].(map[string]interface{})["keep"] != true || req.Messages[0].Content != "[REDACTED]" {
		t.Fatalf("content envelope lost fields: %s", rewritten)
	}
}

func TestP6F10OutputBlockRetainsProviderEvidenceWithoutDelivery(t *testing.T) {
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 2, Name: "block output email", Version: 1, Stage: guardrail.StageNonStreamOutput,
		DetectorRef: guardrail.DetectorPII, Action: guardrail.ActionBlock,
		Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	})
	pinned := service.Pin(guardrail.Subject{OrgID: 1, APIKeyID: 2, Model: "gpt-4o", Endpoint: "chat"})
	var held billing.SettleRequest
	var holdReason string
	deliveryPrepared := false
	b := &mockBilling{
		holdOutputFn: func(_ context.Context, req billing.SettleRequest, reason string) error {
			held, holdReason = req, reason
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			deliveryPrepared = true
			return nil, nil
		},
	}
	database := &testutil.MockDB{}
	relay := newTestRelay(database, b)
	route := &RouteResult{Channel: models.Channel{ID: 1, Provider: "test", Name: "test"}, ChannelKey: models.ChannelKey{ID: 1}}
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"email alice@example.com"}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-p6f10-output-block")
	ctx = contextWithGuardrailPin(ctx, pinned)
	ctx = contextWithRouteAttempt(ctx, 1, 1)
	ctx = contextWithAttemptID(ctx, "att-output-block")
	rec := httptest.NewRecorder()
	relay.relayNonStreamRequest(ctx, rec, resp, route, "req-p6f10-output-block", 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 2, 3, "upstream-1")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "guardrail_error.output_blocked") {
		t.Fatalf("expected output block, got %d body=%s", rec.Code, rec.Body.String())
	}
	if deliveryPrepared || holdReason != "guardrail_withheld" || held.UsagePurpose != metering.UsagePurposeProviderEvidence || held.AttemptID != "att-output-block" {
		t.Fatalf("unsafe output financial path: prepared=%t reason=%s hold=%+v", deliveryPrepared, holdReason, held)
	}
}

func TestNonStreamReasoningContentIsBlockedBeforeDelivery(t *testing.T) {
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 22, Name: "block reasoning email", Version: 1, Stage: guardrail.StageNonStreamOutput,
		DetectorRef: guardrail.DetectorPII, Action: guardrail.ActionBlock,
		Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	})
	pinned := service.Pin(guardrail.Subject{OrgID: 1, APIKeyID: 2, Model: "gpt-4o", Endpoint: "chat"})
	var holdReason string
	deliveryPrepared := false
	b := &mockBilling{
		holdOutputFn: func(_ context.Context, _ billing.SettleRequest, reason string) error {
			holdReason = reason
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			deliveryPrepared = true
			return nil, nil
		},
	}
	relay := newTestRelay(&testutil.MockDB{}, b)
	route := &RouteResult{Channel: models.Channel{ID: 1, Provider: "test", Name: "test"}, ChannelKey: models.ChannelKey{ID: 1}}
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl-reasoning-block","choices":[{"message":{"role":"assistant","content":"safe","reasoning_content":"alice@example.com"}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = contextWithGuardrailPin(ctx, pinned)
	ctx = contextWithRouteAttempt(ctx, 1, 1)
	ctx = contextWithAttemptID(ctx, "att-reasoning-block")
	rec := httptest.NewRecorder()

	relay.relayNonStreamRequest(ctx, rec, resp, route, "req-reasoning-block", 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 2, 3, "upstream-reasoning-block")

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "guardrail_error.output_blocked") {
		t.Fatalf("reasoning content bypassed output block: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if deliveryPrepared || holdReason != "guardrail_withheld" {
		t.Fatalf("reasoning block reached delivery: prepared=%t hold=%q", deliveryPrepared, holdReason)
	}
}

func TestP6F10OutputRedactionDeliversTransformedContent(t *testing.T) {
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 3, Name: "redact output email", Version: 1, Stage: guardrail.StageNonStreamOutput,
		DetectorRef: guardrail.DetectorPII, Action: guardrail.ActionRedact,
		Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	})
	pinned := service.Pin(guardrail.Subject{OrgID: 1, APIKeyID: 2, Model: "gpt-4o", Endpoint: "chat"})
	held := false
	b := &mockBilling{holdOutputFn: func(context.Context, billing.SettleRequest, string) error { held = true; return nil }}
	relay := newTestRelay(&testutil.MockDB{}, b)
	route := &RouteResult{Channel: models.Channel{ID: 1, Provider: "test", Name: "test"}, ChannelKey: models.ChannelKey{ID: 1}}
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl-2","choices":[{"message":{"role":"assistant","content":"email alice@example.com"}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-p6f10-output-redact")
	ctx = contextWithGuardrailPin(ctx, pinned)
	ctx = contextWithRouteAttempt(ctx, 1, 1)
	ctx = contextWithAttemptID(ctx, "att-output-redact")
	rec := httptest.NewRecorder()
	relay.relayNonStreamRequest(ctx, rec, resp, route, "req-p6f10-output-redact", 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 2, 3, "upstream-2")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "[REDACTED]") || strings.Contains(rec.Body.String(), "alice@example.com") {
		t.Fatalf("expected transformed delivery, got %d body=%s", rec.Code, rec.Body.String())
	}
	if held {
		t.Fatal("redacted deliverable output entered cost hold")
	}
}

func TestNonStreamReasoningIsRedactedBeforeDelivery(t *testing.T) {
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 23, Name: "redact reasoning email", Version: 1, Stage: guardrail.StageNonStreamOutput,
		DetectorRef: guardrail.DetectorPII, Action: guardrail.ActionRedact,
		Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	})
	pinned := service.Pin(guardrail.Subject{OrgID: 1, APIKeyID: 2, Model: "gpt-4o", Endpoint: "chat"})
	held := false
	b := &mockBilling{holdOutputFn: func(context.Context, billing.SettleRequest, string) error { held = true; return nil }}
	relay := newTestRelay(&testutil.MockDB{}, b)
	route := &RouteResult{Channel: models.Channel{ID: 1, Provider: "test", Name: "test"}, ChannelKey: models.ChannelKey{ID: 1}}
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl-reasoning-redact","choices":[{"message":{"role":"assistant","content":"safe","reasoning":"alice@example.com"}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = contextWithGuardrailPin(ctx, pinned)
	ctx = contextWithRouteAttempt(ctx, 1, 1)
	ctx = contextWithAttemptID(ctx, "att-reasoning-redact")
	rec := httptest.NewRecorder()

	relay.relayNonStreamRequest(ctx, rec, resp, route, "req-reasoning-redact", 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 2, 3, "upstream-reasoning-redact")

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reasoning":"[REDACTED]"`) ||
		strings.Contains(rec.Body.String(), "alice@example.com") || !strings.Contains(rec.Body.String(), `"content":"safe"`) {
		t.Fatalf("reasoning was not atomically redacted: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if held {
		t.Fatal("redacted reasoning entered provider-evidence hold")
	}
}

func TestNonStreamUnsupportedKnownTextShapeIsWithheld(t *testing.T) {
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 24, Name: "redact modeled output", Version: 1, Stage: guardrail.StageNonStreamOutput,
		DetectorRef: guardrail.DetectorPII, Action: guardrail.ActionRedact,
		Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	})
	pinned := service.Pin(guardrail.Subject{OrgID: 1, APIKeyID: 2, Model: "gpt-4o", Endpoint: "chat"})
	var holdReason string
	deliveryPrepared := false
	b := &mockBilling{
		holdOutputFn: func(_ context.Context, _ billing.SettleRequest, reason string) error {
			holdReason = reason
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			deliveryPrepared = true
			return nil, nil
		},
	}
	relay := newTestRelay(&testutil.MockDB{}, b)
	route := &RouteResult{Channel: models.Channel{ID: 1, Provider: "test", Name: "test"}, ChannelKey: models.ChannelKey{ID: 1}}
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl-reasoning-shape","choices":[{"message":{"role":"assistant","content":"safe","reasoning_content":{"text":"alice@example.com"}}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = contextWithGuardrailPin(ctx, pinned)
	ctx = contextWithRouteAttempt(ctx, 1, 1)
	ctx = contextWithAttemptID(ctx, "att-reasoning-shape")
	rec := httptest.NewRecorder()

	relay.relayNonStreamRequest(ctx, rec, resp, route, "req-reasoning-shape", 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 2, 3, "upstream-reasoning-shape")

	if rec.Code != http.StatusForbidden || deliveryPrepared || holdReason != "guardrail_withheld" {
		t.Fatalf("unsupported modeled text shape was delivered: status=%d prepared=%t hold=%q body=%s", rec.Code, deliveryPrepared, holdReason, rec.Body.String())
	}
}

func TestP6F10OutputTransformRejectsChangedContentPath(t *testing.T) {
	response := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{
			"message": map[string]interface{}{"content": "alice@example.com"},
		}},
	}
	_, refs := nonStreamOutputTexts(response)
	err := applyOutputGuardrailTransform(guardrail.Result{
		Transformed: true,
		Texts:       []guardrail.Text{{Path: "choices[1].message.content", Content: "[REDACTED]"}},
	}, refs)
	if err == nil {
		t.Fatal("changed content path must fail closed before customer delivery")
	}
	if got := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"]; got != "alice@example.com" {
		t.Fatalf("unsafe transform modified the response before validation: %#v", got)
	}
}

func TestP6F10StructuredOutputTextAndToolArgumentsAreInspectable(t *testing.T) {
	response := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{
			"message": map[string]interface{}{
				"content": []interface{}{map[string]interface{}{"type": "text", "text": "alice@example.com"}},
				"tool_calls": []interface{}{map[string]interface{}{
					"function": map[string]interface{}{"name": "lookup", "arguments": `{"email":"alice@example.com"}`},
				}},
			},
		}},
	}
	texts, refs := nonStreamOutputTexts(response)
	if len(texts) != 2 || len(refs) != 2 {
		t.Fatalf("structured output content was skipped: texts=%+v refs=%+v", texts, refs)
	}
	result := guardrail.Result{Transformed: true, Texts: []guardrail.Text{
		{Path: texts[0].Path, Content: "[REDACTED]"},
		{Path: texts[1].Path, Content: `{"email":"[REDACTED]"}`},
	}}
	if err := applyOutputGuardrailTransform(result, refs); err != nil {
		t.Fatal(err)
	}
	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	if got := message["content"].([]interface{})[0].(map[string]interface{})["text"]; got != "[REDACTED]" {
		t.Fatalf("structured text was not transformed: %#v", got)
	}
	if got := message["tool_calls"].([]interface{})[0].(map[string]interface{})["function"].(map[string]interface{})["arguments"]; got != `{"email":"[REDACTED]"}` {
		t.Fatalf("tool arguments were not transformed safely: %#v", got)
	}
}

func TestP6F10InvalidStructuredTransformIsAtomic(t *testing.T) {
	response := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{
			"message": map[string]interface{}{
				"content": "alice@example.com",
				"tool_calls": []interface{}{map[string]interface{}{
					"function": map[string]interface{}{"arguments": `{"email":"alice@example.com"}`},
				}},
			},
		}},
	}
	texts, refs := nonStreamOutputTexts(response)
	result := guardrail.Result{Transformed: true, Texts: []guardrail.Text{
		{Path: texts[0].Path, Content: "[REDACTED]"},
		{Path: texts[1].Path, Content: `{"email":[REDACTED]}`},
	}}
	if err := applyOutputGuardrailTransform(result, refs); err == nil {
		t.Fatal("invalid structured arguments must fail closed")
	}
	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	if message["content"] != "alice@example.com" {
		t.Fatalf("partial mutation occurred before structured validation: %#v", message)
	}
}

func TestP6F10GuardrailTraceDecisionClassification(t *testing.T) {
	if got := sourceComponentForTraceStage("guardrail_nonstream_output"); got != "guardrail" {
		t.Fatalf("unexpected source component %q", got)
	}
	kind, result, reason := decisionEvidence("guardrail.decision", map[string]interface{}{
		"guardrail_decision": "block", "guardrail_outcome": "unavailable",
	})
	if kind != "guardrail" || result != "block" || reason != "unavailable" {
		t.Fatalf("unexpected guardrail decision evidence: %s %s %s", kind, result, reason)
	}
}

func TestP6F10IndeterminatePolicyIsNotReportedAsInfrastructureOutage(t *testing.T) {
	result := guardrail.Result{Decision: guardrail.DecisionBlock, Degraded: true, Receipts: []guardrail.Receipt{{Outcome: guardrail.OutcomeIndeterminate}}}
	if guardrailBlockedByUnavailable(result) {
		t.Fatal("deterministic indeterminate policy result was mislabeled as detector infrastructure outage")
	}
	result.Receipts[0].Outcome = guardrail.OutcomeUnavailable
	if !guardrailBlockedByUnavailable(result) {
		t.Fatal("detector infrastructure outage lost its typed 503 path")
	}
}
