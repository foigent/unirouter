package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

type fakeProviderCostReconciler struct {
	request billing.ProviderReconciliationRequest
	result  *billing.ProviderReconciliationResult
	calls   int
	err     error
}

func (f *fakeProviderCostReconciler) ReconcileProviderAttempt(_ context.Context, request billing.ProviderReconciliationRequest) (*billing.ProviderReconciliationResult, error) {
	f.calls++
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return nil, billing.ErrProviderReconciliationUnsafe
}

func TestP6F40CProviderCostHandlerRejectsCallerSuppliedAmount(t *testing.T) {
	fake := &fakeProviderCostReconciler{}
	h := &Handler{providerCostReconciler: fake}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/provider-cost/reconciliations", bytes.NewBufferString(`{
		"org_id":1,"request_id":"op_1","attempt_id":"att_1","action":"collect",
		"reason":"reviewed","evidence_ref":"case://1","amount":999999
	}`))
	rec := httptest.NewRecorder()
	h.ReconcileProviderCost(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, fake.calls)
}

func TestP6F40CProviderCostHandlerUsesAuthenticatedActorAndNoAmount(t *testing.T) {
	fake := &fakeProviderCostReconciler{}
	h := &Handler{providerCostReconciler: fake}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/provider-cost/reconciliations", bytes.NewBufferString(`{
		"org_id":7,"request_id":"op_7","attempt_id":"att_7","action":"refund",
		"reason":"provider confirmed no delivery","evidence_ref":"case://7"
	}`))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(42))
	rec := httptest.NewRecorder()
	h.ReconcileProviderCost(rec, req.WithContext(ctx))
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Equal(t, 1, fake.calls)
	require.Equal(t, int64(42), fake.request.ActorUserID)
	require.Equal(t, int64(7), fake.request.OrgID)
}

func TestP6F40CProviderCostHandlerRejectsTrailingObject(t *testing.T) {
	fake := &fakeProviderCostReconciler{}
	h := &Handler{providerCostReconciler: fake}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/provider-cost/reconciliations", bytes.NewBufferString(`
		{"org_id":1,"request_id":"op_1","attempt_id":"att_1","action":"refund","reason":"reviewed","evidence_ref":"case://1"}
		{"org_id":2}
	`))
	rec := httptest.NewRecorder()
	h.ReconcileProviderCost(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, fake.calls)
}

func TestP6F40CProviderCostHandlerMapsInternalFailureToServerError(t *testing.T) {
	fake := &fakeProviderCostReconciler{err: context.DeadlineExceeded}
	h := &Handler{providerCostReconciler: fake}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/provider-cost/reconciliations", bytes.NewBufferString(`{
		"org_id":7,"request_id":"op_7","attempt_id":"att_7","action":"refund",
		"reason":"reviewed","evidence_ref":"case://7"
	}`))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(42))
	rec := httptest.NewRecorder()
	h.ReconcileProviderCost(rec, req.WithContext(ctx))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestProviderCostReconciliationAuditUsesBoundedResourceAndKeepsExactAttemptIdentity(t *testing.T) {
	requestID := "op_6e2c3f9ee70d839a56d047d495675308"
	attemptID := "att_be050b77c5c63c10169255f2def50308"
	fake := &fakeProviderCostReconciler{result: &billing.ProviderReconciliationResult{
		OrgID: 1, RequestID: requestID, AttemptID: attemptID, APIKeyID: 69,
		Action: billing.ProviderReconciliationRefund, State: "reconciled",
		CustomerChargedMicros: "0", CustomerRefundMicros: "0",
		Currency: "CNY", AmountScale: 1_000_000,
	}}
	var auditArgs []any
	db := &testutil.MockDB{ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "INSERT INTO audit_logs") {
			auditArgs = append([]any(nil), args...)
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}}
	h := &Handler{db: db, providerCostReconciler: fake}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/provider-cost/reconciliations", bytes.NewBufferString(`{
		"org_id":1,"request_id":"`+requestID+`","attempt_id":"`+attemptID+`","action":"refund",
		"reason":"reviewed","evidence_ref":"case://bounded-audit"
	}`))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(42))
	rec := httptest.NewRecorder()

	h.ReconcileProviderCost(rec, req.WithContext(ctx))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, auditArgs, 14)
	require.Equal(t, requestID, auditArgs[4])
	require.LessOrEqual(t, len(auditArgs[4].(string)), 64)
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(auditArgs[5].(string)), &detail))
	require.Equal(t, requestID, detail["request_id"])
	require.Equal(t, attemptID, detail["attempt_id"])
}
