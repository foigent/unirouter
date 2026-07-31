package asyncmedia

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	imageManifestPath = "internal/endpointprotocol/artifacts/manifests/openai-gpt-image.v1.json"
)

var (
	portableIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	objectRefPattern  = regexp.MustCompile(`^media_[a-z0-9][a-z0-9._-]{0,95}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	unsignedPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
)

var requiredThreatControls = []string{
	"cancel_complete_race_and_double_charge",
	"cross_tenant_object_reference",
	"lease_split_brain_and_duplicate_dispatch",
	"malformed_media_and_decompression_bomb",
	"mutable_object_substitution",
	"provider_callback_spoofing",
	"raw_media_or_prompt_persistence",
	"retention_purge_failure",
	"signed_url_or_locator_leakage",
}

var commonGapIDs = []string{
	"charge_trigger_unresolved",
	"media_provider_unresolved",
	"object_store_unresolved",
	"retention_unresolved",
}

func ValidateFixture(body []byte) (ValidatedFixture, error) {
	fixture, err := decodeFixture(body)
	if err != nil {
		return ValidatedFixture{}, err
	}
	dependencies, err := historicalDependencies()
	if err != nil {
		return ValidatedFixture{}, err
	}
	if !sameArtifactSet(fixture.Dependencies, dependencies) {
		return ValidatedFixture{},
			contractError(ErrConflict, "dependency_identity_mismatch")
	}
	for _, dependency := range dependencies {
		if dependency.Path == imageManifestPath &&
			fixture.Plan.EndpointEvidence.ReferenceManifestSHA256 !=
				dependency.SHA256 {
			return ValidatedFixture{},
				contractError(ErrConflict, "endpoint_reference_digest_mismatch")
		}
	}
	if err := validateFixtureIdentity(fixture); err != nil {
		return ValidatedFixture{}, err
	}
	if err := validatePlan(fixture.Plan); err != nil {
		return ValidatedFixture{}, err
	}
	if err := validateExpected(fixture); err != nil {
		return ValidatedFixture{}, err
	}
	semanticSHA, err := canonicalHash(fixture.Plan)
	if err != nil {
		return ValidatedFixture{},
			contractError(ErrInvalid, "semantic_hash_failed")
	}
	return ValidatedFixture{Fixture: fixture, SemanticSHA: semanticSHA}, nil
}

func ComputeIdempotencyKey(plan JobPlan) (string, error) {
	input := struct {
		TenantScope  TenantScope       `json:"tenant_scope"`
		Operation    string            `json:"operation_class"`
		IntentSHA256 string            `json:"intent_sha256"`
		InputObjects []MediaObjectRef  `json:"input_objects"`
		Output       OutputExpectation `json:"output"`
	}{
		TenantScope: plan.TenantScope, Operation: plan.OperationClass,
		IntentSHA256: plan.IntentSHA256, InputObjects: plan.InputObjects,
		Output: plan.Output,
	}
	return canonicalHash(input)
}

func decodeFixture(body []byte) (Fixture, error) {
	if len(body) == 0 || len(body) > 1<<20 {
		return Fixture{}, contractError(ErrInvalid, "input_size_invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(
		body,
		eventconnector.StrictJSONOptions{RejectNull: true},
	); strictErr != nil {
		if strictErr.Code == "duplicate_json_key" {
			return Fixture{}, contractError(ErrInvalid, "duplicate_json_key")
		}
		return Fixture{}, contractError(ErrInvalid, "input_json_invalid")
	}
	var fixture Fixture
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		return Fixture{}, contractError(ErrInvalid, "input_shape_invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Fixture{}, contractError(ErrInvalid, "trailing_json")
	}
	return fixture, nil
}

func validateFixtureIdentity(fixture Fixture) error {
	if fixture.SchemaVersion != FixtureSchemaVersion {
		return contractError(ErrInvalid, "fixture_schema_unsupported")
	}
	if !portableIDPattern.MatchString(fixture.FixtureID) {
		return contractError(ErrInvalid, "fixture_id_invalid")
	}
	if fixture.Plan.ContractVersion != ContractVersion {
		return contractError(ErrInvalid, "contract_version_unsupported")
	}
	if fixture.Plan.PlanID != fixture.FixtureID+".plan" {
		return contractError(ErrConflict, "fixture_plan_identity_mismatch")
	}
	return nil
}

func validatePlan(plan JobPlan) error {
	if !portableIDPattern.MatchString(plan.PlanID) {
		return contractError(ErrInvalid, "plan_id_invalid")
	}
	if plan.RuntimeState != RuntimeStateDecisionGated {
		return contractError(ErrConflict, "runtime_state_not_decision_gated")
	}
	if err := validateTenantScope(plan.TenantScope); err != nil {
		return err
	}
	if !digestPattern.MatchString(plan.IntentSHA256) ||
		!digestPattern.MatchString(plan.IdempotencyKey) {
		return contractError(ErrInvalid, "plan_digest_invalid")
	}
	for index, object := range plan.InputObjects {
		if err := validateMediaObject(plan.TenantScope, object); err != nil {
			return err
		}
		for prior := 0; prior < index; prior++ {
			if plan.InputObjects[prior].ObjectRef == object.ObjectRef {
				return contractError(ErrConflict, "duplicate_object_ref")
			}
		}
	}
	if plan.Output.Count != "1" {
		return contractError(ErrInvalid, "output_count_unsupported")
	}
	switch plan.OperationClass {
	case OperationTextToImage:
		if len(plan.InputObjects) != 0 || plan.Output.Modality != "image" ||
			plan.EndpointEvidence.CoverageStatus != CoverageDecisionGatedImage {
			return contractError(ErrConflict, "text_to_image_semantics_invalid")
		}
	case OperationImageToVideo:
		if len(plan.InputObjects) != 1 ||
			!strings.HasPrefix(plan.InputObjects[0].MediaType, "image/") ||
			plan.Output.Modality != "video" ||
			plan.EndpointEvidence.CoverageStatus != CoverageUnavailable {
			return contractError(ErrConflict, "image_to_video_semantics_invalid")
		}
	default:
		return contractError(ErrInvalid, "operation_class_unsupported")
	}
	if err := validateDecisionGates(plan.DecisionGates); err != nil {
		return err
	}
	if plan.Lifecycle != (LifecycleBoundary{
		AdmissionState: "contract_only", QueueState: "not_created",
		LeaseState: "not_created", ExecutionState: "not_started",
		OutcomeState: "unknown", CancellationState: "not_requested",
		SettlementState: "not_started_decision_gated",
	}) {
		return contractError(ErrConflict, "lifecycle_started_before_gate")
	}
	if err := validateEndpointEvidence(plan.EndpointEvidence); err != nil {
		return err
	}
	if plan.ChargeState != ChargeUnresolved {
		return contractError(ErrConflict, "charge_state_not_unresolved")
	}
	if err := validateGaps(plan.OperationClass, plan.Gaps); err != nil {
		return err
	}
	if !stringSlicesEqual(plan.ThreatControls, requiredThreatControls) {
		return contractError(ErrConflict, "threat_control_coverage_incomplete")
	}
	if plan.Privacy != (PrivacyBoundary{MetadataOnly: true}) {
		return contractError(ErrConflict, "privacy_boundary_violated")
	}
	if plan.RuntimeBoundary != (RuntimeBoundary{
		RuntimeMutation: RuntimeMutationNone, ExternalDecisionGate: "P7-D03",
	}) {
		return contractError(ErrConflict, "runtime_boundary_violated")
	}
	if plan.Compatibility != (CompatibilityBoundary{
		RequestPathUnchanged: true, RoutingUnchanged: true,
		BillingUnchanged: true, CompletedFeedWireUnchanged: true,
		OutboxIdentityUnchanged: true, CursorBehaviorUnchanged: true,
	}) {
		return contractError(ErrConflict, "compatibility_boundary_violated")
	}
	idempotencyKey, err := ComputeIdempotencyKey(plan)
	if err != nil {
		return contractError(ErrInvalid, "idempotency_hash_failed")
	}
	if plan.IdempotencyKey != idempotencyKey {
		return contractError(ErrConflict, "idempotency_key_mismatch")
	}
	return nil
}

func validateTenantScope(scope TenantScope) error {
	for _, value := range []string{scope.OrgRef, scope.ProjectRef} {
		if !unsignedPattern.MatchString(value) {
			return contractError(ErrInvalid, "tenant_scope_invalid")
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return contractError(ErrInvalid, "tenant_scope_invalid")
		}
	}
	return nil
}

func validateMediaObject(scope TenantScope, object MediaObjectRef) error {
	if !objectRefPattern.MatchString(object.ObjectRef) ||
		!digestPattern.MatchString(object.ContentSHA256) ||
		!object.Immutable {
		return contractError(ErrInvalid, "object_reference_invalid")
	}
	if object.TenantScope != scope {
		return contractError(ErrConflict, "cross_tenant_object_reference")
	}
	if object.MediaType != "image/jpeg" && object.MediaType != "image/png" {
		return contractError(ErrInvalid, "input_media_type_unsupported")
	}
	if !unsignedPattern.MatchString(object.SizeBytes) {
		return contractError(ErrInvalid, "object_size_invalid")
	}
	size, err := strconv.ParseUint(object.SizeBytes, 10, 64)
	if err != nil || size == 0 || size > math.MaxInt64 {
		return contractError(ErrInvalid, "object_size_invalid")
	}
	return nil
}

func validateDecisionGates(gates DecisionGates) error {
	if gates.MediaProvider != DecisionPending ||
		gates.ObjectStore != DecisionPending ||
		gates.Retention != DecisionPending ||
		gates.ChargeTrigger != DecisionPending {
		return contractError(ErrConflict, "p7_d03_decision_invented")
	}
	return nil
}

func validateEndpointEvidence(evidence EndpointEvidence) error {
	if evidence.ReferenceProfileID != endpointprotocol.ProfileOpenAIImageGPT ||
		evidence.ReferenceManifestPath != imageManifestPath ||
		!digestPattern.MatchString(evidence.ReferenceManifestSHA256) {
		return contractError(ErrConflict, "endpoint_reference_identity_mismatch")
	}
	if evidence.CoverageStatus != CoverageDecisionGatedImage &&
		evidence.CoverageStatus != CoverageUnavailable {
		return contractError(ErrInvalid, "endpoint_coverage_status_invalid")
	}
	return nil
}

func validateGaps(operation string, gaps []GapDeclaration) error {
	expected := append([]string(nil), commonGapIDs...)
	if operation == OperationImageToVideo {
		expected = append(expected, "endpoint_profile_unavailable")
		sort.Strings(expected)
	}
	actual := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		if !portableIDPattern.MatchString(gap.GapID) ||
			gap.State != "missing" ||
			gap.Provenance != "declared_by_product_gate" {
			return contractError(ErrInvalid, "gap_declaration_invalid")
		}
		actual = append(actual, gap.GapID)
	}
	if !stringSlicesEqual(actual, expected) {
		return contractError(ErrConflict, "gap_declarations_incomplete")
	}
	return nil
}

func validateExpected(fixture Fixture) error {
	expected := fixture.Expected
	gapIDs := make([]string, 0, len(fixture.Plan.Gaps))
	for _, gap := range fixture.Plan.Gaps {
		gapIDs = append(gapIDs, gap.GapID)
	}
	if expected.OperationClass != fixture.Plan.OperationClass ||
		expected.InputObjectCount != len(fixture.Plan.InputObjects) ||
		expected.OutputModality != fixture.Plan.Output.Modality ||
		expected.CoverageStatus != fixture.Plan.EndpointEvidence.CoverageStatus ||
		!stringSlicesEqual(expected.GapIDs, gapIDs) ||
		expected.RuntimeMutation != fixture.Plan.RuntimeBoundary.RuntimeMutation {
		return contractError(ErrConflict, "fixture_expected_mismatch")
	}
	return nil
}

func canonicalHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return "", err
	}
	canonical, connectorErr := eventconnector.CanonicalJSON(decoded)
	if connectorErr != nil {
		return "", connectorErr
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func hash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sameArtifactSet(left, right []ArtifactIdentity) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]ArtifactIdentity(nil), left...)
	rightCopy := append([]ArtifactIdentity(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool { return leftCopy[i].Path < leftCopy[j].Path })
	sort.Slice(rightCopy, func(i, j int) bool { return rightCopy[i].Path < rightCopy[j].Path })
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
