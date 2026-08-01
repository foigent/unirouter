package riskcontrol

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/agenttrust"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/provenancepassport"
)

const (
	SchemaArtifactPath = "artifacts/risk-control-review.v1.schema.json"
	GoldenReportPath   = "artifacts/reports/core.v1.json"
)

//go:embed artifacts/risk-control-review.v1.schema.json artifacts/fixtures/*.json artifacts/reports/core.v1.json
var conformanceArtifacts embed.FS

type ArtifactIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ReviewSeed struct {
	ContractVersion     string `json:"contract_version"`
	ReviewID            string `json:"review_id"`
	EvaluatedAt         string `json:"evaluated_at"`
	Audience            string `json:"audience"`
	PolicyVersion       string `json:"policy_version"`
	RiskEpoch           string `json:"risk_epoch"`
	AssertionTTLSeconds string `json:"assertion_ttl_seconds"`
}

type FixtureExpected struct {
	Advice          string         `json:"advice"`
	Dimensions      RiskDimensions `json:"dimensions"`
	ProposalState   string         `json:"proposal_state"`
	ControlIDs      []string       `json:"control_ids"`
	CaptureGaps     int            `json:"capture_gaps"`
	PromotionGate   string         `json:"promotion_gate"`
	RuntimeMutation string         `json:"runtime_mutation"`
}

type ConformanceFixture struct {
	SchemaVersion string             `json:"schema_version"`
	FixtureID     string             `json:"fixture_id"`
	Dependencies  []ArtifactIdentity `json:"dependencies"`
	RequestSeed   ReviewSeed         `json:"request_seed"`
	Expected      FixtureExpected    `json:"expected"`
}

type ConformanceCase struct {
	CaseID      string   `json:"case_id"`
	CaseClasses []string `json:"case_classes"`
	Passed      bool     `json:"passed"`
	Result      string   `json:"result"`
}

type ConformanceReport struct {
	SchemaVersion   string             `json:"schema_version"`
	ContractVersion string             `json:"contract_version"`
	Schema          ArtifactIdentity   `json:"schema"`
	Fixtures        []ArtifactIdentity `json:"fixtures"`
	Dependencies    []ArtifactIdentity `json:"dependencies"`
	Cases           []ConformanceCase  `json:"cases"`
	Summary         struct {
		Fixtures int `json:"fixtures"`
		Cases    int `json:"cases"`
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
	} `json:"summary"`
}

type loadedFixture struct {
	identity ArtifactIdentity
	value    ConformanceFixture
	request  ReviewRequest
}

func ArtifactDigests() (ArtifactIdentity, []ArtifactIdentity, ArtifactIdentity, error) {
	schema, err := artifactIdentity(SchemaArtifactPath)
	if err != nil {
		return ArtifactIdentity{}, nil, ArtifactIdentity{}, err
	}
	loaded, err := loadFixtures()
	if err != nil {
		return ArtifactIdentity{}, nil, ArtifactIdentity{}, err
	}
	fixtures := make([]ArtifactIdentity, 0, len(loaded))
	for _, fixture := range loaded {
		fixtures = append(fixtures, fixture.identity)
	}
	golden, err := artifactIdentity(GoldenReportPath)
	return schema, fixtures, golden, err
}

func GoldenReportBytes() ([]byte, error) {
	return conformanceArtifacts.ReadFile(GoldenReportPath)
}

