package modelcapabilitypublication

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type rowQuerier interface {
	Querier
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ApplyProjectionPlan executes only the exact environment rows frozen in a
// validated Plan. Declaration filters never run in the write path.
func ApplyProjectionPlan(ctx context.Context, tx pgx.Tx, plan Plan) error {
	if err := ValidatePlan(plan); err != nil {
		return err
	}
	if plan.Validation != ValidationValid {
		return ErrPlanConflict
	}
	if len(plan.ProjectionItems) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `DROP TRIGGER model_deployment_capabilities_receipt_projection_guard ON model_deployment_capabilities`); err != nil {
		return fmt.Errorf("open deployment projection guard: %w", err)
	}
	for _, item := range plan.ProjectionItems {
		var affected int64
		switch item.Action {
		case ProjectionActionNoOp:
			if err := verifyProjectionRow(ctx, tx, item.Final); err != nil {
				return err
			}
			continue
		case ProjectionActionInsert:
			result, err := tx.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
    deployment_id,operation_id,capability_id,support_state,evidence_state,
    evidence_ref,verified_at,revision,support_revision
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
				item.Final.DeploymentID, item.Final.OperationID, item.Final.CapabilityID,
				item.Final.SupportState, item.Final.EvidenceState, item.Final.EvidenceRef,
				item.Final.VerifiedAt, item.Final.Revision, item.Final.SupportRevision)
			if err != nil {
				return fmt.Errorf("insert exact deployment projection %s: %w", deploymentProjectionKey(item.Final), err)
			}
			affected = result.RowsAffected()
		case ProjectionActionUpdate:
			if item.Expected == nil {
				return fmt.Errorf("%w: projection update lacks expected row", ErrInvalidDeclaration)
			}
			expected := *item.Expected
			result, err := tx.Exec(ctx, `
UPDATE model_deployment_capabilities
   SET support_state=$4,evidence_state=$5,evidence_ref=$6,verified_at=$7,
       revision=$8,support_revision=$9
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3
   AND support_state=$10 AND evidence_state=$11 AND evidence_ref=$12
   AND verified_at IS NOT DISTINCT FROM $13
   AND revision=$14 AND support_revision=$15`,
				item.Final.DeploymentID, item.Final.OperationID, item.Final.CapabilityID,
				item.Final.SupportState, item.Final.EvidenceState, item.Final.EvidenceRef,
				item.Final.VerifiedAt, item.Final.Revision, item.Final.SupportRevision,
				expected.SupportState, expected.EvidenceState, expected.EvidenceRef,
				expected.VerifiedAt, expected.Revision, expected.SupportRevision)
			if err != nil {
				return fmt.Errorf("update exact deployment projection %s: %w", deploymentProjectionKey(item.Final), err)
			}
			affected = result.RowsAffected()
		default:
			return fmt.Errorf("%w: unsupported projection action %q", ErrInvalidDeclaration, item.Action)
		}
		if affected != item.ExpectedAffectedRows {
			return fmt.Errorf("%w: projection %s affected %d rows, expected %d", ErrPlanConflict,
				deploymentProjectionKey(item.Final), affected, item.ExpectedAffectedRows)
		}
		if err := verifyProjectionRow(ctx, tx, item.Final); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
CREATE TRIGGER model_deployment_capabilities_receipt_projection_guard
BEFORE INSERT OR UPDATE ON model_deployment_capabilities
FOR EACH ROW EXECUTE FUNCTION enforce_model_capability_evidence_projection()`); err != nil {
		return fmt.Errorf("reseal deployment projection guard: %w", err)
	}
	return nil
}

// RevertProjectionPlan restores only the exact rows frozen by the original
// Plan. It is used only by the guarded down recovery path.
func RevertProjectionPlan(ctx context.Context, tx pgx.Tx, plan Plan) error {
	if err := ValidatePlan(plan); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE model_deployment_capabilities, model_deployment_operations,
model_deployments, provider_connections, model_registry, model_capability_probes,
model_capability_routing_approvals, freeze_idempotency, billing_provider_attempts, schema_migrations
IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("lock guarded publication recovery tables: %w", err)
	}
	if len(plan.ProjectionItems) == 0 {
		return nil
	}
	for _, trigger := range []string{
		"a_model_deployment_capabilities_support_revision",
		"model_deployment_capabilities_receipt_projection_guard",
		"model_deployment_capabilities_revision_guard",
	} {
		if _, err := tx.Exec(ctx, `DROP TRIGGER `+trigger+` ON model_deployment_capabilities`); err != nil {
			return fmt.Errorf("open deployment projection recovery guard %s: %w", trigger, err)
		}
	}
	for index := len(plan.ProjectionItems) - 1; index >= 0; index-- {
		item := plan.ProjectionItems[index]
		var affected int64
		switch item.Action {
		case ProjectionActionNoOp:
			continue
		case ProjectionActionInsert:
			result, err := deleteExactProjection(ctx, tx, item.Final)
			if err != nil {
				return err
			}
			affected = result.RowsAffected()
		case ProjectionActionUpdate:
			if item.Expected == nil {
				return fmt.Errorf("%w: projection rollback lacks predecessor", ErrInvalidDeclaration)
			}
			result, err := updateExactProjection(ctx, tx, item.Final, *item.Expected)
			if err != nil {
				return err
			}
			affected = result.RowsAffected()
		default:
			return fmt.Errorf("%w: unsupported rollback action %q", ErrInvalidDeclaration, item.Action)
		}
		if affected != item.ExpectedAffectedRows {
			return fmt.Errorf("%w: projection rollback %s affected %d rows, expected %d", ErrPlanConflict,
				deploymentProjectionKey(item.Final), affected, item.ExpectedAffectedRows)
		}
	}
	for _, statement := range []string{
		`CREATE TRIGGER a_model_deployment_capabilities_support_revision BEFORE UPDATE ON model_deployment_capabilities FOR EACH ROW EXECUTE FUNCTION enforce_model_deployment_capability_support_revision()`,
		`CREATE TRIGGER model_deployment_capabilities_receipt_projection_guard BEFORE INSERT OR UPDATE ON model_deployment_capabilities FOR EACH ROW EXECUTE FUNCTION enforce_model_capability_evidence_projection()`,
		`CREATE TRIGGER model_deployment_capabilities_revision_guard BEFORE UPDATE ON model_deployment_capabilities FOR EACH ROW EXECUTE FUNCTION enforce_model_deployment_capability_revision()`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("reseal deployment projection recovery guard: %w", err)
		}
	}
	return verifyProjectionRollback(ctx, tx, plan)
}

// VerifyPlanFinal verifies the complete static projection represented by the
// Plan plus every exact dynamic row it explicitly mutated.
func VerifyPlanFinal(ctx context.Context, q rowQuerier, plan Plan) error {
	if err := ValidatePlan(plan); err != nil {
		return err
	}
	static, err := StaticProjectionFromPlan(plan)
	if err != nil {
		return err
	}
	if err := VerifyStaticProjection(ctx, q, static); err != nil {
		return err
	}
	for _, item := range plan.ProjectionItems {
		if err := verifyProjectionRow(ctx, q, item.Final); err != nil {
			return err
		}
	}
	return nil
}

// VerifyProjectionCoverage proves that the declaration filter still expands
// to exactly the Plan rows. It is a verifier only; the filter never writes.
func VerifyProjectionCoverage(ctx context.Context, q rowQuerier, plan Plan, declaration Declaration) error {
	want := make(map[string]DeploymentProjection, len(plan.ProjectionItems))
	for _, item := range plan.ProjectionItems {
		want[deploymentProjectionKey(item.Final)] = item.Final
	}
	seen := make(map[string]struct{}, len(want))
	for _, effect := range declaration.DeclaredProjectionEffects {
		candidates, err := readProjectionCandidates(ctx, q, effect)
		if err != nil {
			return err
		}
		for _, candidate := range candidates {
			key := fmt.Sprintf("%020d\x00%s\x00%s", candidate.DeploymentID, candidate.OperationID, candidate.CapabilityID)
			final, exists := want[key]
			if !exists || candidate.Existing == nil || *candidate.Existing != final {
				return fmt.Errorf("%w: projection coverage drift at %s", ErrPlanConflict, key)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%w: duplicate projection coverage at %s", ErrPlanConflict, key)
			}
			seen[key] = struct{}{}
		}
	}
	if len(seen) != len(want) {
		return fmt.Errorf("%w: projection coverage count mismatch: got %d want %d", ErrPlanConflict, len(seen), len(want))
	}
	return nil
}

func VerifyStaticProjection(ctx context.Context, q Querier, expected Baseline) error {
	expected = normalizeBaseline(expected)
	bindings, err := exportBindings(ctx, q)
	if err != nil {
		return err
	}
	selectors, err := exportSelectors(ctx, q)
	if err != nil {
		return err
	}
	actual := normalizeBaseline(Baseline{SchemaVersion: BaselineSchemaV1, ThroughMigration: expected.ThroughMigration,
		Bindings: bindings, Selectors: selectors})
	if !slices.Equal(actual.Bindings, expected.Bindings) || !slices.Equal(actual.Selectors, expected.Selectors) {
		return fmt.Errorf("%w: static capability-contract projection mismatch", ErrPlanConflict)
	}
	return nil
}

func StaticProjectionFromPlan(plan Plan) (Baseline, error) {
	bindings := make([]Binding, 0, len(plan.BindingItems))
	for _, item := range plan.BindingItems {
		bindings = append(bindings, item.Final)
	}
	selectors := make([]Selector, 0, len(plan.SelectorItems))
	for _, item := range plan.SelectorItems {
		selectors = append(selectors, item.Final)
	}
	static := normalizeBaseline(Baseline{SchemaVersion: BaselineSchemaV1, ThroughMigration: plan.PublicationID,
		Bindings: bindings, Selectors: selectors})
	if len(static.Bindings) == 0 || len(static.Selectors) == 0 {
		return Baseline{}, fmt.Errorf("%w: Plan lacks complete static projection", ErrInvalidDeclaration)
	}
	return static, nil
}

func verifyProjectionRow(ctx context.Context, q rowQuerier, want DeploymentProjection) error {
	var got DeploymentProjection
	if err := q.QueryRow(ctx, `
SELECT deployment_id,operation_id,capability_id,support_state,evidence_state,
       evidence_ref,verified_at,revision,support_revision
  FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3`,
		want.DeploymentID, want.OperationID, want.CapabilityID).Scan(
		&got.DeploymentID, &got.OperationID, &got.CapabilityID, &got.SupportState,
		&got.EvidenceState, &got.EvidenceRef, &got.VerifiedAt, &got.Revision, &got.SupportRevision,
	); err != nil {
		return fmt.Errorf("verify deployment projection %s: %w", deploymentProjectionKey(want), err)
	}
	if got != want {
		return fmt.Errorf("%w: deployment projection mismatch at %s", ErrPlanConflict, deploymentProjectionKey(want))
	}
	return nil
}

func verifyProjectionRollback(ctx context.Context, q rowQuerier, plan Plan) error {
	for _, item := range plan.ProjectionItems {
		switch item.Action {
		case ProjectionActionNoOp:
			if err := verifyProjectionRow(ctx, q, item.Final); err != nil {
				return err
			}
		case ProjectionActionInsert:
			var count int64
			if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployment_capabilities
WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3`, item.Final.DeploymentID,
				item.Final.OperationID, item.Final.CapabilityID).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("%w: inserted projection remains after rollback", ErrPlanConflict)
			}
		case ProjectionActionUpdate:
			if item.Expected == nil {
				return fmt.Errorf("%w: projection rollback lacks predecessor", ErrInvalidDeclaration)
			}
			if err := verifyProjectionRow(ctx, q, *item.Expected); err != nil {
				return err
			}
		}
	}
	return nil
}

