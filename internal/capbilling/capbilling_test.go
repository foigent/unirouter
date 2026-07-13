package capbilling

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/terminalhook"
)

type fakeProvider struct {
	mu          sync.Mutex
	freeze      *billing.FreezeResult
	freezeErr   error
	extend      *billing.ExtendReservationResult
	extendErr   error
	extendCalls int
	settle      *billing.SettleResult
	settleErr   error
	cancelErr   error
	cancelCalls int
	beginReq    billing.ProviderAttemptStart
	completeReq billing.ProviderAttemptCompletion
}

func (p *fakeProvider) QueryModelPriceForOrg(context.Context, int64, string) (float64, float64, error) {
	return 1, 1, nil
}

func (p *fakeProvider) CalculateCost(context.Context, string, int, int) (int, error) {
	return 1, nil
}

func (p *fakeProvider) Freeze(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
	return p.freeze, p.freezeErr
}

func (p *fakeProvider) ExtendReservation(_ context.Context, req billing.ExtendReservationRequest) (*billing.ExtendReservationResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.extendCalls++
	if p.extend != nil || p.extendErr != nil {
		return p.extend, p.extendErr
	}
	return &billing.ExtendReservationResult{
		Success: true, Sequence: req.Sequence,
		AddedCompletionTokens:  req.AdditionalCompletionTokens,
		TargetCompletionTokens: req.TargetCompletionTokens,
		Amount:                 1,
	}, nil
}

func (p *fakeProvider) BeginStreamDelivery(context.Context, billing.StreamDeliveryStart) error {
	return nil
}
func (p *fakeProvider) RenewStreamDelivery(context.Context, billing.StreamDeliveryStart) error {
	return nil
}
func (p *fakeProvider) CheckpointStreamDelivery(context.Context, billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error) {
	return &billing.StreamCheckpointResult{Sequence: 1}, nil
}
func (p *fakeProvider) FinishStreamDelivery(context.Context, billing.StreamDeliveryStart, string, string) error {
	return nil
}

func (p *fakeProvider) BeginProviderAttempt(_ context.Context, req billing.ProviderAttemptStart) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.beginReq = req
	return nil
}

