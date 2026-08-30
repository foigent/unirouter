package modelcapabilitypublication

import (
	"context"
	"fmt"
	"slices"
)

// PostCommitRecoveryCheck performs an honest read-only static check after the
// original command result or external readback evidence was lost. The caller
// must hold the publication owner's outer serialization window.
func PostCommitRecoveryCheck(ctx context.Context, q rowQuerier, trustedEnvironment string, plan *Plan,
	artifactLedger []string, authority *PlanAuthority,
) (RecoveryCheckResult, error) {
	result := RecoveryCheckResult{
		LedgerState:        LedgerStateUnexpected,
		LedgerRelation:     LedgerRelationUnexpected,
		StaticVerification: StaticVerificationPlanUnavailable,
		DynamicReadback:    DynamicReadbackUnreconstructable,
	}
	actualLedger, err := readLedger(ctx, q)
	if err != nil {
		return result, err
	}
	if !isLedgerPrefix(actualLedger, artifactLedger) {
		return result, fmt.Errorf("%w: recovery database ledger is not an exact artifact prefix", ErrPlanConflict)
	}
	if plan == nil {
		currentStatic, err := FoldedProjectionForLedger(actualLedger)
		if err != nil {
			return result, err
		}
		if err := VerifyStaticProjection(ctx, q, currentStatic); err != nil {
			result.StaticVerification = StaticVerificationMismatch
			return result, err
		}
		return result, nil
	}
	if authority == nil {
		return result, fmt.Errorf("%w: compiled Plan authority is required for recovery check", ErrPlanConflict)
	}
	if err := ValidatePlanAuthority(*authority, *plan); err != nil {
		return result, err
	}
	if plan.Target.Environment != trustedEnvironment {
		return result, fmt.Errorf("%w: recovery Plan environment does not match trusted runtime environment", ErrPlanConflict)
	}
	identity, err := readTargetIdentity(ctx, q, trustedEnvironment)
	if err != nil {
		return result, err
	}
	if identity != plan.Target {
		return result, fmt.Errorf("%w: recovery Plan does not target this database", ErrPlanConflict)
	}
	if slices.Contains(actualLedger, plan.PublicationID) {
		result.LedgerState = LedgerStateExactPublicationPresent
	} else {
		result.LedgerState = LedgerStateMissing
	}
	switch {
	case slices.Equal(actualLedger, plan.ProposedLedger):
		result.LedgerRelation = LedgerRelationExactPlanPostState
		static, err := StaticProjectionFromPlan(*plan)
		if err != nil {
			return result, err
		}
		if err := VerifyStaticProjection(ctx, q, static); err != nil {
			result.StaticVerification = StaticVerificationMismatch
			return result, err
		}
		result.StaticVerification = StaticVerificationForPlan
	case isLedgerPrefix(plan.ProposedLedger, actualLedger) && isLedgerPrefix(actualLedger, artifactLedger):
		result.LedgerRelation = LedgerRelationAdvancedCompatible
		currentStatic, err := recoveryStaticProjection(*plan, actualLedger)
		if err != nil {
			return result, err
		}
		if err := VerifyStaticProjection(ctx, q, currentStatic); err != nil {
			result.StaticVerification = StaticVerificationMismatch
			return result, err
		}
		result.StaticVerification = StaticVerificationCurrent
	default:
		return result, fmt.Errorf("%w: recovery ledger is not exact or advanced-compatible", ErrPlanConflict)
	}
	return result, nil
}

func recoveryStaticProjection(plan Plan, actualLedger []string) (Baseline, error) {
	projection, err := StaticProjectionFromPlan(plan)
	if err != nil {
		return Baseline{}, err
	}
	planIndex := slices.Index(actualLedger, plan.PublicationID)
	if planIndex < 0 {
		return Baseline{}, fmt.Errorf("%w: recovery Plan publication is missing", ErrPlanConflict)
	}
	declarations, err := CompiledDeclarations()
	if err != nil {
		return Baseline{}, err
	}
	for _, declaration := range declarations {
		index := slices.Index(actualLedger, declaration.PublicationID)
		if index <= planIndex {
			continue
		}
		if declaration.ExpectedStaticPredecessor != projection.ThroughMigration {
			return Baseline{}, fmt.Errorf("%w: advanced publication %s does not follow static predecessor %s", ErrPlanConflict,
				declaration.PublicationID, projection.ThroughMigration)
		}
		projection, err = Fold(projection, []Declaration{declaration})
		if err != nil {
			return Baseline{}, err
		}
	}
	return projection, nil
}

func isLedgerPrefix(prefix, full []string) bool {
	return len(prefix) <= len(full) && slices.Equal(prefix, full[:len(prefix)])
}