func deleteExactProjection(ctx context.Context, tx pgx.Tx, row DeploymentProjection) (pgconn.CommandTag, error) {
	return tx.Exec(ctx, `DELETE FROM model_deployment_capabilities
WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3
  AND support_state=$4 AND evidence_state=$5 AND evidence_ref=$6
  AND verified_at IS NOT DISTINCT FROM $7 AND revision=$8 AND support_revision=$9`,
		row.DeploymentID, row.OperationID, row.CapabilityID, row.SupportState, row.EvidenceState,
		row.EvidenceRef, row.VerifiedAt, row.Revision, row.SupportRevision)
}

func updateExactProjection(ctx context.Context, tx pgx.Tx, from, to DeploymentProjection) (pgconn.CommandTag, error) {
	return tx.Exec(ctx, `UPDATE model_deployment_capabilities
SET support_state=$4,evidence_state=$5,evidence_ref=$6,verified_at=$7,revision=$8,support_revision=$9
WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3
  AND support_state=$10 AND evidence_state=$11 AND evidence_ref=$12
  AND verified_at IS NOT DISTINCT FROM $13 AND revision=$14 AND support_revision=$15`,
		to.DeploymentID, to.OperationID, to.CapabilityID, to.SupportState, to.EvidenceState,
		to.EvidenceRef, to.VerifiedAt, to.Revision, to.SupportRevision,
		from.SupportState, from.EvidenceState, from.EvidenceRef, from.VerifiedAt, from.Revision, from.SupportRevision)
}
