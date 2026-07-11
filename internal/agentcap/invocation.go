package agentcap

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

type invocationLeaseContextKey struct{}

// InvocationReservation is a server-owned handle for one admitted model
// operation. It contains no bearer token or external-system authority.
type InvocationReservation struct {
	OperationID        string
	WorkflowID         string
	WorkflowGeneration string
	AgentID            string
	OrgID              int64
	Lineage            []string
	ReservedTokens     int64
	ReservedCostCents  int64
	LeaseExpiresAt     time.Time
	StateExpiresAt     time.Time
}

// ContextWithInvocationLeaseTTL records the transport's maximum expected
// lifetime so the shared concurrency lease cannot expire while a valid stream
// is still active.
func ContextWithInvocationLeaseTTL(ctx context.Context, ttl time.Duration) context.Context {
	return context.WithValue(ctx, invocationLeaseContextKey{}, ttl)
}

func InvocationLeaseTTLFromContext(ctx context.Context) time.Duration {
	if ctx == nil {
		return DefaultInvocationLeaseTTL
	}
	ttl, _ := ctx.Value(invocationLeaseContextKey{}).(time.Duration)
	if ttl <= 0 {
		return DefaultInvocationLeaseTTL
	}
	return ttl
}

// ReserveInvocation atomically charges one new request against every Agent in
// the signed lineage. Rate attempts are not refunded; cost/token reservations
// and concurrency are released by a financial terminal hook.
func (s *Service) ReserveInvocation(ctx context.Context, claims *Claims, operationID string, estimatedTokens, reservedCostCents int64, leaseTTL time.Duration) (*InvocationReservation, error) {
	if s == nil || claims == nil || strings.TrimSpace(operationID) == "" || len(operationID) > 64 ||
		estimatedTokens <= 0 || reservedCostCents <= 0 || estimatedTokens > MaxExactRuntimeBudget ||
		reservedCostCents > MaxExactRuntimeBudget {
		return nil, ErrInvalidRequest
	}
	if err := validateClaims(claims, s.tokenTTL); err != nil || claims.ExpiresAt == nil || !claims.ExpiresAt.Time.After(s.now()) {
		return nil, ErrInvalidToken
	}
	if leaseTTL <= 0 {
		leaseTTL = DefaultInvocationLeaseTTL
	}
	if leaseTTL > MaxInvocationLeaseTTL {
		return nil, fmt.Errorf("%w: invocation lease exceeds %s", ErrInvalidRequest, MaxInvocationLeaseTTL)
	}

	now := s.now().UTC()
	leaseExpiresAt := now.Add(leaseTTL + InvocationFinalizationGrace)
	stateExpiresAt := leaseExpiresAt.Add(InvocationFinalizationGrace)
	reservation := &InvocationReservation{
		OperationID:        operationID,
		WorkflowID:         claims.WorkflowID,
		WorkflowGeneration: claims.WorkflowGeneration,
		AgentID:            claims.AgentID,
		OrgID:              claims.OrgID,
		Lineage:            slices.Clone(claims.Lineage),
		ReservedTokens:     estimatedTokens,
		ReservedCostCents:  reservedCostCents,
		LeaseExpiresAt:     leaseExpiresAt,
		StateExpiresAt:     stateExpiresAt,
	}
	if err := s.reserveInvocation(ctx, claims, reservation, leaseTTL+InvocationFinalizationGrace, stateExpiresAt.Sub(now)); err != nil {
		return nil, err
	}
	return reservation, nil
}

func (s *Service) FinalizeInvocation(ctx context.Context, reservation *InvocationReservation, actualTokens, actualCostCents int64) error {
	if err := validateInvocationReservation(reservation); err != nil || actualTokens < 0 || actualCostCents < 0 ||
		actualTokens > MaxExactRuntimeBudget || actualCostCents > MaxExactRuntimeBudget {
		return ErrInvalidRequest
	}
	return s.finalizeInvocation(ctx, reservation, actualTokens, actualCostCents)
}

func (s *Service) CancelInvocation(ctx context.Context, reservation *InvocationReservation) error {
	if err := validateInvocationReservation(reservation); err != nil {
		return ErrInvalidRequest
	}
	return s.cancelInvocation(ctx, reservation)
}

func validateInvocationReservation(reservation *InvocationReservation) error {
	if reservation == nil || strings.TrimSpace(reservation.OperationID) == "" || len(reservation.OperationID) > 64 ||
		!validWorkflowID(reservation.WorkflowID) || reservation.WorkflowGeneration == "" || reservation.AgentID == "" ||
		reservation.OrgID <= 0 || len(reservation.Lineage) == 0 || reservation.Lineage[len(reservation.Lineage)-1] != reservation.AgentID ||
		reservation.ReservedTokens <= 0 || reservation.ReservedCostCents <= 0 {
		return ErrInvalidRequest
	}
	seen := make(map[string]struct{}, len(reservation.Lineage))
	for _, agentID := range reservation.Lineage {
		if err := validateAgentID(agentID); err != nil {
			return ErrInvalidRequest
		}
		if _, exists := seen[agentID]; exists {
			return ErrInvalidRequest
		}
		seen[agentID] = struct{}{}
	}
	return nil
}
