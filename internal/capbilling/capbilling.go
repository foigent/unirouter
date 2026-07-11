package capbilling

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
)

const (
	ErrorCodeCostBudgetExceeded  = "agent_capability.cost_budget_exceeded"
	ErrorCodeTokenBudgetExceeded = "agent_capability.token_budget_exceeded"
	ErrorCodeRPSLimitExceeded    = "agent_capability.rps_limit_exceeded"
	ErrorCodeRPMLimitExceeded    = "agent_capability.rpm_limit_exceeded"
	ErrorCodeConcurrencyExceeded = "agent_capability.concurrency_exceeded"
	ErrorCodeStateUnavailable    = "agent_capability.runtime_state_unavailable"
	ErrorCodeRevoked             = "agent_capability.runtime_revoked"
	ErrorCodeConflict            = "agent_capability.runtime_conflict"

	runtimeTerminalTimeout = 2 * time.Second
	billingCleanupTimeout  = 10 * time.Second
)

type Provider interface {
	QueryModelPriceForOrg(ctx context.Context, orgID int64, model string) (float64, float64, error)
	CalculateCost(ctx context.Context, model string, promptTokens, completionTokens int) (int, error)
	Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error)
	MarkBillableOutput(ctx context.Context, requestID string, orgID int64) error
	PersistSettlementUsage(ctx context.Context, req billing.SettleRequest) error
	Settle(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error)
	Cancel(ctx context.Context, requestID string, orgID int64) error
}

type RuntimeBudget interface {
	ReserveInvocation(ctx context.Context, claims *agentcap.Claims, operationID string, estimatedTokens, reservedCostCents int64, leaseTTL time.Duration) (*agentcap.InvocationReservation, error)
	FinalizeInvocation(ctx context.Context, reservation *agentcap.InvocationReservation, actualTokens, actualCostCents int64) error
	CancelInvocation(ctx context.Context, reservation *agentcap.InvocationReservation) error
}

// Billing joins the durable wallet terminal state to the shared Agent
// ancestor reservation without changing ordinary root API-key billing.
type Billing struct {
	inner        Provider
	runtime      RuntimeBudget
	reservations sync.Map
}

type trackedReservation struct {
	mu              sync.Mutex
	reservation     *agentcap.InvocationReservation
	settleAttempted bool
	terminal        string
	actualTokens    int64
	actualCostCents int64
}

func New(inner Provider, runtime RuntimeBudget) *Billing {
	return &Billing{inner: inner, runtime: runtime}
}

func (b *Billing) QueryModelPriceForOrg(ctx context.Context, orgID int64, model string) (float64, float64, error) {
	return b.inner.QueryModelPriceForOrg(ctx, orgID, model)
}

func (b *Billing) CalculateCost(ctx context.Context, model string, promptTokens, completionTokens int) (int, error) {
	return b.inner.CalculateCost(ctx, model, promptTokens, completionTokens)
}

func (b *Billing) Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
	result, err := b.inner.Freeze(ctx, req)
	if err != nil || result == nil || !result.Success {
		return result, err
	}
	claims := middleware.GetAgentCapabilityFromContext(ctx)
	if claims == nil {
		return result, nil
	}
	if b.runtime == nil {
		if cancelErr := b.cancelFrozen(ctx, req.RequestID, req.OrgID); cancelErr != nil {
			return nil, fmt.Errorf("Agent runtime budget is unavailable; cancel frozen billing: %w", cancelErr)
		}
		return runtimeRejectedResult(agentcap.ErrUnavailable), nil
	}

	reservation, reserveErr := b.runtime.ReserveInvocation(
		ctx,
		claims,
		req.RequestID,
		int64(req.EstimatedTokens),
		int64(result.Amount),
		agentcap.InvocationLeaseTTLFromContext(ctx),
	)
	if reserveErr != nil {
		if cancelErr := b.cancelFrozen(ctx, req.RequestID, req.OrgID); cancelErr != nil {
			return nil, fmt.Errorf("reserve Agent ancestor budget: %v; cancel frozen billing: %w", reserveErr, cancelErr)
		}
		return runtimeRejectedResult(reserveErr), nil
	}

	tracked := &trackedReservation{reservation: reservation}
	if existing, loaded := b.reservations.LoadOrStore(req.RequestID, tracked); loaded {
		current, ok := existing.(*trackedReservation)
		if !ok || !sameReservation(current.reservation, reservation) {
			cleanupCtx, cancel := runtimeContext(ctx)
			defer cancel()
			_ = b.runtime.CancelInvocation(cleanupCtx, reservation)
			return nil, fmt.Errorf("Agent runtime reservation identity conflict for operation %s", req.RequestID)
		}
	}
	return result, nil
}

func (b *Billing) MarkBillableOutput(ctx context.Context, requestID string, orgID int64) error {
	return b.inner.MarkBillableOutput(ctx, requestID, orgID)
}

func (b *Billing) PersistSettlementUsage(ctx context.Context, req billing.SettleRequest) error {
	return b.inner.PersistSettlementUsage(ctx, req)
}

