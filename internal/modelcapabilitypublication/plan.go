package modelcapabilitypublication

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProjectionCandidate is one exact environment row matched by a declaration
// effect. Existing is nil only when the deployment capability row is absent.
type ProjectionCandidate struct {
	DeploymentID int64
	OperationID  string
	CapabilityID string
	Existing     *DeploymentProjection
}

// TargetState is the complete pre-state used by the pure plan builder.
type TargetState struct {
	Identity             TargetIdentity
	Ledger               []string
	Bindings             []Binding
	Selectors            []Selector
	ProjectionCandidates map[string][]ProjectionCandidate
}

// BuildPlan produces the exact immutable plan for one independent
// publication. predecessor must be the folded projection at the declaration's
// exact expected schema predecessor.
func BuildPlan(authority PlanAuthority, target TargetState) (Plan, error) {
	artifact := authority.Artifact
	artifactLedger := authority.ArtifactMigrationLedger
	predecessor := authority.Predecessor
	declaration := authority.Declaration
	declaration = NormalizeDeclaration(declaration)
	if err := ValidateDeclaration(declaration); err != nil {
		return Plan{}, err
	}
	if predecessor.ThroughMigration != declaration.ExpectedStaticPredecessor {
		return Plan{}, fmt.Errorf("%w: predecessor projection ends at %s, declaration expects static predecessor %s", ErrInvalidDeclaration,
			predecessor.ThroughMigration, declaration.ExpectedStaticPredecessor)
	}
	digest, err := DeclarationDigest(declaration)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		SchemaVersion:           PlanSchemaV1,
		CanonicalizationVersion: CanonicalizationV1,
		PublicationID:           declaration.PublicationID,
		DeclarationDigest:       digest,
		Artifact:                artifact,
		Target:                  target.Identity,
		ArtifactMigrationLedger: append([]string(nil), artifactLedger...),
		ArtifactMigrationFiles:  append([]MigrationFileIdentity(nil), authority.ArtifactMigrationFiles...),
		PublicationDownSHA256:   authority.PublicationDownSHA256,
		PublicationVerifySHA256: authority.PublicationVerifySHA256,
		ExpectedLedger:          append([]string(nil), target.Ledger...),
		Validation:              ValidationValid,
	}
	publicationIndex := slices.Index(artifactLedger, declaration.PublicationID)
	if publicationIndex <= 0 || artifactLedger[publicationIndex-1] != declaration.ExpectedSchemaPredecessor {
		plan.addConflict("artifact_declaration_mismatch", declaration.PublicationID)
	}
	if len(target.Ledger) > len(artifactLedger) || !slices.Equal(target.Ledger, artifactLedger[:len(target.Ledger)]) {
		plan.addConflict("schema_ledger_mismatch", declaration.ExpectedSchemaPredecessor)
	} else {
		plan.PendingMigrations = append([]string(nil), artifactLedger[len(target.Ledger):]...)
		if publicationIndex >= len(target.Ledger) {
			plan.ApplyMigrations = append([]string(nil), artifactLedger[len(target.Ledger):publicationIndex+1]...)
			plan.ProposedLedger = append(append([]string(nil), target.Ledger...), plan.ApplyMigrations...)
		}
	}
	if publicationIndex < len(target.Ledger) || len(plan.ApplyMigrations) == 0 {
		plan.addConflict("schema_ledger_mismatch", declaration.ExpectedSchemaPredecessor)
	}
	if !slices.Contains(target.Ledger, declaration.ExpectedStaticPredecessor) {
		plan.addConflict("schema_ledger_mismatch", declaration.ExpectedStaticPredecessor)
	}
	plan.ProposedLedger = append(append([]string(nil), plan.ExpectedLedger...), plan.ApplyMigrations...)
	plan.planBindings(predecessor, declaration, target.Bindings)
	plan.planSelectors(predecessor, declaration, target.Selectors)
	plan.planProjections(declaration, target.ProjectionCandidates)
	if len(plan.Conflicts) != 0 {
		plan.Validation = ValidationConflict
	}
	return FinalizePlan(plan)
}

