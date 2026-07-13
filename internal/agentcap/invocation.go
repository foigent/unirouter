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
	OperationID        string    `json:"operation_id"`
	WorkflowID         string    `json:"workflow_id"`
	WorkflowGeneration string    `json:"workflow_generation"`
	AgentID            string    `json:"agent_id"`
	OrgID              int64     `json:"org_id"`
	Lineage            []string  `json:"lineage"`
	ReservedTokens     int64     `json:"reserved_tokens"`
	ReservedCostCents  int64     `json:"reserved_cost_cents"`
	ExtensionSequence  int       `json:"extension_sequence"`
	LeaseExpiresAt     time.Time `json:"lease_expires_at"`
	StateExpiresAt     time.Time `json:"state_expires_at"`
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

// ExtendInvocation grows every Agent ancestor reservation before another
// stream batch may be disclosed. Sequence makes an ambiguous Redis retry
// idempotent without turning the reservation handle into external authority.
func (s *Service) ExtendInvocation(ctx context.Context, reservation *InvocationReservation, sequence int, additionalTokens, additionalCostCents int64) error {
	if err := validateInvocationReservation(reservation); err != nil || sequence <= 0 ||
		additionalTokens <= 0 || additionalCostCents < 0 ||
		additionalTokens > MaxExactRuntimeBudget || additionalCostCents > MaxExactRuntimeBudget ||
		reservation.ReservedTokens > MaxExactRuntimeBudget-additionalTokens ||
		reservation.ReservedCostCents > MaxExactRuntimeBudget-additionalCostCents {
		return ErrInvalidRequest
	}
	if reservation.ExtensionSequence > sequence {
		return ErrConflict
	}
	if err := s.extendInvocation(ctx, reservation, sequence, additionalTokens, additionalCostCents); err != nil {
		return err
	}
	if reservation.ExtensionSequence < sequence {
		reservation.ReservedTokens += additionalTokens
		reservation.ReservedCostCents += additionalCostCents
		reservation.ExtensionSequence = sequence
	}
	return nil
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
		reservation.ReservedTokens <= 0 || reservation.ReservedCostCents <= 0 || reservation.ExtensionSequence < 0 {
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