func TestP6F40BProviderAttemptAuthorityPassesThroughCapabilityBilling(t *testing.T) {
	provider := &fakeProvider{}
	service := New(provider, nil)
	start := billing.ProviderAttemptStart{RequestID: "op_cap_attempt", AttemptID: "att_cap_attempt", OrgID: 7}
	completion := billing.ProviderAttemptCompletion{RequestID: start.RequestID, AttemptID: start.AttemptID, OrgID: 7, Outcome: billing.ProviderAttemptOutcomeUnknown}
	if err := service.BeginProviderAttempt(t.Context(), start); err != nil {
		t.Fatalf("begin provider attempt: %v", err)
	}
	if err := service.CompleteProviderAttempt(t.Context(), completion); err != nil {
		t.Fatalf("complete provider attempt: %v", err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.beginReq != start || provider.completeReq != completion {
		t.Fatalf("provider-attempt authority changed across capability billing: begin=%+v complete=%+v", provider.beginReq, provider.completeReq)
	}
}

func (p *fakeProvider) CompleteProviderAttempt(_ context.Context, req billing.ProviderAttemptCompletion) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completeReq = req
	return nil
}

func (p *fakeProvider) PrepareNonStreamDelivery(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
	return &billing.PrepareDeliveryResult{Funded: true, Preview: billing.SettleResult{Success: true}}, nil
}

func (p *fakeProvider) HoldNonStreamOutput(context.Context, billing.SettleRequest, string) error {
	return nil
}

func (p *fakeProvider) RecordDelivery(context.Context, billing.DeliveryEvent) error { return nil }

func (p *fakeProvider) MarkBillableOutput(context.Context, string, int64) error { return nil }

func (p *fakeProvider) PersistSettlementUsage(context.Context, billing.SettleRequest) error {
	return nil
}

func (p *fakeProvider) Settle(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.settle, p.settleErr
}

func (p *fakeProvider) Cancel(context.Context, string, int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelCalls++
	return p.cancelErr
}

type fakeRuntime struct {
	mu              sync.Mutex
	reserveErr      error
	finalizeErr     error
	cancelErr       error
	reserveCalls    int
	finalizeCalls   int
	cancelCalls     int
	leaseTTL        time.Duration
	reservedTokens  int64
	reservedCost    int64
	extendErr       error
	extendCalls     int
	extendedTokens  int64
	extendedCost    int64
	finalizedTokens int64
	finalizedCost   int64
}

func (r *fakeRuntime) ReserveInvocation(_ context.Context, claims *agentcap.Claims, operationID string, tokens, cost int64, leaseTTL time.Duration) (*agentcap.InvocationReservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reserveCalls++
	r.leaseTTL = leaseTTL
	r.reservedTokens = tokens
	r.reservedCost = cost
	if r.reserveErr != nil {
		return nil, r.reserveErr
	}
	return &agentcap.InvocationReservation{
		OperationID:        operationID,
		WorkflowID:         claims.WorkflowID,
		WorkflowGeneration: claims.WorkflowGeneration,
		AgentID:            claims.AgentID,
		OrgID:              claims.OrgID,
		Lineage:            append([]string(nil), claims.Lineage...),
		ReservedTokens:     tokens,
		ReservedCostCents:  cost,
		LeaseExpiresAt:     time.Now().UTC().Add(leaseTTL + agentcap.InvocationFinalizationGrace),
		StateExpiresAt:     time.Now().UTC().Add(leaseTTL + 2*agentcap.InvocationFinalizationGrace),
	}, nil
}

func (r *fakeRuntime) ExtendInvocation(_ context.Context, reservation *agentcap.InvocationReservation, sequence int, tokens, cost int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.extendCalls++
	r.extendedTokens += tokens
	r.extendedCost += cost
	if r.extendErr != nil {
		return r.extendErr
	}
	reservation.ReservedTokens += tokens
	reservation.ReservedCostCents += cost
	reservation.ExtensionSequence = sequence
	return nil
}

func TestP6F11AgentAncestorExtensionRunsAfterWalletAndRetainsConservativeFreeze(t *testing.T) {
	provider := successfulProvider()
	runtime := &fakeRuntime{}
	service := New(provider, runtime)
	requestID := "req_cap_stream_extension"
	if result, err := service.Freeze(capabilityContext(time.Minute), billing.FreezeRequest{
		RequestID: requestID, OrgID: 7, EstimatedTokens: 200,
	}); err != nil || result == nil || !result.Success {
		t.Fatalf("freeze = %+v / %v", result, err)
	}
	runtime.extendErr = agentcap.ErrTokenBudgetExceeded
	result, err := service.ExtendReservation(t.Context(), billing.ExtendReservationRequest{
		RequestID: requestID, OrgID: 7, Sequence: 1,
		AdditionalCompletionTokens: 1_000, TargetCompletionTokens: 1_100,
	})
	if err != nil || result == nil || result.Success || result.ErrorCode != ErrorCodeTokenBudgetExceeded {
		t.Fatalf("ancestor extension denial = %+v / %v", result, err)
	}
	provider.mu.Lock()
	walletCalls := provider.extendCalls
	provider.mu.Unlock()
	if walletCalls != 1 || runtime.extendCalls != 1 || runtime.extendedTokens != 1_000 || runtime.extendedCost != 1 {
		t.Fatalf("wallet/ancestor extension chain missing: wallet=%d runtime=%+v", walletCalls, runtime)
	}
}

func (r *fakeRuntime) FinalizeInvocation(_ context.Context, _ *agentcap.InvocationReservation, tokens, cost int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalizeCalls++
	r.finalizedTokens = tokens
	r.finalizedCost = cost
	return r.finalizeErr
}

func (r *fakeRuntime) CancelInvocation(context.Context, *agentcap.InvocationReservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelCalls++
	return r.cancelErr
}

func capabilityContext(ttl time.Duration) context.Context {
	claims := &agentcap.Claims{
		WorkflowID:         "capwf_0123456789abcdef0123456789abcdef",
		WorkflowGeneration: "gen_test",
		AgentID:            "agent:child",
		OrgID:              7,
		Lineage:            []string{"agent:root", "agent:child"},
	}
	ctx := context.WithValue(context.Background(), middleware.CtxAgentCapability, claims)
	return agentcap.ContextWithInvocationLeaseTTL(ctx, ttl)
}

func successfulProvider() *fakeProvider {
	return &fakeProvider{
		freeze: &billing.FreezeResult{Success: true, Amount: 25},
		settle: &billing.SettleResult{Success: true, ActualCost: 17, ChargedCost: 17},
	}
}

type fakeTerminalHooks struct {
	registerErr    error
	reconcileErr   error
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
	return h.reconcileErr
}

func TestP6F20DurableHookOwnsAgentTerminalRecovery(t *testing.T) {
	provider := successfulProvider()
	runtime := &fakeRuntime{}
	hooks := &fakeTerminalHooks{}
	service := New(provider, runtime)
	service.SetTerminalHooks(hooks)
	ctx := capabilityContext(30 * time.Minute)

	result, err := service.Freeze(ctx, billing.FreezeRequest{RequestID: "req_p6f20_agent", OrgID: 7, EstimatedTokens: 300})
	if err != nil || result == nil || !result.Success {
		t.Fatalf("freeze = %#v / %v", result, err)
	}
	if hooks.registerCalls != 1 || hooks.hookType != terminalhook.HookAgentAncestor ||
		hooks.operationID != "req_p6f20_agent" || hooks.orgID != 7 || hooks.expiresAt.IsZero() {
		t.Fatalf("unexpected terminal hook registration: %#v", hooks)
	}
	if _, ok := hooks.reservation.(*agentcap.InvocationReservation); !ok {
		t.Fatalf("terminal hook reservation type = %T", hooks.reservation)
	}

	settled, err := service.Settle(context.Background(), billing.SettleRequest{
		RequestID: "req_p6f20_agent", OrgID: 7, PromptTokens: 120, CompletionTokens: 80,
	})
	if err != nil || settled == nil || !settled.Success {
		t.Fatalf("settle = %#v / %v", settled, err)
	}
	if hooks.reconcileCalls != 1 {
		t.Fatalf("terminal reconciliations = %d, want 1", hooks.reconcileCalls)
	}
	if runtime.finalizeCalls != 1 || runtime.cancelCalls != 0 {
		t.Fatalf("immediate idempotent fast path did not complement durable recovery: %#v", runtime)
	}
	if _, exists := service.reservations.Load("req_p6f20_agent"); exists {
		t.Fatal("durably owned reservation remained in process memory")
	}
}

func TestP6F20AgentHookRegistrationFailureRollsBackAdmissionAndWallet(t *testing.T) {
	provider := successfulProvider()
	runtime := &fakeRuntime{}
	hooks := &fakeTerminalHooks{registerErr: errors.New("database unavailable")}
	service := New(provider, runtime)
	service.SetTerminalHooks(hooks)

	result, err := service.Freeze(capabilityContext(time.Minute), billing.FreezeRequest{
		RequestID: "req_p6f20_hook_failure", OrgID: 7, EstimatedTokens: 10,
	})
	if err == nil || result != nil {
		t.Fatalf("hook registration failure = %#v / %v", result, err)
	}
	if runtime.cancelCalls != 1 || provider.cancelCalls != 1 {
		t.Fatalf("runtime cancels=%d billing cancels=%d", runtime.cancelCalls, provider.cancelCalls)
	}
}

func TestCapabilityFreezeAndSettleJoinAncestorBudgetToFinancialTerminal(t *testing.T) {
	provider := successfulProvider()
	runtime := &fakeRuntime{}
	service := New(provider, runtime)
	ctx := capabilityContext(30 * time.Minute)

	result, err := service.Freeze(ctx, billing.FreezeRequest{RequestID: "req_joined", OrgID: 7, EstimatedTokens: 300})
	if err != nil || result == nil || !result.Success {
		t.Fatalf("freeze = %#v / %v", result, err)
	}
	if runtime.reserveCalls != 1 || runtime.reservedTokens != 300 || runtime.reservedCost != 25 || runtime.leaseTTL != 30*time.Minute {
		t.Fatalf("unexpected runtime reservation: %#v", runtime)
	}
	settled, err := service.Settle(context.Background(), billing.SettleRequest{RequestID: "req_joined", OrgID: 7, PromptTokens: 120, CompletionTokens: 80})
	if err != nil || settled == nil || !settled.Success {
		t.Fatalf("settle = %#v / %v", settled, err)
	}
	if runtime.finalizeCalls != 1 || runtime.finalizedTokens != 200 || runtime.finalizedCost != 17 || runtime.cancelCalls != 0 {
		t.Fatalf("unexpected runtime finalization: %#v", runtime)
	}
	if _, exists := service.reservations.Load("req_joined"); exists {
		t.Fatal("terminal reservation tracker was not released")
	}
}

func TestRootAPIKeyBillingBypassesAgentRuntime(t *testing.T) {
	provider := successfulProvider()
	runtime := &fakeRuntime{}
	service := New(provider, runtime)
	result, err := service.Freeze(context.Background(), billing.FreezeRequest{RequestID: "req_root", OrgID: 7, EstimatedTokens: 10})
	if err != nil || !result.Success {
		t.Fatalf("root freeze = %#v / %v", result, err)
	}
	if _, err := service.Settle(context.Background(), billing.SettleRequest{RequestID: "req_root", OrgID: 7}); err != nil {
		t.Fatal(err)
	}
	if runtime.reserveCalls != 0 || runtime.finalizeCalls != 0 || runtime.cancelCalls != 0 {
		t.Fatalf("ordinary root billing touched Agent runtime: %#v", runtime)
	}
}

func TestRuntimeAdmissionDenialCancelsWalletFreezeAndReturnsTypedCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "cost", err: agentcap.ErrCostBudgetExceeded, code: ErrorCodeCostBudgetExceeded},
		{name: "tokens", err: agentcap.ErrTokenBudgetExceeded, code: ErrorCodeTokenBudgetExceeded},
		{name: "rps", err: agentcap.ErrRPSLimitExceeded, code: ErrorCodeRPSLimitExceeded},
		{name: "rpm", err: agentcap.ErrRPMLimitExceeded, code: ErrorCodeRPMLimitExceeded},
		{name: "concurrency", err: agentcap.ErrConcurrencyExceeded, code: ErrorCodeConcurrencyExceeded},
		{name: "revoked", err: agentcap.ErrRevoked, code: ErrorCodeRevoked},
		{name: "unavailable", err: agentcap.ErrUnavailable, code: ErrorCodeStateUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := successfulProvider()
			runtime := &fakeRuntime{reserveErr: test.err}
			service := New(provider, runtime)
			result, err := service.Freeze(capabilityContext(time.Minute), billing.FreezeRequest{RequestID: "req_denied_" + test.name, OrgID: 7, EstimatedTokens: 10})
			if err != nil || result == nil || result.Success || result.ErrorCode != test.code {
				t.Fatalf("denial = %#v / %v, want %s", result, err, test.code)
			}
			if provider.cancelCalls != 1 {
				t.Fatalf("wallet freeze cancellation calls=%d, want 1", provider.cancelCalls)
			}
		})
	}
}