func SampleRequestBytes() ([]byte, error) {
	fixtures, err := loadFixtures()
	if err != nil {
		return nil, err
	}
	if len(fixtures) == 0 {
		return nil, errors.New("riskcontrol: fixtures missing")
	}
	return json.Marshal(fixtures[0].request)
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
	if len(fixtures) == 0 {
		return ConformanceReport{}, errors.New("riskcontrol: fixtures missing")
	}
	report := ConformanceReport{
		SchemaVersion:   ConformanceReportVersion,
		ContractVersion: ContractVersion,
		Schema:          schema,
		Fixtures:        make([]ArtifactIdentity, 0, len(fixtures)),
		Dependencies:    append([]ArtifactIdentity(nil), fixtures[0].value.Dependencies...),
		Cases:           make([]ConformanceCase, 0, 30),
	}
	for _, fixture := range fixtures {
		if !sameArtifactSet(report.Dependencies, fixture.value.Dependencies) {
			return ConformanceReport{}, fmt.Errorf("%s: dependency set mismatch", fixture.identity.Path)
		}
		report.Fixtures = append(report.Fixtures, fixture.identity)
		report.Cases = append(report.Cases, runFixtureCases(fixture)...)
	}
	report.Cases = append(report.Cases, runNegativeCases(fixtures[0])...)
	report.Summary.Fixtures = len(fixtures)
	report.Summary.Cases = len(report.Cases)
	for _, current := range report.Cases {
		if current.Passed {
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
	body := mustMarshal(fixture.request)
	first, firstErr := Evaluate(body)
	second, secondErr := Evaluate(body)
	firstBytes, _ := json.Marshal(first)
	secondBytes, _ := json.Marshal(second)
	service := NewService(NewMemoryStore())
	accepted, acceptedErr := service.Apply(body)
	pretty, _ := json.MarshalIndent(fixture.request, "", "  ")
	duplicate, duplicateErr := service.Apply(pretty)

	controlIDs := make([]string, 0, len(first.Proposal.Controls))
	noRuntimeControls := true
	for _, control := range first.Proposal.Controls {
		controlIDs = append(controlIDs, control.ControlID)
		noRuntimeControls = noRuntimeControls && !control.EnforcementAuthorized
	}
	boundedAssertion := first.Assertion.Scope == AssertionScope &&
		first.Assertion.AuthorityEffect == "downscope_only" &&
		first.Assertion.SignatureState == "not_issued_offline" &&
		first.Assertion.ExpiresAt == "2026-07-31T08:10:00.000000Z" &&
		!first.Assertion.MissingEvidenceCanIncreasePrivilege &&
		!first.Assertion.RuntimeEnforcementAuthorized
	proposalBoundary := first.Proposal.State == fixture.value.Expected.ProposalState &&
		first.Proposal.HumanReviewRequired &&
		!first.Proposal.SelfPromoted &&
		!first.Proposal.RuntimePromotionAuthorized &&
		first.Proposal.PromotionGate == fixture.value.Expected.PromotionGate
	runtimeBoundary := first.RuntimeBoundary.RuntimeMutation == fixture.value.Expected.RuntimeMutation &&
		!first.RuntimeBoundary.RemoteRiskDependency &&
		!first.RuntimeBoundary.ProviderOrShadowTraffic &&
		!first.RuntimeBoundary.CredentialsAccepted &&
		!first.RuntimeBoundary.RuntimePolicyRead &&
		!first.RuntimeBoundary.RuntimeEnforcementAuthorized &&
		!first.RuntimeBoundary.RuntimePromotionAuthorized
	sourceScopeBoundary := first.Evidence.SourceScope.OrgRef == "101" &&
		first.Evidence.SourceScope.ProjectRef == "202" &&
		first.Evidence.SourceScope.SourceInstance == "unirouter-development" &&
		first.Evidence.SourceScope.ScopeHash ==
			fixture.request.EvidenceBinding.PassportSourceScopeHash &&
		first.Evidence.SourceScope.TenantBindingStatus == ScopeBindingUnverified

	return []ConformanceCase{
		conformanceCase(fixture.value.FixtureID+"/determinism", []string{"canonicalization", "determinism", "hash"}, firstErr == nil && secondErr == nil && bytes.Equal(firstBytes, secondBytes), "report_bytes_stable", firstErrorCode(firstErr, secondErr)),
		conformanceCase(fixture.value.FixtureID+"/agent-trust", []string{"agent_trust", "receipt", "signature"}, first.Evidence.AgentTrustDecision == agenttrust.DecisionShadowOnly && first.Evidence.ReceiptCryptographicallyValid && !first.Evidence.ReceiptRuntimeTrusted, "b10_receipt_verified_not_runtime_trusted", "agent_trust_evidence_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/provenance", []string{"capture_gap", "provenance", "source"}, first.Evidence.SourceHash == fixture.request.EvidenceBinding.PassportSourceHash && first.Evidence.EnvelopeHash == fixture.request.EvidenceBinding.PassportEnvelopeHash && first.Evidence.CaptureGapCount == fixture.value.Expected.CaptureGaps, "b11_exact_source_and_gaps_bound", "provenance_evidence_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/source-scope", []string{"binding", "source", "tenant"}, sourceScopeBoundary, "source_scope_exact_tenant_mapping_unverified", "source_scope_boundary_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/evidence-binding", []string{"binding", "hash", "tenant"}, first.EvidenceBindingHash == first.Assertion.EvidenceBindingHash && first.Assertion.TenantRef == fixture.request.TenantRef, "evidence_binding_exact", "evidence_binding_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/risk-dimensions", []string{"advice", "risk"}, first.Dimensions == fixture.value.Expected.Dimensions, "categorical_risk_dimensions_exact", "risk_dimension_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/advice", []string{"advice", "shadow"}, first.Advice == fixture.value.Expected.Advice && first.Assertion.Advice == first.Advice, "shadow_advice_only", "advice_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/bounded-assertion", []string{"assertion", "audience", "epoch", "ttl"}, boundedAssertion, "bounded_unsigned_offline_assertion", "assertion_boundary_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/assertion-hash", []string{"assertion", "hash"}, digestPattern.MatchString(first.Assertion.AssertionHash), "assertion_hash_exact", "assertion_hash_invalid"),
		conformanceCase(fixture.value.FixtureID+"/proposal-controls", []string{"control", "proposal"}, stringSlicesEqual(controlIDs, fixture.value.Expected.ControlIDs) && noRuntimeControls, "evidence_derived_controls_exact", "control_proposal_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/proposal-lifecycle", []string{"human_review", "lifecycle", "proposal"}, proposalBoundary && len(first.Proposal.Lifecycle) == 3, "simulated_human_review_required", "proposal_lifecycle_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/digital-twin", []string{"digital_twin", "replay", "routing"}, first.Proposal.Simulation.ReusedDirectly && first.Proposal.Simulation.ContractVersion == governancesim.ContractVersion && first.Proposal.Simulation.RuntimeMutation == governancesim.RuntimeMutationNone, "b8_evaluator_reused", "digital_twin_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/replay", []string{"conflict", "replay"}, acceptedErr == nil && duplicateErr == nil && accepted.ReplayStatus == ReplayAccepted && duplicate.ReplayStatus == ReplayDuplicateNoop, "duplicate_noop", firstErrorCode(acceptedErr, duplicateErr)),
		conformanceCase(fixture.value.FixtureID+"/privacy", []string{"privacy"}, !containsRestrictedContent(first), "metadata_only_report", "restricted_content_exposed"),
		conformanceCase(fixture.value.FixtureID+"/runtime-boundary", []string{"runtime_boundary"}, runtimeBoundary, "runtime_mutation_none", "runtime_boundary_mismatch"),
	}
}

func runNegativeCases(fixture loadedFixture) []ConformanceCase {
	body := mustMarshal(fixture.request)
	duplicate := bytes.Replace(
		body,
		[]byte(`"contract_version":`),
		[]byte(`"contract_version":"unirouter.risk-control-review.v1","contract_version":`),
		1,
	)
	_, duplicateErr := Evaluate(duplicate)
	secret := bytes.Replace(
		body,
		[]byte(`"review_id":`),
		[]byte(`"credential_value":"forbidden","review_id":`),
		1,
	)
	_, secretErr := Evaluate(secret)

	wrongTenant := cloneRequest(fixture.request)
	wrongTenant.TenantRef = "sha256:" + strings.Repeat("f", 64)
	_, tenantErr := Evaluate(mustMarshal(wrongTenant))
	wrongAudience := cloneRequest(fixture.request)
	wrongAudience.Audience = "wrong-audience"
	_, audienceErr := Evaluate(mustMarshal(wrongAudience))
	zeroEpoch := cloneRequest(fixture.request)
	zeroEpoch.RiskEpoch = "0"
	_, epochErr := Evaluate(mustMarshal(zeroEpoch))
	wrongTTL := cloneRequest(fixture.request)
	wrongTTL.AssertionTTLSeconds = "301"
	_, ttlErr := Evaluate(mustMarshal(wrongTTL))
	subMicro := cloneRequest(fixture.request)
	subMicro.EvaluatedAt = "2026-07-31T08:05:00.0000001Z"
	subMicro.AgentTrust.EvaluatedAt = subMicro.EvaluatedAt
	_, timeErr := Evaluate(mustMarshal(subMicro))
	bindingSource := cloneRequest(fixture.request)
	bindingSource.EvidenceBinding.PassportSourceHash = "sha256:" + strings.Repeat("0", 64)
	_, sourceErr := Evaluate(mustMarshal(bindingSource))
	bindingScope := cloneRequest(fixture.request)
	bindingScope.EvidenceBinding.PassportSourceScopeHash =
		"sha256:" + strings.Repeat("0", 64)
	_, scopeErr := Evaluate(mustMarshal(bindingScope))
	bindingSemantic := cloneRequest(fixture.request)
	bindingSemantic.EvidenceBinding.AgentTrustInputSemanticHash = "sha256:" + strings.Repeat("0", 64)
	_, semanticErr := Evaluate(mustMarshal(bindingSemantic))
	tamperedSignature := cloneRequest(fixture.request)
	tamperedSignature.AgentTrust.SignedReceipt.Signature =
		base64.StdEncoding.EncodeToString(make([]byte, 64))
	_, signatureErr := Evaluate(mustMarshal(tamperedSignature))
	tamperedSource := cloneRequest(fixture.request)
	rawSource, _ := base64.StdEncoding.DecodeString(
		tamperedSource.ProvenancePassport.SourceSnapshotBase64,
	)
	rawSource[len(rawSource)-2] ^= 1
	tamperedSource.ProvenancePassport.SourceSnapshotBase64 =
		base64.StdEncoding.EncodeToString(rawSource)
	_, passportErr := Evaluate(mustMarshal(tamperedSource))

	service := NewService(NewMemoryStore())
	_, acceptedErr := service.Apply(body)
	conflict := cloneRequest(fixture.request)
	conflict.RiskEpoch = "2"
	_, conflictErr := service.Apply(mustMarshal(conflict))

	return []ConformanceCase{
		conformanceCase("negative/duplicate-key", []string{"negative", "strict_json"}, errorCode(duplicateErr) == "duplicate_json_key", "duplicate_rejected", errorCode(duplicateErr)),
		conformanceCase("negative/secret-field", []string{"negative", "privacy"}, errorCode(secretErr) == "input_shape_invalid", "secret_field_rejected", errorCode(secretErr)),
		conformanceCase("negative/tenant-binding", []string{"negative", "tenant"}, errorCode(tenantErr) == "tenant_binding_mismatch", "cross_tenant_binding_rejected", errorCode(tenantErr)),
		conformanceCase("negative/audience", []string{"audience", "negative"}, errorCode(audienceErr) == "audience_invalid", "wrong_audience_rejected", errorCode(audienceErr)),
		conformanceCase("negative/risk-epoch", []string{"epoch", "negative"}, errorCode(epochErr) == "risk_epoch_invalid", "zero_epoch_rejected", errorCode(epochErr)),
		conformanceCase("negative/assertion-ttl", []string{"negative", "ttl"}, errorCode(ttlErr) == "assertion_ttl_unsupported", "unfrozen_ttl_rejected", errorCode(ttlErr)),
		conformanceCase("negative/submicrosecond-time", []string{"negative", "precision", "time"}, errorCode(timeErr) == "evaluated_at_invalid", "submicrosecond_rejected", errorCode(timeErr)),
		conformanceCase("negative/source-hash-binding", []string{"binding", "negative", "source"}, errorCode(sourceErr) == "passport_source_hash_mismatch", "source_hash_tamper_rejected", errorCode(sourceErr)),
		conformanceCase("negative/source-scope-binding", []string{"binding", "negative", "source", "tenant"}, errorCode(scopeErr) == "passport_source_scope_hash_mismatch", "source_scope_tamper_rejected", errorCode(scopeErr)),
		conformanceCase("negative/semantic-binding", []string{"binding", "hash", "negative"}, errorCode(semanticErr) == "agent_trust_semantic_hash_mismatch", "semantic_hash_tamper_rejected", errorCode(semanticErr)),
		conformanceCase("negative/receipt-signature", []string{"negative", "receipt", "signature"}, errorCode(signatureErr) == "agent_trust_invalid", "receipt_signature_tamper_rejected", errorCode(signatureErr)),
		conformanceCase("negative/source-snapshot", []string{"negative", "provenance", "source"}, errorCode(passportErr) == "provenance_passport_invalid", "source_tamper_rejected", errorCode(passportErr)),
		conformanceCase("negative/replay-conflict", []string{"conflict", "negative", "replay"}, acceptedErr == nil && errorCode(conflictErr) == "review_identity_conflict", "semantic_conflict_rejected", firstErrorCode(acceptedErr, conflictErr)),
	}
}

func loadFixtures() ([]loadedFixture, error) {
	paths, err := fs.Glob(conformanceArtifacts, "artifacts/fixtures/*.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	agentRequestBytes, err := agenttrust.SampleRequestBytes()
	if err != nil {
		return nil, err
	}
	var agentRequest agenttrust.ReviewRequest
	if err := json.Unmarshal(agentRequestBytes, &agentRequest); err != nil {
		return nil, err
	}
	agentReport, err := agenttrust.Evaluate(mustMarshal(agentRequest))
	if err != nil {
		return nil, err
	}
	passportRequestBytes, err := provenancepassport.SampleRequestBytes()
	if err != nil {
		return nil, err
	}
	var passportRequest provenancepassport.Request
	if err := json.Unmarshal(passportRequestBytes, &passportRequest); err != nil {
		return nil, err
	}
	passportReport, err := provenancepassport.Evaluate(mustMarshal(passportRequest))
	if err != nil {
		return nil, err
	}
	sourceScope, err := extractSourceScope(passportRequest)
	if err != nil {
		return nil, err
	}
	authoritativeDependencies, err := dependencyArtifacts()
	if err != nil {
		return nil, err
	}

	result := make([]loadedFixture, 0, len(paths))
	for _, path := range paths {
		body, readErr := conformanceArtifacts.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		if strictErr := eventconnector.ValidateStrictJSON(
			body,
			eventconnector.StrictJSONOptions{RejectNull: true},
		); strictErr != nil {
			return nil, fmt.Errorf("%s: %s", path, strictErr.Code)
		}
		var fixture ConformanceFixture
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&fixture); err != nil {
			return nil, err
		}
		if fixture.SchemaVersion != FixtureSchemaVersion ||
			!portableIDPattern.MatchString(fixture.FixtureID) {
			return nil, fmt.Errorf("%s: fixture identity invalid", path)
		}
		if !sameArtifactSet(fixture.Dependencies, authoritativeDependencies) {
			return nil, fmt.Errorf("%s: dependency identity mismatch", path)
		}
		seed := fixture.RequestSeed
		request := ReviewRequest{
			ContractVersion:     seed.ContractVersion,
			ReviewID:            seed.ReviewID,
			EvaluatedAt:         seed.EvaluatedAt,
			TenantRef:           agentRequest.TenantRef,
			Audience:            seed.Audience,
			PolicyVersion:       seed.PolicyVersion,
			RiskEpoch:           seed.RiskEpoch,
			AssertionTTLSeconds: seed.AssertionTTLSeconds,
			AgentTrust:          agentRequest,
			ProvenancePassport:  passportRequest,
			EvidenceBinding: EvidenceBinding{
				ProvenanceRef:                agentRequest.ProvenanceRef,
				AgentTrustInputSemanticHash:  agentReport.InputSemanticHash,
				AgentTrustReceiptPayloadHash: agentReport.ReceiptVerification.PayloadHash,
				PassportInputSemanticHash:    passportReport.InputSemanticHash,
				PassportSourceHash:           passportReport.Source.SourceHash,
				PassportEnvelopeHash:         passportReport.Source.EnvelopeHash,
				PassportSourceScopeHash:      sourceScope.ScopeHash,
			},
		}
		result = append(result, loadedFixture{
			identity: ArtifactIdentity{
				Path:   strings.TrimPrefix(path, "artifacts/"),
				SHA256: hash(body),
			},
			value:   fixture,
			request: request,
		})
	}
	return result, nil
}

func dependencyArtifacts() ([]ArtifactIdentity, error) {
	result := make([]ArtifactIdentity, 0, 9)
	appendArtifacts := func(
		schemaPath string,
		schemaHash string,
		fixtures []ArtifactIdentity,
		goldenPath string,
		goldenHash string,
	) {
		result = append(result, ArtifactIdentity{Path: schemaPath, SHA256: schemaHash})
		result = append(result, fixtures...)
		result = append(result, ArtifactIdentity{Path: goldenPath, SHA256: goldenHash})
	}

	governanceSchema, governanceFixtures, governanceGolden, err :=
		governancesim.ArtifactDigests()
	if err != nil {
		return nil, err
	}
	governanceConverted := convertArtifacts(governanceFixtures)
	appendArtifacts(
		dependencyPath("internal/governancesim/artifacts/", governanceSchema.Path),
		governanceSchema.SHA256,
		prefixArtifacts("internal/governancesim/artifacts/", governanceConverted),
		dependencyPath("internal/governancesim/artifacts/", governanceGolden.Path),
		governanceGolden.SHA256,
	)

	agentSchema, agentFixtures, agentGolden, err := agenttrust.ArtifactDigests()
	if err != nil {
		return nil, err
	}
	appendArtifacts(
		dependencyPath("internal/agenttrust/artifacts/", agentSchema.Path),
		agentSchema.SHA256,
		prefixArtifacts("internal/agenttrust/artifacts/", convertArtifacts(agentFixtures)),
		dependencyPath("internal/agenttrust/artifacts/", agentGolden.Path),
		agentGolden.SHA256,
	)

	passportSchema, passportFixtures, passportGolden, err :=
		provenancepassport.ArtifactDigests()
	if err != nil {
		return nil, err
	}
	appendArtifacts(
		dependencyPath("internal/provenancepassport/artifacts/", passportSchema.Path),
		passportSchema.SHA256,
		prefixArtifacts("internal/provenancepassport/artifacts/", convertArtifacts(passportFixtures)),
		dependencyPath("internal/provenancepassport/artifacts/", passportGolden.Path),
		passportGolden.SHA256,
	)
	sort.Slice(result, func(left, right int) bool {
		return result[left].Path < result[right].Path
	})
	return result, nil
}

func convertArtifacts[T interface {
	agenttrust.ArtifactIdentity | governancesim.ArtifactIdentity | provenancepassport.ArtifactIdentity
}](values []T) []ArtifactIdentity {
	body, _ := json.Marshal(values)
	var result []ArtifactIdentity
	_ = json.Unmarshal(body, &result)
	return result
}

func prefixArtifacts(prefix string, values []ArtifactIdentity) []ArtifactIdentity {
	result := make([]ArtifactIdentity, len(values))
	for index, value := range values {
		result[index] = ArtifactIdentity{
			Path: dependencyPath(prefix, value.Path), SHA256: value.SHA256,
		}
	}
	return result
}

func dependencyPath(prefix, path string) string {
	return prefix + strings.TrimPrefix(path, "artifacts/")
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

func cloneRequest(request ReviewRequest) ReviewRequest {
	body, _ := json.Marshal(request)
	var clone ReviewRequest
	_ = json.Unmarshal(body, &clone)
	return clone
}

func conformanceCase(
	id string,
	classes []string,
	passed bool,
	success string,
	failure string,
) ConformanceCase {
	classes = sortedUnique(classes)
	result := success
	if !passed {
		result = failure
		if result == "" {
			result = "failed"
		}
	}
	return ConformanceCase{
		CaseID: id, CaseClasses: classes, Passed: passed, Result: result,
	}
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

func firstErrorCode(values ...error) string {
	for _, err := range values {
		if err != nil {
			return errorCode(err)
		}
	}
	return ""
}

func mustMarshal(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}
