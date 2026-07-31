package asyncmedia

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	SchemaArtifactPath = "artifacts/async-media-job.v1.schema.json"
	GoldenReportPath   = "artifacts/reports/core.v1.json"
)

//go:embed artifacts/async-media-job.v1.schema.json artifacts/fixtures/*.json artifacts/reports/core.v1.json
var conformanceArtifacts embed.FS

type loadedFixture struct {
	identity  ArtifactIdentity
	raw       []byte
	validated ValidatedFixture
}

func ArtifactDigests() (
	ArtifactIdentity,
	[]ArtifactIdentity,
	ArtifactIdentity,
	error,
) {
	schema, err := artifactIdentity(SchemaArtifactPath)
	if err != nil {
		return ArtifactIdentity{}, nil, ArtifactIdentity{}, err
	}
	fixtures, err := loadFixtures()
	if err != nil {
		return ArtifactIdentity{}, nil, ArtifactIdentity{}, err
	}
	fixtureIdentities := make([]ArtifactIdentity, 0, len(fixtures))
	for _, fixture := range fixtures {
		fixtureIdentities = append(fixtureIdentities, fixture.identity)
	}
	golden, err := artifactIdentity(GoldenReportPath)
	return schema, fixtureIdentities, golden, err
}

func GoldenReportBytes() ([]byte, error) {
	return conformanceArtifacts.ReadFile(GoldenReportPath)
}

func RunConformance() (ConformanceReport, error) {
	schema, err := artifactIdentity(SchemaArtifactPath)
	if err != nil {
		return ConformanceReport{}, err
	}
	fixtures, err := loadFixtures()
	if err != nil {
		return ConformanceReport{}, err
	}
	if len(fixtures) != 2 {
		return ConformanceReport{}, errors.New("asyncmedia: expected two fixtures")
	}
	dependencies, err := historicalDependencies()
	if err != nil {
		return ConformanceReport{}, err
	}
	report := ConformanceReport{
		SchemaVersion:   ConformanceReportVersion,
		ContractVersion: ContractVersion,
		Schema:          schema,
		Fixtures:        make([]ArtifactIdentity, 0, len(fixtures)),
		Dependencies:    dependencies,
		Cases:           make([]ConformanceCase, 0, 56),
	}
	for _, fixture := range fixtures {
		report.Fixtures = append(report.Fixtures, fixture.identity)
		report.Cases = append(report.Cases, runFixtureCases(fixture)...)
	}
	report.Cases = append(
		report.Cases,
		runNegativeCases(fixtures[0], fixtures[1])...,
	)
	report.Summary.Fixtures = len(fixtures)
	report.Summary.Cases = len(report.Cases)
	for _, testCase := range report.Cases {
		if testCase.Passed {
			report.Summary.Passed++
		} else {
			report.Summary.Failed++
		}
	}
	return report, nil
}

