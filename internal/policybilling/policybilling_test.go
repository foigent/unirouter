package policybilling

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/terminalhook"
	"github.com/uniroute/unirouter/internal/workloadadmission"
)

type fakeProvider struct {
	freezeResult *billing.FreezeResult
	freezeErr    error
	settleResult *billing.SettleResult
	settleErr    error
	freezeCalls  int
	settleCalls  int
	cancelCalls  int
	beginReq     billing.ProviderAttemptStart
	completeReq  billing.ProviderAttemptCompletion
}

func (p *fakeProvider) QueryModelPriceForOrg(context.Context, int64, string) (float64, float64, error) {
	return 1, 1, nil
}

func (p *fakeProvider) CalculateCost(context.Context, string, int, int) (int, error) {
	return 1, nil
}

func (p *fakeProvider) Freeze(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
	p.freezeCalls++
	if p.freezeResult == nil && p.freezeErr == nil {
		return &billing.FreezeResult{Success: true, Amount: 10}, nil
	}
	return p.freezeResult, p.freezeErr
}

func (p *fakeProvider) BeginProviderAttempt(_ context.Context, req billing.ProviderAttemptStart) error {
	p.beginReq = req
	return nil
}

func (p *fakeProvider) CompleteProviderAttempt(_ context.Context, req billing.ProviderAttemptCompletion) error {
	p.completeReq = req
	return nil
}

func (p *fakeProvider) PrepareNonStreamDelivery(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
	return &billing.PrepareDeliveryResult{Funded: true, Preview: billing.SettleResult{Success: true}}, nil
}

func (p *fakeProvider) RecordDelivery(context.Context, billing.DeliveryEvent) error { return nil }

func TestP6F40BProviderAttemptAuthorityPassesThroughPolicyBilling(t *testing.T) {
	provider := &fakeProvider{}
	service := New(provider, nil)
	start := billing.ProviderAttemptStart{RequestID: "op_policy_attempt", AttemptID: "att_policy_attempt", OrgID: 7}
	completion := billing.ProviderAttemptCompletion{RequestID: start.RequestID, AttemptID: start.AttemptID, OrgID: 7, Outcome: billing.ProviderAttemptOutcomeUnknown}
	if err := service.BeginProviderAttempt(t.Context(), start); err != nil {
		t.Fatalf("begin provider attempt: %v", err)
	}
	if err := service.CompleteProviderAttempt(t.Context(), completion); err != nil {
		t.Fatalf("complete provider attempt: %v", err)
	}
	if provider.beginReq != start || provider.completeReq != completion {
		t.Fatalf("provider-attempt authority changed across policy billing: begin=%+v complete=%+v", provider.beginReq, provider.completeReq)
	}
}

func (p *fakeProvider) MarkBillableOutput(context.Context, string, int64) error { return nil }
func (p *fakeProvider) PersistSettlementUsage(context.Context, billing.SettleRequest) error {
	return nil
}

func (p *fakeProvider) Settle(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
	p.settleCalls++
	if p.settleResult == nil && p.settleErr == nil {
		return &billing.SettleResult{Success: true, ActualCost: 1}, nil
	}
	return p.settleResult, p.settleErr
}

func (p *fakeProvider) Cancel(context.Context, string, int64) error {
	p.cancelCalls++
	return nil
}

type fakeRuntime struct {
	reserveErr        error
	finalizeErr       error
	cancelErr         error
	lastRequest       limitpolicy.AdmissionRequest
	reserveCalls      int
	finalizeCalls     int
	cancelCalls       int
	cancelBeforeCalls int
	finalizedTokens   int64
}

func (r *fakeRuntime) Reserve(_ context.Context, request limitpolicy.AdmissionRequest) (*limitpolicy.Reservation, error) {
	r.reserveCalls++
	r.lastRequest = request
	if r.reserveErr != nil {
		return nil, r.reserveErr
	}
	return &limitpolicy.Reservation{OperationID: request.OperationID}, nil
}