func (plan *Plan) planBindings(predecessor Baseline, declaration Declaration, actual []Binding) {
	actualByKey := make(map[string]Binding, len(actual))
	for _, binding := range actual {
		actualByKey[bindingKey(binding)] = binding
	}
	expectedKeys := make(map[string]struct{}, len(predecessor.Bindings)+len(declaration.BindingAppends))
	for _, binding := range predecessor.Bindings {
		key := bindingKey(binding)
		expectedKeys[key] = struct{}{}
		actualBinding, exists := actualByKey[key]
		if !exists || actualBinding != binding {
			plan.addConflict("binding_content_mismatch", key)
			continue
		}
		expected := actualBinding
		plan.BindingItems = append(plan.BindingItems, BindingPlanItem{Action: BindingActionNoOp, Expected: &expected, Final: binding})
	}
	for _, binding := range declaration.BindingAppends {
		key := bindingKey(binding)
		expectedKeys[key] = struct{}{}
		if actualBinding, exists := actualByKey[key]; exists {
			code := "binding_content_mismatch"
			if actualBinding == binding {
				code = "unledgered_publication_state"
			}
			plan.addConflict(code, key)
			continue
		}
		plan.BindingItems = append(plan.BindingItems, BindingPlanItem{Action: BindingActionAppend, Final: binding})
	}
	for _, binding := range actual {
		key := bindingKey(binding)
		if _, expected := expectedKeys[key]; !expected {
			plan.addConflict("binding_content_mismatch", key)
		}
	}
}

func (plan *Plan) planSelectors(predecessor Baseline, declaration Declaration, actual []Selector) {
	actualByKey := make(map[string]Selector, len(actual))
	for _, selector := range actual {
		actualByKey[selectorKey(selector)] = selector
	}
	transitions := make(map[string]SelectorTransition, len(declaration.SelectorTransitions))
	for _, transition := range declaration.SelectorTransitions {
		transitions[selectorTransitionKey(transition)] = transition
	}
	expectedKeys := make(map[string]struct{}, len(predecessor.Selectors)+len(transitions))
	for _, selector := range predecessor.Selectors {
		key := selectorKey(selector)
		expectedKeys[key] = struct{}{}
		if _, changes := transitions[key]; changes {
			continue
		}
		actualSelector, exists := actualByKey[key]
		if !exists || actualSelector != selector {
			plan.addConflict("selector_predecessor_mismatch", key)
			continue
		}
		expected := actualSelector
		plan.SelectorItems = append(plan.SelectorItems, SelectorPlanItem{Action: SelectorActionNoOp, Expected: &expected, Final: selector})
	}
	for key, transition := range transitions {
		expectedKeys[key] = struct{}{}
		actualSelector, exists := actualByKey[key]
		final := selectorFromTransition(transition)
		if transition.ExpectedPredecessor == nil {
			if exists {
				code := "selector_predecessor_mismatch"
				if actualSelector == final {
					code = "unledgered_publication_state"
				}
				plan.addConflict(code, key)
				continue
			}
			plan.SelectorItems = append(plan.SelectorItems, SelectorPlanItem{Action: SelectorActionPromotion, Final: final})
			continue
		}
		if !exists {
			plan.addConflict("selector_predecessor_mismatch", key)
			continue
		}
		if actualSelector == final {
			plan.addConflict("unledgered_publication_state", key)
			continue
		}
		if actualSelector.ProbeContractID != transition.ExpectedPredecessor.ProbeContractID ||
			actualSelector.ProbeContractVersion != transition.ExpectedPredecessor.ProbeContractVersion {
			plan.addConflict("selector_predecessor_mismatch", key)
			continue
		}
		expected := actualSelector
		plan.SelectorItems = append(plan.SelectorItems, SelectorPlanItem{Action: SelectorActionPromotion, Expected: &expected, Final: final})
	}
	for _, selector := range actual {
		key := selectorKey(selector)
		if _, expected := expectedKeys[key]; !expected {
			plan.addConflict("undeclared_selector_promotion", key)
		}
	}
}