func (b *Billing) Settle(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
	value, tracked := b.reservations.Load(req.RequestID)
	if !tracked {
		return b.inner.Settle(ctx, req)
	}
	state, ok := value.(*trackedReservation)
	if !ok {
		return nil, fmt.Errorf("invalid Agent runtime reservation tracker")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.settleAttempted = true

	result, err := b.inner.Settle(ctx, req)
	if err != nil || result == nil || !result.Success {
		return result, err
	}
	state.terminal = "settled"
	state.actualTokens = int64(req.PromptTokens) + int64(req.CompletionTokens)
	state.actualCostCents = int64(result.ActualCost)
	if err := b.finalizeTracked(ctx, req.RequestID, state); err != nil {
		log.Printf("[agent-capability] operation=%s ancestor budget finalization retained conservatively: %v", req.RequestID, err)
	}
	return result, nil
}

func (b *Billing) Cancel(ctx context.Context, requestID string, orgID int64) error {
	value, tracked := b.reservations.Load(requestID)
	if !tracked {
		return b.inner.Cancel(ctx, requestID, orgID)
	}
	state, ok := value.(*trackedReservation)
	if !ok {
		return fmt.Errorf("invalid Agent runtime reservation tracker")
	}
	state.mu.Lock()
	defer state.mu.Unlock()

	if err := b.inner.Cancel(ctx, requestID, orgID); err != nil {
		return err
	}
	if state.terminal == "settled" {
		if err := b.finalizeTracked(ctx, requestID, state); err != nil {
			log.Printf("[agent-capability] operation=%s settled ancestor budget finalization retained conservatively: %v", requestID, err)
		}
		return nil
	}
	if state.settleAttempted {
		// The durable recovery loop uses the underlying billing service directly.
		// Once settlement was attempted, a later nil Cancel result cannot prove
		// whether cancellation or a concurrent recovery won. Retaining the
		// reservation until its bounded Redis TTL prevents quota overspend; P6-F20
		// will replace this conservative crash/recovery gap with a durable hook.
		b.reservations.Delete(requestID)
		log.Printf("[agent-capability] operation=%s ancestor reservation retained after ambiguous billing recovery boundary", requestID)
		return nil
	}
	state.terminal = "cancelled"
	terminalCtx, cancel := runtimeContext(ctx)
	defer cancel()
	if err := b.runtime.CancelInvocation(terminalCtx, state.reservation); err != nil {
		log.Printf("[agent-capability] operation=%s ancestor reservation cancellation retained conservatively: %v", requestID, err)
		return nil
	}
	b.reservations.Delete(requestID)
	return nil
}

func (b *Billing) finalizeTracked(ctx context.Context, requestID string, state *trackedReservation) error {
	terminalCtx, cancel := runtimeContext(ctx)
	defer cancel()
	if err := b.runtime.FinalizeInvocation(terminalCtx, state.reservation, state.actualTokens, state.actualCostCents); err != nil {
		return err
	}
	b.reservations.Delete(requestID)
	return nil
}

func (b *Billing) cancelFrozen(ctx context.Context, requestID string, orgID int64) error {
	cleanupBase := context.Background()
	if ctx != nil {
		cleanupBase = context.WithoutCancel(ctx)
	}
	cleanupCtx, cancel := context.WithTimeout(cleanupBase, billingCleanupTimeout)
	defer cancel()
	return b.inner.Cancel(cleanupCtx, requestID, orgID)
}

func runtimeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, runtimeTerminalTimeout)
}

func runtimeRejectedResult(err error) *billing.FreezeResult {
	return &billing.FreezeResult{
		Success:     false,
		ErrorCode:   runtimeErrorCode(err),
		ErrorDetail: "Agent capability runtime admission denied",
	}
}

func runtimeErrorCode(err error) string {
	switch {
	case errors.Is(err, agentcap.ErrCostBudgetExceeded):
		return ErrorCodeCostBudgetExceeded
	case errors.Is(err, agentcap.ErrTokenBudgetExceeded):
		return ErrorCodeTokenBudgetExceeded
	case errors.Is(err, agentcap.ErrRPSLimitExceeded):
		return ErrorCodeRPSLimitExceeded
	case errors.Is(err, agentcap.ErrRPMLimitExceeded):
		return ErrorCodeRPMLimitExceeded
	case errors.Is(err, agentcap.ErrConcurrencyExceeded):
		return ErrorCodeConcurrencyExceeded
	case errors.Is(err, agentcap.ErrRevoked), errors.Is(err, agentcap.ErrInvalidToken), errors.Is(err, agentcap.ErrAuthorityChanged), errors.Is(err, agentcap.ErrInactiveRoot):
		return ErrorCodeRevoked
	case errors.Is(err, agentcap.ErrConflict):
		return ErrorCodeConflict
	default:
		return ErrorCodeStateUnavailable
	}
}

func sameReservation(left, right *agentcap.InvocationReservation) bool {
	if left == nil || right == nil {
		return false
	}
	return left.OperationID == right.OperationID && left.WorkflowID == right.WorkflowID &&
		left.WorkflowGeneration == right.WorkflowGeneration && left.AgentID == right.AgentID &&
		left.OrgID == right.OrgID && left.ReservedTokens == right.ReservedTokens &&
		left.ReservedCostCents == right.ReservedCostCents && slices.Equal(left.Lineage, right.Lineage)
}