func (r *fakeRuntime) Finalize(_ context.Context, _ *limitpolicy.Reservation, actualTokens int64) error {
	r.finalizeCalls++
	r.finalizedTokens = actualTokens
	return r.finalizeErr
}

func (r *fakeRuntime) Cancel(context.Context, *limitpolicy.Reservation) error {
	r.cancelCalls++
	return r.cancelErr
}

func (r *fakeRuntime) CancelBeforeDispatch(context.Context, *limitpolicy.Reservation) error {
	r.cancelBeforeCalls++
	return nil
}

func (r *fakeRuntime) TerminalStateTTL(leaseTTL time.Duration) time.Duration {
	return leaseTTL + 7*time.Minute
}

type fakeTerminalHooks struct {
	registerErr    error
	registerCalls  int
	reconcileCalls int
	hookType       string
	operationID    string
	orgID          int64
	reservation    any
	expiresAt      time.Time
}

func (h *fakeTerminalHooks) Register(_ context.Context, hookType, operationID string, orgID int64, reservation any, expiresAt time.Time) error {
	h.registerCalls++
	h.hookType = hookType
	h.operationID = operationID
	h.orgID = orgID
	h.reservation = reservation
	h.expiresAt = expiresAt
	return h.registerErr
}

func (h *fakeTerminalHooks) Reconcile(_ context.Context, operationID string, orgID int64) error {
	h.reconcileCalls++
	h.operationID = operationID
	h.orgID = orgID
	return nil
}

func policyContext() context.Context {
	apiKey := &models.ApiKey{ID: 13, OrgID: 7, QPSLimit: 10, RPMLimit: 60}
	ctx := context.WithValue(context.Background(), middleware.CtxAPIKey, apiKey)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxProjectID, int64(11))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(13))
	return ctx
}