func TestPreSettlementCancelReleasesAncestorReservation(t *testing.T) {
	provider := successfulProvider()
	runtime := &fakeRuntime{}
	service := New(provider, runtime)
	if _, err := service.Freeze(capabilityContext(time.Minute), billing.FreezeRequest{RequestID: "req_cancel", OrgID: 7, EstimatedTokens: 10}); err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(context.Background(), "req_cancel", 7); err != nil {
		t.Fatal(err)
	}
	if runtime.cancelCalls != 1 || runtime.finalizeCalls != 0 {
		t.Fatalf("unexpected runtime terminal calls: %#v", runtime)
	}
}

func TestSettlementFinalizationFailureDoesNotUndoFinancialSuccess(t *testing.T) {
	provider := successfulProvider()
	runtime := &fakeRuntime{finalizeErr: agentcap.ErrUnavailable}
	service := New(provider, runtime)
	if _, err := service.Freeze(capabilityContext(time.Minute), billing.FreezeRequest{RequestID: "req_retry_finalize", OrgID: 7, EstimatedTokens: 10}); err != nil {
		t.Fatal(err)
	}
	result, err := service.Settle(context.Background(), billing.SettleRequest{RequestID: "req_retry_finalize", OrgID: 7, PromptTokens: 4, CompletionTokens: 6})
	if err != nil || result == nil || !result.Success {
		t.Fatalf("financial success must be preserved: %#v / %v", result, err)
	}
	runtime.mu.Lock()
	runtime.finalizeErr = nil
	runtime.mu.Unlock()
	if err := service.Cancel(context.Background(), "req_retry_finalize", 7); err != nil {
		t.Fatal(err)
	}
	if runtime.finalizeCalls != 2 || runtime.cancelCalls != 0 {
		t.Fatalf("settled reservation was refunded instead of finalized: %#v", runtime)
	}
}

func TestFailedSettlementThenCancelRetainsReservationConservatively(t *testing.T) {
	provider := successfulProvider()
	provider.settleErr = errors.New("temporary settlement failure")
	runtime := &fakeRuntime{}
	service := New(provider, runtime)
	if _, err := service.Freeze(capabilityContext(time.Minute), billing.FreezeRequest{RequestID: "req_ambiguous", OrgID: 7, EstimatedTokens: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Settle(context.Background(), billing.SettleRequest{RequestID: "req_ambiguous", OrgID: 7, PromptTokens: 5, CompletionTokens: 5}); err == nil {
		t.Fatal("expected settlement failure")
	}
	if err := service.Cancel(context.Background(), "req_ambiguous", 7); err != nil {
		t.Fatal(err)
	}
	if runtime.cancelCalls != 0 || runtime.finalizeCalls != 0 {
		t.Fatalf("ambiguous recovery boundary must retain, not refund, ancestor budget: %#v", runtime)
	}
	if _, exists := service.reservations.Load("req_ambiguous"); exists {
		t.Fatal("ambiguous local tracker must not remain unbounded")
	}
}