func (plan *Plan) planProjections(declaration Declaration, candidates map[string][]ProjectionCandidate) {
	seen := make(map[string]struct{})
	for _, effect := range declaration.DeclaredProjectionEffects {
		for _, candidate := range candidates[projectionEffectKey(effect)] {
			key := fmt.Sprintf("%020d\x00%s\x00%s", candidate.DeploymentID, candidate.OperationID, candidate.CapabilityID)
			if _, duplicate := seen[key]; duplicate {
				plan.addConflict("projection_pre_state_mismatch", key)
				continue
			}
			seen[key] = struct{}{}
			finalRef := effect.FinalContractRef
			if candidate.Existing == nil {
				if !effect.AllowInsert {
					plan.addConflict("projection_pre_state_mismatch", key)
					continue
				}
				final := DeploymentProjection{DeploymentID: candidate.DeploymentID, OperationID: candidate.OperationID,
					CapabilityID: candidate.CapabilityID, SupportState: "enabled", EvidenceState: "declared",
					EvidenceRef: finalRef, Revision: 1, SupportRevision: 1}
				plan.ProjectionItems = append(plan.ProjectionItems, ProjectionPlanItem{
					Action: ProjectionActionInsert, Final: final, ExpectedAffectedRows: 1,
				})
				continue
			}
			existing := *candidate.Existing
			if existing.EvidenceRef == finalRef && existing.SupportState == "enabled" && existing.EvidenceState == "declared" {
				plan.addConflict("unledgered_publication_state", key)
				continue
			}
			if existing.SupportState != "enabled" || existing.EvidenceState != "declared" ||
				existing.EvidenceRef != effect.ExpectedContractRef {
				plan.addConflict("projection_pre_state_mismatch", key)
				continue
			}
			final := existing
			final.EvidenceRef = finalRef
			final.Revision++
			expected := existing
			plan.ProjectionItems = append(plan.ProjectionItems, ProjectionPlanItem{
				Action: ProjectionActionUpdate, Expected: &expected, Final: final, ExpectedAffectedRows: 1,
			})
		}
	}
}

func (plan *Plan) addConflict(code, target string) {
	plan.Conflicts = append(plan.Conflicts, Conflict{Code: code, Target: target})
}

// PlanTarget runs the read-only Target characterization transaction. The
// result is local evidence, never a database reservation.
func PlanTarget(ctx context.Context, pool *pgxpool.Pool, environment string, authority PlanAuthority,
) (Plan, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Plan{}, fmt.Errorf("begin publication dry-run: %w", err)
	}
	defer tx.Rollback(ctx) // read-only; rollback also proves no commit path is needed.
	target, err := ReadTargetState(ctx, tx, environment, authority.Declaration)
	if err != nil {
		return Plan{}, err
	}
	return BuildPlan(authority, target)
}

// ReadTargetState reads the exact environment projection under the caller's
// snapshot or publication locks.
func ReadTargetState(ctx context.Context, q interface {
	Querier
	QueryRow(context.Context, string, ...any) pgx.Row
}, environment string, declaration Declaration) (TargetState, error) {
	if strings.TrimSpace(environment) == "" || environment == "unspecified" {
		return TargetState{}, fmt.Errorf("%w: trusted environment identity is required", ErrInvalidDeclaration)
	}
	identity, err := readTargetIdentity(ctx, q, environment)
	if err != nil {
		return TargetState{}, err
	}
	state := TargetState{Identity: identity, ProjectionCandidates: make(map[string][]ProjectionCandidate)}
	rows, err := q.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return TargetState{}, fmt.Errorf("read publication ledger: %w", err)
	}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return TargetState{}, fmt.Errorf("scan publication ledger: %w", err)
		}
		state.Ledger = append(state.Ledger, version)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return TargetState{}, fmt.Errorf("iterate publication ledger: %w", err)
	}
	rows.Close()
	state.Bindings, err = exportBindings(ctx, q)
	if err != nil {
		return TargetState{}, err
	}
	state.Selectors, err = exportSelectors(ctx, q)
	if err != nil {
		return TargetState{}, err
	}
	for _, effect := range declaration.DeclaredProjectionEffects {
		candidates, err := readProjectionCandidates(ctx, q, effect)
		if err != nil {
			return TargetState{}, err
		}
		state.ProjectionCandidates[projectionEffectKey(effect)] = candidates
	}
	return state, nil
}