func TestPolicyDenialHappensBeforeWalletFreeze(t *testing.T) {
	inner := &fakeProvider{}
	runtime := &fakeRuntime{reserveErr: limitpolicy.ErrTPMExceeded}
	service := New(inner, runtime)
	result, err := service.Freeze(policyContext(), billing.FreezeRequest{
		RequestID: "operation-denied", OrgID: 7, Model: "company-chat",
		EstimatedTokens: 100, EstimatedInputTokens: 40, ReservedCompletionTokens: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Success || result.ErrorCode != ErrorCodeTPMExceeded {
		t.Fatalf("result = %#v", result)
	}
	if inner.freezeCalls != 0 {
		t.Fatalf("wallet freeze calls = %d, want 0", inner.freezeCalls)
	}
}

func TestPolicyLifecycleUsesServerSubjectAndActualUsage(t *testing.T) {
	inner := &fakeProvider{}
	runtime := &fakeRuntime{}
	service := New(inner, runtime)
	ctx := workloadadmission.ContextWithClass(policyContext(), workloadadmission.ClassStream)
	result, err := service.Freeze(ctx, billing.FreezeRequest{
		RequestID: "operation-success", OrgID: 7, Model: "company-chat",
		EstimatedTokens: 100, EstimatedInputTokens: 40, ReservedCompletionTokens: 60,
	})
	if err != nil || result == nil || !result.Success {
		t.Fatalf("freeze result=%#v err=%v", result, err)
	}
	request := runtime.lastRequest
	if request.Subject.OrgID != 7 || request.Subject.ProjectID != 11 || request.Subject.APIKeyID != 13 ||
		request.Subject.Model != "company-chat" || request.EstimatedInput != 40 || request.ReservedOutput != 60 {
		t.Fatalf("admission request = %#v", request)
	}
	if request.Subject.BaseRPSLimit != 10 || request.Subject.BaseRPMLimit != 60 {
		t.Fatalf("API-key base limits not propagated: %#v", request.Subject)
	}
	if request.WorkloadClass != workloadadmission.ClassStream {
		t.Fatalf("server-derived workload class = %q", request.WorkloadClass)
	}
	settled, err := service.Settle(context.Background(), billing.SettleRequest{
		RequestID: "operation-success", OrgID: 7, PromptTokens: 44, CompletionTokens: 22,
	})
	if err != nil || settled == nil || !settled.Success {
		t.Fatalf("settle result=%#v err=%v", settled, err)
	}
	if runtime.finalizeCalls != 1 || runtime.finalizedTokens != 66 {
		t.Fatalf("finalize calls=%d tokens=%d", runtime.finalizeCalls, runtime.finalizedTokens)
	}
}

func TestP6F20DurableHookOwnsPolicyAndWorkloadTerminalRecovery(t *testing.T) {
	inner := &fakeProvider{}
	runtime := &fakeRuntime{}
	hooks := &fakeTerminalHooks{}
	service := New(inner, runtime)
	service.SetTerminalHooks(hooks)
	ctx := agentcap.ContextWithInvocationLeaseTTL(policyContext(), 30*time.Minute)

	result, err := service.Freeze(ctx, billing.FreezeRequest{
		RequestID: "operation-p6f20-hook", OrgID: 7, Model: "company-chat", EstimatedTokens: 10,
	})
	if err != nil || result == nil || !result.Success {
		t.Fatalf("freeze result=%#v err=%v", result, err)
	}
	if hooks.registerCalls != 1 || hooks.hookType != terminalhook.HookLimitPolicy ||
		hooks.operationID != "operation-p6f20-hook" || hooks.orgID != 7 {
		t.Fatalf("unexpected hook registration: %#v", hooks)
	}
	if _, ok := hooks.reservation.(*limitpolicy.DurableReservation); !ok {
		t.Fatalf("terminal hook reservation type = %T", hooks.reservation)
	}
	remaining := time.Until(hooks.expiresAt)
	if remaining < 36*time.Minute || remaining > 38*time.Minute {
		t.Fatalf("terminal hook lifetime = %s, want exact runtime bound", remaining)
	}

	settled, err := service.Settle(context.Background(), billing.SettleRequest{
		RequestID: "operation-p6f20-hook", OrgID: 7, PromptTokens: 4, CompletionTokens: 2,
	})
	if err != nil || settled == nil || !settled.Success {
		t.Fatalf("settle result=%#v err=%v", settled, err)
	}
	if hooks.reconcileCalls != 1 || runtime.finalizeCalls != 1 || runtime.cancelCalls != 0 {
		t.Fatalf("reconcile=%d runtime=%#v", hooks.reconcileCalls, runtime)
	}
}

func TestP6F20PolicyHookRegistrationFailureRollsBackAllPriorAdmissions(t *testing.T) {
	inner := &fakeProvider{}
	runtime := &fakeRuntime{}
	hooks := &fakeTerminalHooks{registerErr: errors.New("database unavailable")}
	service := New(inner, runtime)
	service.SetTerminalHooks(hooks)

	result, err := service.Freeze(policyContext(), billing.FreezeRequest{
		RequestID: "operation-p6f20-hook-failure", OrgID: 7, Model: "company-chat", EstimatedTokens: 10,
	})
	if err == nil || result != nil {
		t.Fatalf("hook registration failure = %#v / %v", result, err)
	}
	if runtime.cancelBeforeCalls != 1 || inner.cancelCalls != 1 {
		t.Fatalf("policy cancels=%d billing cancels=%d", runtime.cancelBeforeCalls, inner.cancelCalls)
	}
}

func TestWalletFreezeFailureRollsBackPolicyBeforeDispatch(t *testing.T) {
	inner := &fakeProvider{freezeResult: &billing.FreezeResult{Success: false, ErrorCode: "insufficient_balance"}}
	runtime := &fakeRuntime{}
	service := New(inner, runtime)
	result, err := service.Freeze(policyContext(), billing.FreezeRequest{
		RequestID: "operation-wallet-denied", OrgID: 7, Model: "company-chat", EstimatedTokens: 10,
	})
	if err != nil || result == nil || result.Success {
		t.Fatalf("freeze result=%#v err=%v", result, err)
	}
	if runtime.cancelBeforeCalls != 1 {
		t.Fatalf("pre-dispatch policy cancellations = %d", runtime.cancelBeforeCalls)
	}
}

func TestOrdinaryCancellationReleasesConcurrency(t *testing.T) {
	inner := &fakeProvider{}
	runtime := &fakeRuntime{}
	service := New(inner, runtime)
	if _, err := service.Freeze(policyContext(), billing.FreezeRequest{
		RequestID: "operation-cancel", OrgID: 7, Model: "company-chat", EstimatedTokens: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(context.Background(), "operation-cancel", 7); err != nil {
		t.Fatal(err)
	}
	if inner.cancelCalls != 1 || runtime.cancelCalls != 1 {
		t.Fatalf("inner cancels=%d policy cancels=%d", inner.cancelCalls, runtime.cancelCalls)
	}
}

func TestAmbiguousSettlementDoesNotReleasePolicyReservation(t *testing.T) {
	inner := &fakeProvider{settleErr: errors.New("ambiguous database result")}
	runtime := &fakeRuntime{}
	service := New(inner, runtime)
	if _, err := service.Freeze(policyContext(), billing.FreezeRequest{
		RequestID: "operation-ambiguous", OrgID: 7, Model: "company-chat", EstimatedTokens: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Settle(context.Background(), billing.SettleRequest{RequestID: "operation-ambiguous", OrgID: 7}); err == nil {
		t.Fatal("expected ambiguous settlement error")
	}
	if err := service.Cancel(context.Background(), "operation-ambiguous", 7); err != nil {
		t.Fatal(err)
	}
	if runtime.cancelCalls != 0 {
		t.Fatalf("ambiguous settlement released policy reservation %d times", runtime.cancelCalls)
	}
}

func TestTerminalRuntimeFailuresDoNotLeakInProcessTrackers(t *testing.T) {
	t.Run("settlement", func(t *testing.T) {
		inner := &fakeProvider{}
		runtime := &fakeRuntime{finalizeErr: errors.New("redis unavailable")}
		service := New(inner, runtime)
		if _, err := service.Freeze(policyContext(), billing.FreezeRequest{
			RequestID: "operation-finalize-error", OrgID: 7, Model: "company-chat", EstimatedTokens: 10,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Settle(context.Background(), billing.SettleRequest{
			RequestID: "operation-finalize-error", OrgID: 7, PromptTokens: 4, CompletionTokens: 2,
		}); err != nil {
			t.Fatal(err)
		}
		if _, tracked := service.reservations.Load("operation-finalize-error"); tracked {
			t.Fatal("settled reservation tracker leaked after bounded Redis retention")
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		inner := &fakeProvider{}
		runtime := &fakeRuntime{cancelErr: errors.New("redis unavailable")}
		service := New(inner, runtime)
		if _, err := service.Freeze(policyContext(), billing.FreezeRequest{
			RequestID: "operation-cancel-error", OrgID: 7, Model: "company-chat", EstimatedTokens: 10,
		}); err != nil {
			t.Fatal(err)
		}
		if err := service.Cancel(context.Background(), "operation-cancel-error", 7); err != nil {
			t.Fatal(err)
		}
		if _, tracked := service.reservations.Load("operation-cancel-error"); tracked {
			t.Fatal("cancelled reservation tracker leaked after bounded Redis retention")
		}
	})
}

func TestErrorCodesAreTyped(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{limitpolicy.ErrRPSExceeded, ErrorCodeRPSExceeded},
		{limitpolicy.ErrRPMExceeded, ErrorCodeRPMExceeded},
		{limitpolicy.ErrTPMExceeded, ErrorCodeTPMExceeded},
		{limitpolicy.ErrConcurrencyExceeded, ErrorCodeConcurrencyExceeded},
		{limitpolicy.ErrWorkloadCapacity, ErrorCodeWorkloadCapacity},
		{limitpolicy.ErrWorkloadConfig, ErrorCodeWorkloadConfig},
		{limitpolicy.ErrConflict, ErrorCodeConflict},
		{limitpolicy.ErrStateUnavailable, ErrorCodeStateUnavailable},
	}
	for _, test := range tests {
		if got := errorCode(test.err); got != test.code {
			t.Fatalf("errorCode(%v) = %q, want %q", test.err, got, test.code)
		}
	}
}
