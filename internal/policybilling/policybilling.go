package policybilling

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
)

const (
	ErrorCodeRPSExceeded         = "limit_policy.rps_exceeded"
	ErrorCodeRPMExceeded         = "limit_policy.rpm_exceeded"
	ErrorCodeTPMExceeded         = "limit_policy.tpm_exceeded"
	ErrorCodeConcurrencyExceeded = "limit_policy.concurrency_exceeded"
	ErrorCodeStateUnavailable    = "limit_policy.state_unavailable"
	ErrorCodeConflict            = "limit_policy.operation_conflict"

	terminalTimeout = 2 * time.Second
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

type Runtime interface {
	Reserve(ctx context.Context, request limitpolicy.AdmissionRequest) (*limitpolicy.Reservation, error)
	Finalize(ctx context.Context, reservation *limitpolicy.Reservation, actualTokens int64) error
	Cancel(ctx context.Context, reservation *limitpolicy.Reservation) error
	CancelBeforeDispatch(ctx context.Context, reservation *limitpolicy.Reservation) error
}

// Billing joins shared capacity/token admission to the existing durable wallet
// and Agent-ancestor billing chain. It does not change financial truth.
type Billing struct {
	inner        Provider
	runtime      Runtime
	reservations sync.Map
}

type trackedReservation struct {
	mu              sync.Mutex
	reservation     *limitpolicy.Reservation
	settleAttempted bool
	terminal        string
	actualTokens    int64
}

func New(inner Provider, runtime Runtime) *Billing {
	return &Billing{inner: inner, runtime: runtime}
}

func (b *Billing) QueryModelPriceForOrg(ctx context.Context, orgID int64, model string) (float64, float64, error) {
	return b.inner.QueryModelPriceForOrg(ctx, orgID, model)
}

func (b *Billing) CalculateCost(ctx context.Context, model string, promptTokens, completionTokens int) (int, error) {
	return b.inner.CalculateCost(ctx, model, promptTokens, completionTokens)
}

func (b *Billing) Freeze(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
	if b.runtime == nil {
		return rejectedResult(limitpolicy.ErrStateUnavailable), nil
	}
	inputTokens := req.EstimatedInputTokens
	reservedOutput := req.ReservedCompletionTokens
	if inputTokens <= 0 && reservedOutput <= 0 {
		inputTokens = req.EstimatedTokens
	}
	apiKey := middleware.GetAPIKeyFromContext(ctx)
	if apiKey == nil {
		return rejectedResult(limitpolicy.ErrInvalidRequest), nil
	}
	reservation, reserveErr := b.runtime.Reserve(ctx, limitpolicy.AdmissionRequest{
		OperationID: req.RequestID,
		Subject: limitpolicy.Subject{
			OrgID: middleware.GetOrgIDFromContext(ctx), ProjectID: middleware.GetProjectIDFromContext(ctx),
			APIKeyID: middleware.GetKeyIDFromContext(ctx), Model: req.Model,
			BaseRPSLimit: int64(apiKey.QPSLimit), BaseRPMLimit: int64(apiKey.RPMLimit),
		},
		EstimatedInput: int64(inputTokens), ReservedOutput: int64(reservedOutput),
		LeaseTTL: agentcap.InvocationLeaseTTLFromContext(ctx),
	})
	if reserveErr != nil {
		return rejectedResult(reserveErr), nil
	}

	result, err := b.inner.Freeze(ctx, req)
	if result == nil && err == nil {
		err = errors.New("billing freeze returned no result")
	}
	if err != nil || result == nil || !result.Success {
		if !reservation.Idempotent {
			cleanupCtx, cancel := detachedTerminalContext(ctx)
			cleanupErr := b.runtime.CancelBeforeDispatch(cleanupCtx, reservation)
			cancel()
			if cleanupErr != nil {
				if err != nil {
					return result, fmt.Errorf("billing freeze: %w; distributed policy rollback: %v", err, cleanupErr)
				}
				log.Printf("[limit-policy] operation=%s pre-dispatch reservation retained conservatively: %v", req.RequestID, cleanupErr)
			}
		}
		return result, err
	}

	tracked := &trackedReservation{reservation: reservation}
	if existing, loaded := b.reservations.LoadOrStore(req.RequestID, tracked); loaded {
		current, ok := existing.(*trackedReservation)
		if !ok || current.reservation == nil || current.reservation.OperationID != reservation.OperationID {
			return nil, fmt.Errorf("distributed policy reservation identity conflict for operation %s", req.RequestID)
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
		return nil, fmt.Errorf("invalid distributed policy reservation tracker")
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
	if err := b.finalizeTracked(ctx, req.RequestID, state); err != nil {
		log.Printf("[limit-policy] operation=%s terminal state retained conservatively: %v", req.RequestID, err)
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
		return fmt.Errorf("invalid distributed policy reservation tracker")
	}
	state.mu.Lock()
	defer state.mu.Unlock()

	if err := b.inner.Cancel(ctx, requestID, orgID); err != nil {
		return err
	}
	if state.terminal == "settled" {
		if err := b.finalizeTracked(ctx, requestID, state); err != nil {
			log.Printf("[limit-policy] operation=%s settled terminal state retained conservatively: %v", requestID, err)
		}
		return nil
	}
	if state.settleAttempted {
		// P6-F20 will replace this ambiguous cross-process recovery boundary
		// with a durable terminal hook. Until then, retain quota/concurrency
		// only for its bounded Redis TTL rather than risk quota overspend.
		b.reservations.Delete(requestID)
		log.Printf("[limit-policy] operation=%s reservation retained after ambiguous billing recovery boundary", requestID)
		return nil
	}
	state.terminal = "cancelled"
	terminalCtx, cancel := detachedTerminalContext(ctx)
	defer cancel()
	if err := b.runtime.Cancel(terminalCtx, state.reservation); err != nil {
		b.reservations.Delete(requestID)
		log.Printf("[limit-policy] operation=%s cancellation retained conservatively: %v", requestID, err)
		return nil
	}
	b.reservations.Delete(requestID)
	return nil
}

func (b *Billing) finalizeTracked(ctx context.Context, requestID string, state *trackedReservation) error {
	terminalCtx, cancel := detachedTerminalContext(ctx)
	defer cancel()
	err := b.runtime.Finalize(terminalCtx, state.reservation, state.actualTokens)
	// Redis already bounds retained TPM/concurrency state by TTL. Do not turn a
	// terminal Redis outage into an unbounded in-process tracker leak; P6-F20
	// adds the durable cross-process retry hook separately.
	b.reservations.Delete(requestID)
	return err
}

func rejectedResult(err error) *billing.FreezeResult {
	return &billing.FreezeResult{
		Success: false, ErrorCode: errorCode(err), ErrorDetail: "Distributed limit-policy admission denied",
	}
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, limitpolicy.ErrRPSExceeded):
		return ErrorCodeRPSExceeded
	case errors.Is(err, limitpolicy.ErrRPMExceeded):
		return ErrorCodeRPMExceeded
	case errors.Is(err, limitpolicy.ErrTPMExceeded):
		return ErrorCodeTPMExceeded
	case errors.Is(err, limitpolicy.ErrConcurrencyExceeded):
		return ErrorCodeConcurrencyExceeded
	case errors.Is(err, limitpolicy.ErrConflict):
		return ErrorCodeConflict
	default:
		return ErrorCodeStateUnavailable
	}
}

func detachedTerminalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, terminalTimeout)
}
