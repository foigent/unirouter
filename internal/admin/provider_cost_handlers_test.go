package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
)

type fakeProviderCostReconciler struct {
	request billing.ProviderReconciliationRequest
	calls   int
	err     error
}

func (f *fakeProviderCostReconciler) ReconcileProviderAttempt(_ context.Context, request billing.ProviderReconciliationRequest) (*billing.ProviderReconciliationResult, error) {
	f.calls++
	f.request = request
	if f.err != nil {
		return nil, f.err
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