func MarshalConformanceReport(report ConformanceReport) ([]byte, error) {
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func runFixtureCases(fixture loadedFixture) []ConformanceCase {
	first, firstErr := ValidateFixture(fixture.raw)
	second, secondErr := ValidateFixture(fixture.raw)
	dependencies, dependencyErr := historicalDependencies()
	plan := fixture.validated.Fixture.Plan
	expected := fixture.validated.Fixture.Expected
	actualGaps := make([]string, 0, len(plan.Gaps))
	for _, gap := range plan.Gaps {
		actualGaps = append(actualGaps, gap.GapID)
	}
	idempotency, idempotencyErr := ComputeIdempotencyKey(plan)
	objectBoundary := true
	for _, object := range plan.InputObjects {
		objectBoundary = objectBoundary &&
			object.TenantScope == plan.TenantScope &&
			object.Immutable &&
			digestPattern.MatchString(object.ContentSHA256) &&
			!strings.Contains(object.ObjectRef, "://")
	}
	decisionBoundary := plan.DecisionGates == (DecisionGates{
		MediaProvider: DecisionPending, ObjectStore: DecisionPending,
		Retention: DecisionPending, ChargeTrigger: DecisionPending,
	})
	lifecycleBoundary := plan.Lifecycle == (LifecycleBoundary{
		AdmissionState: "contract_only", QueueState: "not_created",
		LeaseState: "not_created", ExecutionState: "not_started",
		OutcomeState: "unknown", CancellationState: "not_requested",
		SettlementState: "not_started_decision_gated",
	})
	privacyBoundary := plan.Privacy == (PrivacyBoundary{MetadataOnly: true})
	runtimeBoundary := plan.RuntimeBoundary == (RuntimeBoundary{
		RuntimeMutation: RuntimeMutationNone, ExternalDecisionGate: "P7-D03",
	})
	compatibilityBoundary := plan.Compatibility == (CompatibilityBoundary{
		RequestPathUnchanged: true, RoutingUnchanged: true,
		BillingUnchanged: true, CompletedFeedWireUnchanged: true,
		OutboxIdentityUnchanged: true, CursorBehaviorUnchanged: true,
	})
	fixtureID := fixture.validated.Fixture.FixtureID
	return []ConformanceCase{
		conformanceCase(fixtureID+"/strict-validation", []string{"fixture", "strict_json"}, firstErr == nil, "strict_fixture_valid", errorCode(firstErr)),
		conformanceCase(fixtureID+"/determinism", []string{"canonicalization", "determinism", "hash"}, firstErr == nil && secondErr == nil && first.SemanticSHA == second.SemanticSHA, "semantic_hash_stable", firstErrorCode(firstErr, secondErr)),
		conformanceCase(fixtureID+"/dependency-pin", []string{"dependency", "hash"}, dependencyErr == nil && sameArtifactSet(dependencies, fixture.validated.Fixture.Dependencies), "b6_artifacts_exact", firstErrorCode(dependencyErr)),
		conformanceCase(fixtureID+"/identity", []string{"identity", "plan"}, plan.PlanID == fixtureID+".plan" && plan.RuntimeState == RuntimeStateDecisionGated, "plan_identity_decision_gated", "plan_identity_mismatch"),
		conformanceCase(fixtureID+"/tenant-scope", []string{"object", "tenant"}, plan.TenantScope.OrgRef == "101" && plan.TenantScope.ProjectRef == "202" && objectBoundary, "tenant_scoped_opaque_references", "tenant_scope_mismatch"),
		conformanceCase(fixtureID+"/idempotency", []string{"hash", "idempotency"}, idempotencyErr == nil && idempotency == plan.IdempotencyKey, "idempotency_key_exact", firstErrorCode(idempotencyErr)),
		conformanceCase(fixtureID+"/decision-gates", []string{"decision_gate", "missing"}, decisionBoundary && stringSlicesEqual(actualGaps, expected.GapIDs), "four_product_decisions_unresolved", "decision_gate_mismatch"),
		conformanceCase(fixtureID+"/lifecycle", []string{"cancellation", "lease", "lifecycle", "queue"}, lifecycleBoundary, "no_job_state_created", "lifecycle_boundary_mismatch"),
		conformanceCase(fixtureID+"/endpoint-coverage", []string{"endpoint", "gap", "profile"}, plan.EndpointEvidence.CoverageStatus == expected.CoverageStatus && digestPattern.MatchString(plan.EndpointEvidence.ReferenceManifestSHA256), "endpoint_coverage_explicit", "endpoint_coverage_mismatch"),
		conformanceCase(fixtureID+"/object-metadata", []string{"media", "object", "privacy"}, objectBoundary && len(plan.InputObjects) == expected.InputObjectCount, "metadata_only_object_identity", "object_metadata_mismatch"),
		conformanceCase(fixtureID+"/charge", []string{"billing", "finality"}, plan.ChargeState == ChargeUnresolved && !plan.RuntimeBoundary.CustomerChargeCreated, "charge_trigger_unresolved_no_debt", "charge_boundary_mismatch"),
		conformanceCase(fixtureID+"/privacy", []string{"privacy"}, privacyBoundary, "no_raw_content_locator_or_credential", "privacy_boundary_mismatch"),
		conformanceCase(fixtureID+"/threat-model", []string{"security", "threat_model"}, stringSlicesEqual(plan.ThreatControls, requiredThreatControls), "required_threat_classes_exact", "threat_control_coverage_incomplete"),
		conformanceCase(fixtureID+"/runtime-boundary", []string{"runtime_boundary"}, runtimeBoundary, "runtime_mutation_none", "runtime_boundary_mismatch"),
		conformanceCase(fixtureID+"/compatibility", []string{"compatibility", "cursor", "feed", "outbox"}, compatibilityBoundary, "existing_runtime_and_feed_unchanged", "compatibility_boundary_mismatch"),
	}
}

func runNegativeCases(first, second loadedFixture) []ConformanceCase {
	image := first
	video := second
	if image.validated.Fixture.Plan.OperationClass != OperationTextToImage {
		image, video = second, first
	}

	duplicate := bytes.Replace(
		image.raw,
		[]byte(`"schema_version":`),
		[]byte(`"schema_version":"unirouter.async-media-job-fixture.v1","schema_version":`),
		1,
	)
	_, duplicateErr := ValidateFixture(duplicate)
	rawPrompt := bytes.Replace(
		image.raw,
		[]byte(`"plan_id":`),
		[]byte(`"raw_prompt":"forbidden","plan_id":`),
		1,
	)
	_, rawPromptErr := ValidateFixture(rawPrompt)
	locator := bytes.Replace(
		video.raw,
		[]byte(`"object_ref":`),
		[]byte(`"storage_locator":"https://forbidden.invalid/object","object_ref":`),
		1,
	)
	_, locatorErr := ValidateFixture(locator)
	_, trailingErr := ValidateFixture(append(append([]byte(nil), image.raw...), []byte(`{}`)...))

	actualProvider := cloneFixture(image.validated.Fixture)
	actualProvider.Plan.DecisionGates.MediaProvider = "selected-provider"
	_, providerErr := ValidateFixture(mustMarshal(actualProvider))
	actualStore := cloneFixture(image.validated.Fixture)
	actualStore.Plan.DecisionGates.ObjectStore = "selected-object-store"
	_, storeErr := ValidateFixture(mustMarshal(actualStore))
	actualRetention := cloneFixture(image.validated.Fixture)
	actualRetention.Plan.DecisionGates.Retention = "7d"
	_, retentionErr := ValidateFixture(mustMarshal(actualRetention))
	actualCharge := cloneFixture(image.validated.Fixture)
	actualCharge.Plan.DecisionGates.ChargeTrigger = "on_accept"
	_, chargeErr := ValidateFixture(mustMarshal(actualCharge))

	runtimeRegistered := cloneFixture(image.validated.Fixture)
	runtimeRegistered.Plan.RuntimeBoundary.RuntimeRegistered = true
	_, runtimeErr := ValidateFixture(mustMarshal(runtimeRegistered))
	networkCall := cloneFixture(image.validated.Fixture)
	networkCall.Plan.RuntimeBoundary.ProviderNetworkCall = true
	_, networkErr := ValidateFixture(mustMarshal(networkCall))
	customerCharge := cloneFixture(image.validated.Fixture)
	customerCharge.Plan.RuntimeBoundary.CustomerChargeCreated = true
	_, customerChargeErr := ValidateFixture(mustMarshal(customerCharge))
	started := cloneFixture(image.validated.Fixture)
	started.Plan.Lifecycle.QueueState = "queued"
	_, lifecycleErr := ValidateFixture(mustMarshal(started))

	crossTenant := cloneFixture(video.validated.Fixture)
	crossTenant.Plan.InputObjects[0].TenantScope.ProjectRef = "203"
	_, crossTenantErr := ValidateFixture(mustMarshal(crossTenant))
	mutable := cloneFixture(video.validated.Fixture)
	mutable.Plan.InputObjects[0].Immutable = false
	_, mutableErr := ValidateFixture(mustMarshal(mutable))
	leadingZero := cloneFixture(video.validated.Fixture)
	leadingZero.Plan.InputObjects[0].SizeBytes = "04096"
	_, leadingZeroErr := ValidateFixture(mustMarshal(leadingZero))
	overflow := cloneFixture(video.validated.Fixture)
	overflow.Plan.InputObjects[0].SizeBytes = "9223372036854775808"
	_, overflowErr := ValidateFixture(mustMarshal(overflow))
	invalidDigest := cloneFixture(video.validated.Fixture)
	invalidDigest.Plan.InputObjects[0].ContentSHA256 = "sha256:INVALID"
	_, digestErr := ValidateFixture(mustMarshal(invalidDigest))
	idempotency := cloneFixture(image.validated.Fixture)
	idempotency.Plan.IdempotencyKey = "sha256:" + strings.Repeat("0", 64)
	_, idempotencyErr := ValidateFixture(mustMarshal(idempotency))
	duplicateObject := cloneFixture(video.validated.Fixture)
	duplicateObject.Plan.InputObjects = append(
		duplicateObject.Plan.InputObjects,
		duplicateObject.Plan.InputObjects[0],
	)
	_, duplicateObjectErr := ValidateFixture(mustMarshal(duplicateObject))

	missingThreat := cloneFixture(image.validated.Fixture)
	missingThreat.Plan.ThreatControls = missingThreat.Plan.ThreatControls[:8]
	_, threatErr := ValidateFixture(mustMarshal(missingThreat))
	missingGap := cloneFixture(image.validated.Fixture)
	missingGap.Plan.Gaps = missingGap.Plan.Gaps[:3]
	missingGap.Expected.GapIDs = missingGap.Expected.GapIDs[:3]
	_, gapErr := ValidateFixture(mustMarshal(missingGap))
	wrongCoverage := cloneFixture(video.validated.Fixture)
	wrongCoverage.Plan.EndpointEvidence.CoverageStatus =
		CoverageDecisionGatedImage
	wrongCoverage.Expected.CoverageStatus = CoverageDecisionGatedImage
	_, coverageErr := ValidateFixture(mustMarshal(wrongCoverage))
	wrongReference := cloneFixture(image.validated.Fixture)
	wrongReference.Plan.EndpointEvidence.ReferenceManifestSHA256 =
		"sha256:" + strings.Repeat("0", 64)
	_, referenceErr := ValidateFixture(mustMarshal(wrongReference))
	wrongDependency := cloneFixture(image.validated.Fixture)
	wrongDependency.Dependencies[0].SHA256 =
		"sha256:" + strings.Repeat("0", 64)
	_, dependencyErr := ValidateFixture(mustMarshal(wrongDependency))
	wrongExpected := cloneFixture(image.validated.Fixture)
	wrongExpected.Expected.OutputModality = "video"
	_, expectedErr := ValidateFixture(mustMarshal(wrongExpected))

	return []ConformanceCase{
		negativeCase("duplicate-key", []string{"negative", "strict_json"}, duplicateErr, "duplicate_json_key"),
		negativeCase("raw-prompt-field", []string{"negative", "privacy"}, rawPromptErr, "input_shape_invalid"),
		negativeCase("storage-locator-field", []string{"negative", "privacy"}, locatorErr, "input_shape_invalid"),
		negativeCase("trailing-json", []string{"negative", "strict_json"}, trailingErr, "input_json_invalid"),
		negativeCase("provider-decision", []string{"decision_gate", "negative"}, providerErr, "p7_d03_decision_invented"),
		negativeCase("object-store-decision", []string{"decision_gate", "negative"}, storeErr, "p7_d03_decision_invented"),
		negativeCase("retention-decision", []string{"decision_gate", "negative", "retention"}, retentionErr, "p7_d03_decision_invented"),
		negativeCase("charge-trigger-decision", []string{"billing", "decision_gate", "negative"}, chargeErr, "p7_d03_decision_invented"),
		negativeCase("runtime-registration", []string{"negative", "runtime_boundary"}, runtimeErr, "runtime_boundary_violated"),
		negativeCase("provider-network", []string{"negative", "network", "runtime_boundary"}, networkErr, "runtime_boundary_violated"),
		negativeCase("customer-charge", []string{"billing", "negative", "runtime_boundary"}, customerChargeErr, "runtime_boundary_violated"),
		negativeCase("lifecycle-start", []string{"lifecycle", "negative", "queue"}, lifecycleErr, "lifecycle_started_before_gate"),
		negativeCase("cross-tenant-object", []string{"negative", "object", "tenant"}, crossTenantErr, "cross_tenant_object_reference"),
		negativeCase("mutable-object", []string{"negative", "object"}, mutableErr, "object_reference_invalid"),
		negativeCase("leading-zero-size", []string{"decimal", "negative", "object"}, leadingZeroErr, "object_size_invalid"),
		negativeCase("int64-overflow-size", []string{"decimal", "int64", "negative", "object"}, overflowErr, "object_size_invalid"),
		negativeCase("invalid-content-digest", []string{"hash", "negative", "object"}, digestErr, "object_reference_invalid"),
		negativeCase("idempotency-tamper", []string{"hash", "idempotency", "negative"}, idempotencyErr, "idempotency_key_mismatch"),
		negativeCase("duplicate-object-ref", []string{"negative", "object"}, duplicateObjectErr, "duplicate_object_ref"),
		negativeCase("missing-threat-control", []string{"negative", "security", "threat_model"}, threatErr, "threat_control_coverage_incomplete"),
		negativeCase("missing-gap", []string{"gap", "negative"}, gapErr, "gap_declarations_incomplete"),
		negativeCase("video-profile-overclaim", []string{"endpoint", "gap", "negative"}, coverageErr, "image_to_video_semantics_invalid"),
		negativeCase("endpoint-reference-digest", []string{"endpoint", "hash", "negative"}, referenceErr, "endpoint_reference_digest_mismatch"),
		negativeCase("dependency-digest", []string{"dependency", "hash", "negative"}, dependencyErr, "dependency_identity_mismatch"),
		negativeCase("fixture-expected", []string{"fixture", "negative"}, expectedErr, "fixture_expected_mismatch"),
	}
}

func loadFixtures() ([]loadedFixture, error) {
	paths, err := fs.Glob(conformanceArtifacts, "artifacts/fixtures/*.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	result := make([]loadedFixture, 0, len(paths))
	for _, path := range paths {
		body, readErr := conformanceArtifacts.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		validated, validateErr := ValidateFixture(body)
		if validateErr != nil {
			return nil, fmt.Errorf("%s: %w", path, validateErr)
		}
		result = append(result, loadedFixture{
			identity: ArtifactIdentity{
				Path: strings.TrimPrefix(path, "artifacts/"), SHA256: hash(body),
			},
			raw: body, validated: validated,
		})
	}
	return result, nil
}

func artifactIdentity(path string) (ArtifactIdentity, error) {
	body, err := conformanceArtifacts.ReadFile(path)
	if err != nil {
		return ArtifactIdentity{}, err
	}
	if strictErr := eventconnector.ValidateStrictJSON(
		body,
		eventconnector.StrictJSONOptions{},
	); strictErr != nil {
		return ArtifactIdentity{}, fmt.Errorf("%s: %s", path, strictErr.Code)
	}
	return ArtifactIdentity{
		Path: strings.TrimPrefix(path, "artifacts/"), SHA256: hash(body),
	}, nil
}

func cloneFixture(fixture Fixture) Fixture {
	body, _ := json.Marshal(fixture)
	var result Fixture
	_ = json.Unmarshal(body, &result)
	return result
}

func mustMarshal(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}

func conformanceCase(
	id string,
	classes []string,
	passed bool,
	success string,
	failure string,
) ConformanceCase {
	result := success
	if !passed {
		result = failure
		if result == "" {
			result = "failed"
		}
	}
	return ConformanceCase{
		CaseID: id, CaseClasses: sortedUnique(classes), Passed: passed,
		Result: result,
	}
}

func negativeCase(
	id string,
	classes []string,
	err error,
	expected string,
) ConformanceCase {
	actual := errorCode(err)
	return conformanceCase(
		"negative/"+id,
		append(classes, "negative"),
		actual == expected,
		expected+"_rejected",
		actual,
	)
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func errorCode(err error) string {
	var contractErr *ContractError
	if errors.As(err, &contractErr) {
		return contractErr.Code
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstErrorCode(values ...error) string {
	for _, err := range values {
		if err != nil {
			return errorCode(err)
		}
	}
	return ""
}
