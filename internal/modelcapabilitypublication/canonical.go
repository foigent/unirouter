package modelcapabilitypublication

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/strictjson"
)

var (
	migrationIDPattern = regexp.MustCompile(`^[0-9]{6}_[a-z0-9][a-z0-9_]{0,56}$`)
	sha256Pattern      = regexp.MustCompile(`^(?:sha256:)?[0-9a-f]{64}$`)
	gitObjectPattern   = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

func decodeStrict(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return fmt.Errorf("%w: strict JSON: %v", ErrInvalidDeclaration, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrInvalidDeclaration, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: multiple top-level values", ErrInvalidDeclaration)
		}
		return fmt.Errorf("%w: trailing data: %v", ErrInvalidDeclaration, err)
	}
	return nil
}

func DecodeBaseline(data []byte) (Baseline, error) {
	var baseline Baseline
	if err := decodeStrict(data, &baseline); err != nil {
		return Baseline{}, err
	}
	baseline = normalizeBaseline(baseline)
	if err := ValidateBaseline(baseline); err != nil {
		return Baseline{}, err
	}
	return baseline, nil
}

func DecodeDeclaration(data []byte) (Declaration, error) {
	var declaration Declaration
	if err := decodeStrict(data, &declaration); err != nil {
		return Declaration{}, err
	}
	declaration = NormalizeDeclaration(declaration)
	if err := ValidateDeclaration(declaration); err != nil {
		return Declaration{}, err
	}
	return declaration, nil
}