func readTargetIdentity(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, environment string) (TargetIdentity, error) {
	environment = strings.TrimSpace(environment)
	if environment == "" || environment == "unspecified" {
		return TargetIdentity{}, fmt.Errorf("%w: trusted environment identity is required", ErrInvalidDeclaration)
	}
	identity := TargetIdentity{Environment: environment}
	if err := q.QueryRow(ctx, `SELECT current_database(), system_identifier::text FROM pg_control_system()`).Scan(
		&identity.DatabaseName, &identity.SystemIdentifier,
	); err != nil {
		return TargetIdentity{}, fmt.Errorf("read server-owned Target identity: %w", err)
	}
	return identity, nil
}

func readProjectionCandidates(ctx context.Context, q Querier, effect ProjectionEffect) ([]ProjectionCandidate, error) {
	rows, err := q.Query(ctx, `
SELECT deployment.id, operation.operation_id, $2::text,
       capability.deployment_id, capability.support_state, capability.evidence_state,
       capability.evidence_ref, capability.verified_at,
       capability.revision, capability.support_revision
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_registry model ON model.id=deployment.public_model_id
  JOIN model_deployment_operations operation ON operation.deployment_id=deployment.id
  LEFT JOIN model_deployment_capabilities capability
    ON capability.deployment_id=deployment.id
   AND capability.operation_id=operation.operation_id
   AND capability.capability_id=$2
 WHERE operation.operation_id=$1
   AND model.execution_profile_id=$3
   AND model.execution_profile_version=$4
   AND operation.protocol_profile_id=$5
   AND operation.protocol_profile_version=$6
   AND connection.provider=$7
   AND deployment.upstream_model=$8
   AND (
       capability.deployment_id IS NULL
       OR (capability.support_state='enabled' AND capability.evidence_state='declared')
   )
 ORDER BY deployment.id`, effect.OperationID, effect.CapabilityID,
		effect.ExecutionProfileID, effect.ExecutionProfileVersion,
		effect.ProtocolProfileID, effect.ProtocolProfileVersion,
		effect.ExpectedProvider, effect.ExpectedUpstreamModel)
	if err != nil {
		return nil, fmt.Errorf("query projection candidates %s: %w", projectionEffectKey(effect), err)
	}
	defer rows.Close()
	var candidates []ProjectionCandidate
	for rows.Next() {
		var candidate ProjectionCandidate
		var deploymentID *int64
		var supportState, evidenceState, evidenceRef *string
		var verifiedAt *time.Time
		var revision, supportRevision *int64
		if err := rows.Scan(&candidate.DeploymentID, &candidate.OperationID, &candidate.CapabilityID,
			&deploymentID, &supportState, &evidenceState, &evidenceRef, &verifiedAt, &revision, &supportRevision); err != nil {
			return nil, fmt.Errorf("scan projection candidate: %w", err)
		}
		if deploymentID != nil {
			candidate.Existing = &DeploymentProjection{DeploymentID: *deploymentID, OperationID: candidate.OperationID,
				CapabilityID: candidate.CapabilityID, SupportState: *supportState, EvidenceState: *evidenceState,
				EvidenceRef: *evidenceRef, VerifiedAt: verifiedAt, Revision: *revision, SupportRevision: *supportRevision}
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projection candidates: %w", err)
	}
	slices.SortFunc(candidates, func(a, b ProjectionCandidate) int {
		if a.DeploymentID < b.DeploymentID {
			return -1
		}
		if a.DeploymentID > b.DeploymentID {
			return 1
		}
		return strings.Compare(a.CapabilityID, b.CapabilityID)
	})
	return candidates, nil
}