func DecodePlan(data []byte) (Plan, error) {
	var plan Plan
	if err := decodeStrict(data, &plan); err != nil {
		return Plan{}, err
	}
	plan = NormalizePlan(plan)
	if err := ValidatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func CanonicalBaseline(baseline Baseline) ([]byte, error) {
	baseline = normalizeBaseline(baseline)
	if err := ValidateBaseline(baseline); err != nil {
		return nil, err
	}
	return json.Marshal(baseline)
}

func CanonicalDeclaration(declaration Declaration) ([]byte, error) {
	declaration = NormalizeDeclaration(declaration)
	if err := ValidateDeclaration(declaration); err != nil {
		return nil, err
	}
	return json.Marshal(declaration)
}

func DeclarationDigest(declaration Declaration) (string, error) {
	wire, err := CanonicalDeclaration(declaration)
	if err != nil {
		return "", err
	}
	return sha256Hex(wire), nil
}

func CanonicalPlan(plan Plan) ([]byte, error) {
	plan = NormalizePlan(plan)
	plan.PlanDigest = ""
	if err := validatePlanFields(plan, false); err != nil {
		return nil, err
	}
	return json.Marshal(plan)
}

func FinalizePlan(plan Plan) (Plan, error) {
	plan = NormalizePlan(plan)
	plan.PlanDigest = ""
	wire, err := CanonicalPlan(plan)
	if err != nil {
		return Plan{}, err
	}
	plan.PlanDigest = sha256Hex(wire)
	if err := ValidatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func VerifyPlanDigest(plan Plan) error {
	want := strings.TrimSpace(plan.PlanDigest)
	if want == "" {
		return fmt.Errorf("%w: plan digest is required", ErrInvalidDeclaration)
	}
	plan.PlanDigest = ""
	wire, err := CanonicalPlan(plan)
	if err != nil {
		return err
	}
	if got := sha256Hex(wire); got != want {
		return fmt.Errorf("%w: plan digest mismatch: got %s want %s", ErrInvalidDeclaration, got, want)
	}
	return nil
}

func NormalizeDeclaration(declaration Declaration) Declaration {
	declaration.BindingAppends = append([]Binding(nil), declaration.BindingAppends...)
	declaration.SelectorTransitions = append([]SelectorTransition(nil), declaration.SelectorTransitions...)
	declaration.DeclaredProjectionEffects = append([]ProjectionEffect(nil), declaration.DeclaredProjectionEffects...)
	declaration.ProfileAttachmentGuards = append([]ProfileAttachmentGuard(nil), declaration.ProfileAttachmentGuards...)
	slices.SortFunc(declaration.BindingAppends, func(a, b Binding) int { return strings.Compare(bindingKey(a), bindingKey(b)) })
	slices.SortFunc(declaration.SelectorTransitions, func(a, b SelectorTransition) int {
		return strings.Compare(selectorTransitionKey(a), selectorTransitionKey(b))
	})
	slices.SortFunc(declaration.DeclaredProjectionEffects, func(a, b ProjectionEffect) int {
		return strings.Compare(projectionEffectKey(a), projectionEffectKey(b))
	})
	slices.SortFunc(declaration.ProfileAttachmentGuards, func(a, b ProfileAttachmentGuard) int {
		return strings.Compare(profileAttachmentGuardKey(a), profileAttachmentGuardKey(b))
	})
	return declaration
}

func normalizeBaseline(baseline Baseline) Baseline {
	baseline.Bindings = append([]Binding(nil), baseline.Bindings...)
	baseline.Selectors = append([]Selector(nil), baseline.Selectors...)
	slices.SortFunc(baseline.Bindings, func(a, b Binding) int { return strings.Compare(bindingKey(a), bindingKey(b)) })
	slices.SortFunc(baseline.Selectors, func(a, b Selector) int { return strings.Compare(selectorKey(a), selectorKey(b)) })
	return baseline
}

func NormalizePlan(plan Plan) Plan {
	plan.ArtifactMigrationLedger = append([]string(nil), plan.ArtifactMigrationLedger...)
	plan.ArtifactMigrationFiles = append([]MigrationFileIdentity(nil), plan.ArtifactMigrationFiles...)
	plan.ExpectedLedger = append([]string(nil), plan.ExpectedLedger...)
	plan.PendingMigrations = append([]string(nil), plan.PendingMigrations...)
	plan.ApplyMigrations = append([]string(nil), plan.ApplyMigrations...)
	plan.ProposedLedger = append([]string(nil), plan.ProposedLedger...)
	plan.BindingItems = append([]BindingPlanItem(nil), plan.BindingItems...)
	plan.SelectorItems = append([]SelectorPlanItem(nil), plan.SelectorItems...)
	plan.ProjectionItems = append([]ProjectionPlanItem(nil), plan.ProjectionItems...)
	plan.Conflicts = append([]Conflict(nil), plan.Conflicts...)
	slices.SortFunc(plan.BindingItems, func(a, b BindingPlanItem) int { return strings.Compare(bindingKey(a.Final), bindingKey(b.Final)) })
	slices.SortFunc(plan.SelectorItems, func(a, b SelectorPlanItem) int { return strings.Compare(selectorKey(a.Final), selectorKey(b.Final)) })
	slices.SortFunc(plan.ProjectionItems, func(a, b ProjectionPlanItem) int {
		return strings.Compare(deploymentProjectionKey(a.Final), deploymentProjectionKey(b.Final))
	})
	slices.SortFunc(plan.Conflicts, func(a, b Conflict) int {
		if result := strings.Compare(a.Code, b.Code); result != 0 {
			return result
		}
		return strings.Compare(a.Target, b.Target)
	})
	return plan
}

func ValidateBaseline(baseline Baseline) error {
	if baseline.SchemaVersion != BaselineSchemaV1 || baseline.ThroughMigration != BaselineMigrationM100 {
		return fmt.Errorf("%w: unsupported baseline identity", ErrInvalidDeclaration)
	}
	return validateStaticProjectionRows(baseline)
}

func validateFoldStart(baseline Baseline) error {
	if baseline.SchemaVersion != BaselineSchemaV1 {
		return fmt.Errorf("%w: unsupported static projection schema", ErrInvalidDeclaration)
	}
	if err := validateMigrationID("static projection predecessor", baseline.ThroughMigration); err != nil {
		return err
	}
	return validateStaticProjectionRows(baseline)
}

func validateStaticProjectionRows(baseline Baseline) error {
	if len(baseline.Bindings) == 0 || len(baseline.Selectors) == 0 {
		return fmt.Errorf("%w: baseline rows are required", ErrInvalidDeclaration)
	}
	seenBindings := make(map[string]struct{}, len(baseline.Bindings))
	for _, binding := range baseline.Bindings {
		if err := validateBinding(binding); err != nil {
			return err
		}
		key := bindingKey(binding)
		if _, exists := seenBindings[key]; exists {
			return fmt.Errorf("%w: duplicate binding %s", ErrInvalidDeclaration, key)
		}
		seenBindings[key] = struct{}{}
	}
	seenSelectors := make(map[string]struct{}, len(baseline.Selectors))
	for _, selector := range baseline.Selectors {
		if err := validateSelector(selector); err != nil {
			return err
		}
		key := selectorKey(selector)
		if _, exists := seenSelectors[key]; exists {
			return fmt.Errorf("%w: duplicate selector %s", ErrInvalidDeclaration, key)
		}
		seenSelectors[key] = struct{}{}
		if _, exists := seenBindings[bindingKeyFromSelector(selector)]; !exists {
			return fmt.Errorf("%w: selector target is not a binding: %s", ErrInvalidDeclaration, key)
		}
	}
	return nil
}

func ValidateDeclaration(declaration Declaration) error {
	if declaration.SchemaVersion != DeclarationSchemaV1 {
		return fmt.Errorf("%w: unsupported declaration schema", ErrInvalidDeclaration)
	}
	if err := validateMigrationID("publication_id", declaration.PublicationID); err != nil {
		return err
	}
	if err := validateMigrationID("expected_static_predecessor", declaration.ExpectedStaticPredecessor); err != nil {
		return err
	}
	if err := validateMigrationID("expected_schema_predecessor", declaration.ExpectedSchemaPredecessor); err != nil {
		return err
	}
	if declaration.PublicationID <= declaration.ExpectedSchemaPredecessor || declaration.PublicationID <= declaration.ExpectedStaticPredecessor {
		return fmt.Errorf("%w: publication must follow its exact schema predecessor", ErrInvalidDeclaration)
	}
	seenBindings := make(map[string]struct{}, len(declaration.BindingAppends))
	for _, binding := range declaration.BindingAppends {
		if binding.IntroducedMigration != declaration.PublicationID {
			return fmt.Errorf("%w: binding introduced_migration must equal publication_id", ErrInvalidDeclaration)
		}
		if err := validateBinding(binding); err != nil {
			return err
		}
		key := bindingKey(binding)
		if _, exists := seenBindings[key]; exists {
			return fmt.Errorf("%w: duplicate binding append %s", ErrInvalidDeclaration, key)
		}
		seenBindings[key] = struct{}{}
	}
	seenSelectors := make(map[string]struct{}, len(declaration.SelectorTransitions))
	for _, transition := range declaration.SelectorTransitions {
		if err := validateSelectorTransition(transition); err != nil {
			return err
		}
		key := selectorTransitionKey(transition)
		if _, exists := seenSelectors[key]; exists {
			return fmt.Errorf("%w: duplicate selector transition %s", ErrInvalidDeclaration, key)
		}
		seenSelectors[key] = struct{}{}
	}
	seenEffects := make(map[string]struct{}, len(declaration.DeclaredProjectionEffects))
	for _, effect := range declaration.DeclaredProjectionEffects {
		if err := validateProjectionEffect(effect); err != nil {
			return err
		}
		key := projectionEffectKey(effect)
		if _, exists := seenEffects[key]; exists {
			return fmt.Errorf("%w: duplicate projection effect %s", ErrInvalidDeclaration, key)
		}
		seenEffects[key] = struct{}{}
	}
	seenAttachmentGuards := make(map[string]struct{}, len(declaration.ProfileAttachmentGuards))
	for _, guard := range declaration.ProfileAttachmentGuards {
		if err := validateProfileAttachmentGuard(guard); err != nil {
			return err
		}
		key := profileAttachmentGuardKey(guard)
		if _, exists := seenAttachmentGuards[key]; exists {
			return fmt.Errorf("%w: duplicate Profile attachment guard %s", ErrInvalidDeclaration, key)
		}
		seenAttachmentGuards[key] = struct{}{}
	}
	return nil
}

func ValidatePlan(plan Plan) error {
	if err := validatePlanFields(plan, true); err != nil {
		return err
	}
	return VerifyPlanDigest(plan)
}

func validatePlanFields(plan Plan, requireDigest bool) error {
	if plan.SchemaVersion != PlanSchemaV1 || plan.CanonicalizationVersion != CanonicalizationV1 {
		return fmt.Errorf("%w: unsupported plan identity", ErrInvalidDeclaration)
	}
	for name, value := range map[string]string{
		"declaration_digest":                   plan.DeclarationDigest,
		"artifact.commit":                      plan.Artifact.Commit,
		"artifact.tree":                        plan.Artifact.Tree,
		"artifact.version":                     plan.Artifact.Version,
		"artifact.publisher_executable_digest": plan.Artifact.PublisherExecutableDigest,
		"target.environment":                   plan.Target.Environment,
		"target.database_name":                 plan.Target.DatabaseName,
		"target.system_identifier":             plan.Target.SystemIdentifier,
	} {
		if err := validateText(name, value); err != nil {
			return err
		}
	}
	if err := validateMigrationID("publication_id", plan.PublicationID); err != nil {
		return err
	}
	if plan.Artifact.Kind != ArtifactKindSource && plan.Artifact.Kind != ArtifactKindImage {
		return fmt.Errorf("%w: invalid artifact kind", ErrInvalidDeclaration)
	}
	if !gitObjectPattern.MatchString(plan.Artifact.Commit) || !gitObjectPattern.MatchString(plan.Artifact.Tree) {
		return fmt.Errorf("%w: artifact commit and tree must be complete lowercase Git object identities", ErrInvalidDeclaration)
	}
	if !sha256Pattern.MatchString(plan.Artifact.PublisherExecutableDigest) {
		return fmt.Errorf("%w: invalid publisher executable digest", ErrInvalidDeclaration)
	}
	if plan.Artifact.Kind == ArtifactKindImage && !sha256Pattern.MatchString(plan.Artifact.ImageDigest) {
		return fmt.Errorf("%w: image artifact requires exact image digest", ErrInvalidDeclaration)
	}
	if plan.Artifact.Kind == ArtifactKindSource && plan.Artifact.ImageDigest != "" {
		return fmt.Errorf("%w: source artifact cannot carry image digest", ErrInvalidDeclaration)
	}
	for _, ledger := range [][]string{plan.ArtifactMigrationLedger, plan.ExpectedLedger, plan.PendingMigrations, plan.ApplyMigrations, plan.ProposedLedger} {
		for _, migrationID := range ledger {
			if err := validateMigrationID("migration ledger entry", migrationID); err != nil {
				return err
			}
		}
	}
	if len(plan.ArtifactMigrationFiles) != len(plan.ArtifactMigrationLedger) {
		return fmt.Errorf("%w: artifact migration digest inventory length mismatch", ErrInvalidDeclaration)
	}
	for index, file := range plan.ArtifactMigrationFiles {
		if file.Version != plan.ArtifactMigrationLedger[index] || !sha256Pattern.MatchString(file.UpSHA256) {
			return fmt.Errorf("%w: invalid artifact migration digest at index %d", ErrInvalidDeclaration, index)
		}
	}
	if !sha256Pattern.MatchString(plan.PublicationDownSHA256) || !sha256Pattern.MatchString(plan.PublicationVerifySHA256) {
		return fmt.Errorf("%w: publication down and verify digests are required", ErrInvalidDeclaration)
	}
	if len(plan.ExpectedLedger) == 0 || len(plan.ArtifactMigrationLedger) < len(plan.ExpectedLedger) ||
		!slices.Equal(plan.ExpectedLedger, plan.ArtifactMigrationLedger[:len(plan.ExpectedLedger)]) {
		return fmt.Errorf("%w: target ledger is not an exact artifact prefix", ErrInvalidDeclaration)
	}
	wantPending := plan.ArtifactMigrationLedger[len(plan.ExpectedLedger):]
	if !slices.Equal(plan.PendingMigrations, wantPending) {
		return fmt.Errorf("%w: pending migration inventory mismatch", ErrInvalidDeclaration)
	}
	if plan.Validation == ValidationValid {
		if len(plan.ApplyMigrations) == 0 || len(plan.ApplyMigrations) > len(plan.PendingMigrations) ||
			!slices.Equal(plan.ApplyMigrations, plan.PendingMigrations[:len(plan.ApplyMigrations)]) ||
			plan.ApplyMigrations[len(plan.ApplyMigrations)-1] != plan.PublicationID {
			return fmt.Errorf("%w: apply migrations are not the exact pending prefix through publication", ErrInvalidDeclaration)
		}
	} else if len(plan.ApplyMigrations) > len(plan.PendingMigrations) ||
		!slices.Equal(plan.ApplyMigrations, plan.PendingMigrations[:len(plan.ApplyMigrations)]) {
		return fmt.Errorf("%w: conflicting plan has invalid apply prefix", ErrInvalidDeclaration)
	}
	wantProposed := append(append([]string(nil), plan.ExpectedLedger...), plan.ApplyMigrations...)
	if !slices.Equal(plan.ProposedLedger, wantProposed) {
		return fmt.Errorf("%w: proposed ledger does not equal expected plus exact apply set", ErrInvalidDeclaration)
	}
	if plan.Validation != ValidationValid && plan.Validation != ValidationConflict {
		return fmt.Errorf("%w: invalid plan validation", ErrInvalidDeclaration)
	}
	if plan.Validation == ValidationValid && len(plan.Conflicts) != 0 {
		return fmt.Errorf("%w: valid plan contains conflicts", ErrInvalidDeclaration)
	}
	if plan.Validation == ValidationConflict && len(plan.Conflicts) == 0 {
		return fmt.Errorf("%w: conflicting plan lacks conflicts", ErrInvalidDeclaration)
	}
	for _, item := range plan.BindingItems {
		if item.Action != BindingActionAppend && item.Action != BindingActionNoOp {
			return fmt.Errorf("%w: invalid binding action %q", ErrInvalidDeclaration, item.Action)
		}
		if err := validateBinding(item.Final); err != nil {
			return err
		}
		if item.Action == BindingActionAppend && item.Expected != nil {
			return fmt.Errorf("%w: append binding must expect absence", ErrInvalidDeclaration)
		}
		if item.Action == BindingActionNoOp && (item.Expected == nil || *item.Expected != item.Final) {
			return fmt.Errorf("%w: no-op binding must preserve one exact row", ErrInvalidDeclaration)
		}
	}
	for _, item := range plan.SelectorItems {
		if item.Action != SelectorActionPromotion && item.Action != SelectorActionNoOp {
			return fmt.Errorf("%w: invalid selector action %q", ErrInvalidDeclaration, item.Action)
		}
		if err := validateSelector(item.Final); err != nil {
			return err
		}
		if item.Action == SelectorActionNoOp && (item.Expected == nil || *item.Expected != item.Final) {
			return fmt.Errorf("%w: no-op selector must preserve one exact row", ErrInvalidDeclaration)
		}
	}
	for _, item := range plan.ProjectionItems {
		if item.Action != ProjectionActionInsert && item.Action != ProjectionActionUpdate && item.Action != ProjectionActionNoOp {
			return fmt.Errorf("%w: invalid projection action %q", ErrInvalidDeclaration, item.Action)
		}
		if item.ExpectedAffectedRows != 0 && item.ExpectedAffectedRows != 1 {
			return fmt.Errorf("%w: invalid projection affected-row expectation", ErrInvalidDeclaration)
		}
		if err := validateDeploymentProjection(item.Final); err != nil {
			return err
		}
		switch item.Action {
		case ProjectionActionInsert:
			if item.Expected != nil || item.ExpectedAffectedRows != 1 {
				return fmt.Errorf("%w: projection insert requires absent pre-state and one affected row", ErrInvalidDeclaration)
			}
		case ProjectionActionUpdate:
			if item.Expected == nil || item.ExpectedAffectedRows != 1 {
				return fmt.Errorf("%w: projection update requires exact pre-state and one affected row", ErrInvalidDeclaration)
			}
			if err := validateDeploymentProjectionPredecessor(*item.Expected); err != nil {
				return err
			}
			if !sameDeploymentProjectionIdentity(*item.Expected, item.Final) ||
				item.Expected.SupportState != item.Final.SupportState ||
				item.Expected.EvidenceState != item.Final.EvidenceState ||
				!sameProjectionTime(item.Expected.VerifiedAt, item.Final.VerifiedAt) ||
				item.Expected.SupportRevision != item.Final.SupportRevision ||
				item.Final.Revision != item.Expected.Revision+1 {
				return fmt.Errorf("%w: projection update is not a deterministic same-row transition", ErrInvalidDeclaration)
			}
		case ProjectionActionNoOp:
			if item.Expected == nil || *item.Expected != item.Final || item.ExpectedAffectedRows != 0 {
				return fmt.Errorf("%w: projection no-op must preserve one exact row", ErrInvalidDeclaration)
			}
		}
	}
	if requireDigest && strings.TrimSpace(plan.PlanDigest) == "" {
		return fmt.Errorf("%w: plan digest is required", ErrInvalidDeclaration)
	}
	return nil
}

func sameDeploymentProjectionIdentity(left, right DeploymentProjection) bool {
	return left.DeploymentID == right.DeploymentID && left.OperationID == right.OperationID &&
		left.CapabilityID == right.CapabilityID
}

func sameProjectionTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func validateBinding(binding Binding) error {
	for name, value := range map[string]string{
		"operation_id":              binding.OperationID,
		"capability_id":             binding.CapabilityID,
		"execution_profile_id":      binding.ExecutionProfileID,
		"execution_profile_version": binding.ExecutionProfileVersion,
		"protocol_profile_id":       binding.ProtocolProfileID,
		"protocol_profile_version":  binding.ProtocolProfileVersion,
		"probe_contract_id":         binding.ProbeContractID,
		"probe_contract_version":    binding.ProbeContractVersion,
		"expected_provider":         binding.ExpectedProvider,
		"expected_upstream_model":   binding.ExpectedUpstreamModel,
		"public_path":               binding.PublicPath,
		"introduced_migration":      binding.IntroducedMigration,
	} {
		if err := validateText(name, value); err != nil {
			return err
		}
	}
	if !strings.HasPrefix(binding.PublicPath, "/") {
		return fmt.Errorf("%w: public_path must start with slash", ErrInvalidDeclaration)
	}
	return nil
}

func validateSelector(selector Selector) error {
	for name, value := range map[string]string{
		"operation_id":              selector.OperationID,
		"capability_id":             selector.CapabilityID,
		"execution_profile_id":      selector.ExecutionProfileID,
		"execution_profile_version": selector.ExecutionProfileVersion,
		"protocol_profile_id":       selector.ProtocolProfileID,
		"protocol_profile_version":  selector.ProtocolProfileVersion,
		"probe_contract_id":         selector.ProbeContractID,
		"probe_contract_version":    selector.ProbeContractVersion,
	} {
		if err := validateText(name, value); err != nil {
			return err
		}
	}
	return nil
}

func validateSelectorTransition(transition SelectorTransition) error {
	selector := Selector{
		OperationID: transition.OperationID, CapabilityID: transition.CapabilityID,
		ExecutionProfileID: transition.ExecutionProfileID, ExecutionProfileVersion: transition.ExecutionProfileVersion,
		ProtocolProfileID: transition.ProtocolProfileID, ProtocolProfileVersion: transition.ProtocolProfileVersion,
		ProbeContractID: transition.Final.ProbeContractID, ProbeContractVersion: transition.Final.ProbeContractVersion,
	}
	if err := validateSelector(selector); err != nil {
		return err
	}
	if transition.ExpectedPredecessor != nil {
		if err := validateText("expected_predecessor.probe_contract_id", transition.ExpectedPredecessor.ProbeContractID); err != nil {
			return err
		}
		if err := validateText("expected_predecessor.probe_contract_version", transition.ExpectedPredecessor.ProbeContractVersion); err != nil {
			return err
		}
	}
	return nil
}

func validateProjectionEffect(effect ProjectionEffect) error {
	for name, value := range map[string]string{
		"operation_id":              effect.OperationID,
		"capability_id":             effect.CapabilityID,
		"execution_profile_id":      effect.ExecutionProfileID,
		"execution_profile_version": effect.ExecutionProfileVersion,
		"protocol_profile_id":       effect.ProtocolProfileID,
		"protocol_profile_version":  effect.ProtocolProfileVersion,
		"expected_provider":         effect.ExpectedProvider,
		"expected_upstream_model":   effect.ExpectedUpstreamModel,
		"final_contract_ref":        effect.FinalContractRef,
	} {
		if err := validateText(name, value); err != nil {
			return err
		}
	}
	if !effect.AllowInsert && strings.TrimSpace(effect.ExpectedContractRef) == "" {
		return fmt.Errorf("%w: projection update requires expected_contract_ref", ErrInvalidDeclaration)
	}
	if strings.TrimSpace(effect.ExpectedContractRef) != effect.ExpectedContractRef || strings.ContainsAny(effect.ExpectedContractRef, "\r\n") {
		return fmt.Errorf("%w: invalid expected_contract_ref", ErrInvalidDeclaration)
	}
	return nil
}

func validateProfileAttachmentGuard(guard ProfileAttachmentGuard) error {
	if err := validateText("profile_attachment_guard.execution_profile_id", guard.ExecutionProfileID); err != nil {
		return err
	}
	return validateText("profile_attachment_guard.execution_profile_version", guard.ExecutionProfileVersion)
}

func validateDeploymentProjection(projection DeploymentProjection) error {
	if projection.DeploymentID <= 0 || projection.Revision <= 0 || projection.SupportRevision <= 0 {
		return fmt.Errorf("%w: invalid deployment projection numeric identity", ErrInvalidDeclaration)
	}
	for name, value := range map[string]string{
		"operation_id":   projection.OperationID,
		"capability_id":  projection.CapabilityID,
		"support_state":  projection.SupportState,
		"evidence_state": projection.EvidenceState,
		"evidence_ref":   projection.EvidenceRef,
	} {
		if err := validateText(name, value); err != nil {
			return err
		}
	}
	return nil
}

func validateDeploymentProjectionPredecessor(projection DeploymentProjection) error {
	if projection.DeploymentID <= 0 || projection.Revision <= 0 || projection.SupportRevision <= 0 {
		return fmt.Errorf("%w: invalid deployment projection predecessor numeric identity", ErrInvalidDeclaration)
	}
	for name, value := range map[string]string{
		"operation_id":   projection.OperationID,
		"capability_id":  projection.CapabilityID,
		"support_state":  projection.SupportState,
		"evidence_state": projection.EvidenceState,
	} {
		if err := validateText(name, value); err != nil {
			return err
		}
	}
	if projection.EvidenceRef != "" {
		if err := validateText("evidence_ref", projection.EvidenceRef); err != nil {
			return err
		}
	}
	return nil
}

func validateText(name, value string) error {
	if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("%w: invalid %s", ErrInvalidDeclaration, name)
	}
	return nil
}

func validateMigrationID(name, value string) error {
	if err := validateText(name, value); err != nil {
		return err
	}
	if !migrationIDPattern.MatchString(value) {
		return fmt.Errorf("%w: invalid %s", ErrInvalidDeclaration, name)
	}
	return nil
}

func bindingKey(binding Binding) string {
	return strings.Join([]string{binding.OperationID, binding.CapabilityID, binding.ExecutionProfileID,
		binding.ExecutionProfileVersion, binding.ProtocolProfileID, binding.ProtocolProfileVersion,
		binding.ProbeContractID, binding.ProbeContractVersion}, "\x00")
}

func selectorKey(selector Selector) string {
	return strings.Join([]string{selector.OperationID, selector.CapabilityID, selector.ExecutionProfileID,
		selector.ExecutionProfileVersion, selector.ProtocolProfileID, selector.ProtocolProfileVersion}, "\x00")
}

func bindingKeyFromSelector(selector Selector) string {
	return strings.Join([]string{selector.OperationID, selector.CapabilityID, selector.ExecutionProfileID,
		selector.ExecutionProfileVersion, selector.ProtocolProfileID, selector.ProtocolProfileVersion,
		selector.ProbeContractID, selector.ProbeContractVersion}, "\x00")
}

func selectorTransitionKey(transition SelectorTransition) string {
	return strings.Join([]string{transition.OperationID, transition.CapabilityID, transition.ExecutionProfileID,
		transition.ExecutionProfileVersion, transition.ProtocolProfileID, transition.ProtocolProfileVersion}, "\x00")
}

func projectionEffectKey(effect ProjectionEffect) string {
	return strings.Join([]string{effect.OperationID, effect.CapabilityID, effect.ExecutionProfileID,
		effect.ExecutionProfileVersion, effect.ProtocolProfileID, effect.ProtocolProfileVersion,
		effect.ExpectedProvider, effect.ExpectedUpstreamModel}, "\x00")
}

func profileAttachmentGuardKey(guard ProfileAttachmentGuard) string {
	return guard.ExecutionProfileID + "\x00" + guard.ExecutionProfileVersion
}

func deploymentProjectionKey(projection DeploymentProjection) string {
	return fmt.Sprintf("%020d\x00%s\x00%s", projection.DeploymentID, projection.OperationID, projection.CapabilityID)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
