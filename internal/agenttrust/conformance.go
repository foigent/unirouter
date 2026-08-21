package agenttrust

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

	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/governancesim"
)

const (
	SchemaArtifactPath = "artifacts/agent-trust-review.v1.schema.json"
	GoldenReportPath   = "artifacts/reports/core.v1.json"
)

//go:embed artifacts/agent-trust-review.v1.schema.json artifacts/fixtures/*.json artifacts/reports/core.v1.json
var conformanceArtifacts embed.FS

type ArtifactIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type FixtureExpected struct {
	Decision         string `json:"decision"`
	ActiveNodes      int    `json:"active_nodes"`
	RevokedNodes     int    `json:"revoked_nodes"`
	AttenuationDiffs int    `json:"attenuation_diffs"`
	OverallRisk      string `json:"overall_risk"`
	RuntimeMutation  string `json:"runtime_mutation"`
}

type ConformanceFixture struct {
	SchemaVersion     string           `json:"schema_version"`
	FixtureID         string           `json:"fixture_id"`
	GovernanceFixture ArtifactIdentity `json:"governance_fixture"`
	Request           ReviewRequest    `json:"request"`
	Expected          FixtureExpected  `json:"expected"`
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
		return nil, errors.New("agenttrust: fixtures missing")
	}
	return json.Marshal(fixtures[0].value.Request)
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
		return ConformanceReport{}, errors.New("agenttrust: fixtures missing")
	}
	dependencies := []ArtifactIdentity{fixtures[0].value.GovernanceFixture}
	report := ConformanceReport{
		SchemaVersion:   ConformanceReportVersion,
		ContractVersion: ContractVersion,
		Schema:          schema,
		Fixtures:        make([]ArtifactIdentity, 0, len(fixtures)),
		Dependencies:    dependencies,
		Cases:           make([]ConformanceCase, 0, 24),
	}
	for _, fixture := range fixtures {
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
	body := mustMarshal(fixture.value.Request)
	first, firstErr := Evaluate(body)
	second, secondErr := Evaluate(body)
	firstBytes, _ := json.Marshal(first)
	secondBytes, _ := json.Marshal(second)
	service := NewService(NewMemoryStore())
	accepted, acceptedErr := service.Apply(body)
	pretty, _ := json.MarshalIndent(fixture.value.Request, "", "  ")
	duplicate, duplicateErr := service.Apply(pretty)

	active, revoked := 0, 0
	for _, node := range first.EffectiveAuthorityTree {
		switch node.State {
		case NodeActive:
			active++
		case NodeRevoked:
			revoked++
		}
	}
	budgetOK := len(first.AncestorBudgets) == 3 &&
		len(first.AncestorBudgets[2].AncestorBudgetRefs) == 3 &&
		first.AncestorBudgets[2].EffectiveLimits.CostBudgetCents == 1000
	attenuationOK := len(first.AttenuationDiffs) == fixture.value.Expected.AttenuationDiffs &&
		slicesContains(first.AttenuationDiffs[0].RemovedModels, "text-embedding-3-large") &&
		slicesContains(first.AttenuationDiffs[0].RemovedEndpoints, "/v1/embeddings")
	receiptJSON, _ := json.Marshal(first.SignedReceipt)
	noSecretFields := !bytes.Contains(receiptJSON, []byte(`"private_key"`)) &&
		!bytes.Contains(receiptJSON, []byte(`"root_secret"`)) &&
		!bytes.Contains(receiptJSON, []byte(`"capability_token"`))

	return []ConformanceCase{
		conformanceCase(fixture.value.FixtureID+"/determinism", []string{"canonicalization", "determinism", "hash"}, firstErr == nil && secondErr == nil && bytes.Equal(firstBytes, secondBytes), "report_bytes_stable", firstErrorCode(firstErr, secondErr)),
		conformanceCase(fixture.value.FixtureID+"/effective-tree", []string{"authority", "tree"}, active == fixture.value.Expected.ActiveNodes && revoked == fixture.value.Expected.RevokedNodes, "effective_authority_tree_exact", "authority_tree_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/attenuation", []string{"attenuation", "authority"}, attenuationOK, "child_authority_only_reduced", "attenuation_diff_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/ancestor-budget", []string{"budget", "lineage"}, budgetOK, "ancestor_budget_lineage_exact", "ancestor_budget_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/revocation", []string{"revocation", "ttl"}, len(first.RevocationTimeline) == 1 && first.EffectiveAuthorityTree[2].RevocationEventRef != "", "revocation_proof_exact", "revocation_proof_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/signed-receipt", []string{"receipt", "signature", "tenant"}, first.ReceiptVerification.CryptographicallyValid && first.ReceiptVerification.TenantBound && first.ReceiptVerification.AudienceBound && first.ReceiptVerification.DecisionBound && !first.ReceiptVerification.RuntimeTrusted, "signature_valid_fixture_trust_not_runtime", "receipt_verification_failed"),
		conformanceCase(fixture.value.FixtureID+"/key-boundary", []string{"privacy", "signature"}, noSecretFields, "public_evidence_only", "secret_field_exposed"),
		conformanceCase(fixture.value.FixtureID+"/digital-twin", []string{"digital_twin", "replay", "routing"}, first.DigitalTwin.OverallRisk == fixture.value.Expected.OverallRisk && !first.DigitalTwin.SynchronousDependency, "b8_evaluator_reused_off_path", "digital_twin_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/decision", []string{"decision", "shadow"}, first.Decision == fixture.value.Expected.Decision, "shadow_only_review", "decision_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/replay", []string{"replay"}, acceptedErr == nil && duplicateErr == nil && accepted.ReplayStatus == ReplayAccepted && duplicate.ReplayStatus == ReplayDuplicateNoop, "duplicate_noop", firstErrorCode(acceptedErr, duplicateErr)),
		conformanceCase(fixture.value.FixtureID+"/runtime-boundary", []string{"runtime_boundary"}, first.RuntimeMutation == fixture.value.Expected.RuntimeMutation && !first.RuntimeEnforcementAuthorized && !first.SynchronousRuntimeDependency, "runtime_mutation_none", "runtime_boundary_failed"),
	}
}

func runNegativeCases(fixture loadedFixture) []ConformanceCase {
	body := mustMarshal(fixture.value.Request)
	duplicate := bytes.Replace(body, []byte(`"contract_version":`), []byte(`"contract_version":"unirouter.agent-trust-review.v1","contract_version":`), 1)
	_, duplicateErr := Evaluate(duplicate)
	secretField := bytes.Replace(body, []byte(`"review_id":`), []byte(`"root_secret":"forbidden","review_id":`), 1)
	_, secretErr := Evaluate(secretField)

	widened := cloneRequest(fixture.value.Request)
	widened.Authority.Nodes[1].Limits.CostBudgetCents = widened.Authority.Nodes[0].Limits.CostBudgetCents + 1
	_, widenedErr := Evaluate(mustMarshal(widened))
	ttlWidened := cloneRequest(fixture.value.Request)
	ttlWidened.Authority.Nodes[1].ExpiresAt = "2026-07-31T13:00:00.000000Z"
	_, ttlErr := Evaluate(mustMarshal(ttlWidened))
	missingRevocation := cloneRequest(fixture.value.Request)
	missingRevocation.Authority.Revocations = []RevocationRecord{}
	_, missingRevocationErr := Evaluate(mustMarshal(missingRevocation))
	tamperedDecision := cloneRequest(fixture.value.Request)
	tamperedDecision.SignedReceipt.Payload.Decision = DecisionAllow
	_, tamperedDecisionErr := Evaluate(mustMarshal(tamperedDecision))
	tamperedKeyVersion := cloneRequest(fixture.value.Request)
	tamperedKeyVersion.SignedReceipt.KeyVersion = "2"
	_, tamperedKeyErr := Evaluate(mustMarshal(tamperedKeyVersion))
	tamperedSignature := cloneRequest(fixture.value.Request)
	tamperedSignature.SignedReceipt.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	_, tamperedSignatureErr := Evaluate(mustMarshal(tamperedSignature))
	wrongAudience := cloneRequest(fixture.value.Request)
	wrongAudience.Audience = "wrong-audience"
	_, audienceErr := Evaluate(mustMarshal(wrongAudience))
	subMicro := cloneRequest(fixture.value.Request)
	subMicro.EvaluatedAt = "2026-07-31T08:05:00.0000001Z"
	_, subMicroErr := Evaluate(mustMarshal(subMicro))
	invalidTwin := cloneRequest(fixture.value.Request)
	invalidTwin.DigitalTwin.EvaluationMode = "remote"
	_, invalidTwinErr := Evaluate(mustMarshal(invalidTwin))
	unboundNewOperation := cloneRequest(fixture.value.Request)
	setAuthorityEndpoints(&unboundNewOperation, "/v1/responses")
	_, unboundNewOperationErr := Evaluate(mustMarshal(unboundNewOperation))
	projectDefaultNewOperation := cloneRequest(unboundNewOperation)
	projectDefaultNewOperation.Authority.RootGrantBound = true
	projectDefaultNewOperation.Authority.RootGrantScopeMode = accessscope.ModeProjectDefault
	projectDefaultNewOperation.Authority.RootAllowedEndpoints = []string{"/v1/responses"}
	_, projectDefaultNewOperationErr := Evaluate(mustMarshal(projectDefaultNewOperation))
	wildcardNewOperation := cloneRequest(projectDefaultNewOperation)
	wildcardNewOperation.Authority.RootGrantScopeMode = accessscope.ModeExplicit
	wildcardNewOperation.Authority.RootAllowedEndpoints = []string{"*"}
	_, wildcardNewOperationErr := Evaluate(mustMarshal(wildcardNewOperation))

	return []ConformanceCase{
		conformanceCase("negative/duplicate-key", []string{"negative", "strict_json"}, errorCode(duplicateErr) == "duplicate_json_key", "duplicate_rejected", errorCode(duplicateErr)),
		conformanceCase("negative/root-secret-field", []string{"negative", "privacy"}, errorCode(secretErr) == "input_shape_invalid", "secret_field_rejected", errorCode(secretErr)),
		conformanceCase("negative/authority-widening", []string{"attenuation", "negative"}, errorCode(widenedErr) == "authority_attenuation_invalid", "widening_rejected", errorCode(widenedErr)),
		conformanceCase("negative/ttl-widening", []string{"negative", "ttl"}, errorCode(ttlErr) == "authority_ttl_widened", "ttl_widening_rejected", errorCode(ttlErr)),
		conformanceCase("negative/missing-revocation-proof", []string{"negative", "revocation"}, errorCode(missingRevocationErr) == "revocation_evidence_missing", "missing_revocation_rejected", errorCode(missingRevocationErr)),
		conformanceCase("negative/receipt-payload-tamper", []string{"negative", "receipt", "signature"}, errorCode(tamperedDecisionErr) == "receipt_payload_mismatch", "payload_tamper_rejected", errorCode(tamperedDecisionErr)),
		conformanceCase("negative/receipt-key-version-tamper", []string{"key_version", "negative", "signature"}, errorCode(tamperedKeyErr) == "receipt_signature_mismatch", "key_version_tamper_rejected", errorCode(tamperedKeyErr)),
		conformanceCase("negative/receipt-signature-tamper", []string{"negative", "signature"}, errorCode(tamperedSignatureErr) == "receipt_signature_mismatch", "signature_tamper_rejected", errorCode(tamperedSignatureErr)),
		conformanceCase("negative/audience", []string{"audience", "negative"}, errorCode(audienceErr) == "audience_invalid", "wrong_audience_rejected", errorCode(audienceErr)),
		conformanceCase("negative/submicrosecond-time", []string{"negative", "precision", "time"}, errorCode(subMicroErr) == "evaluated_at_invalid", "submicrosecond_rejected", errorCode(subMicroErr)),
		conformanceCase("negative/digital-twin-mode", []string{"digital_twin", "negative"}, errorCode(invalidTwinErr) == "digital_twin_invalid", "invalid_twin_rejected", errorCode(invalidTwinErr)),
		conformanceCase("negative/new-operation-unbound-root", []string{"authority", "negative", "scope"}, errorCode(unboundNewOperationErr) == "authority_root_scope_invalid", "unbound_root_rejected", errorCode(unboundNewOperationErr)),
		conformanceCase("negative/new-operation-project-default", []string{"authority", "negative", "scope"}, errorCode(projectDefaultNewOperationErr) == "authority_root_scope_invalid", "project_default_rejected", errorCode(projectDefaultNewOperationErr)),
		conformanceCase("negative/new-operation-wildcard", []string{"authority", "negative", "scope"}, errorCode(wildcardNewOperationErr) == "authority_root_scope_invalid", "wildcard_rejected", errorCode(wildcardNewOperationErr)),
	}
}

func setAuthorityEndpoints(request *ReviewRequest, endpoint string) {
	for index := range request.Authority.Nodes {
		request.Authority.Nodes[index].Limits.AllowedEndpoints = []string{endpoint}
	}
}

func loadFixtures() ([]loadedFixture, error) {
	paths, err := fs.Glob(conformanceArtifacts, "artifacts/fixtures/*.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	governanceRequestBytes, err := governancesim.SampleRequestBytes()
	if err != nil {
		return nil, err
	}
	var governanceRequest governancesim.SimulationRequest
	if err := json.Unmarshal(governanceRequestBytes, &governanceRequest); err != nil {
		return nil, err
	}
	_, governanceFixtures, _, err := governancesim.ArtifactDigests()
	if err != nil {
		return nil, err
	}
	result := make([]loadedFixture, 0, len(paths))
	for _, path := range paths {
		body, readErr := conformanceArtifacts.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
			return nil, fmt.Errorf("%s: %s", path, strictErr.Code)
		}
		var fixture ConformanceFixture
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&fixture); err != nil {
			return nil, err
		}
		if fixture.SchemaVersion != FixtureSchemaVersion || !portableIDPattern.MatchString(fixture.FixtureID) {
			return nil, fmt.Errorf("%s: invalid fixture identity", path)
		}
		dependencyFound := false
		for _, identity := range governanceFixtures {
			if identity.Path == fixture.GovernanceFixture.Path && identity.SHA256 == fixture.GovernanceFixture.SHA256 {
				dependencyFound = true
				break
			}
		}
		if !dependencyFound {
			return nil, fmt.Errorf("%s: governance fixture identity mismatch", path)
		}
		fixture.Request.DigitalTwin = governanceRequest
		result = append(result, loadedFixture{
			identity: ArtifactIdentity{Path: strings.TrimPrefix(path, "artifacts/"), SHA256: hash(body)},
			value:    fixture,
		})
	}
	return result, nil
}

func artifactIdentity(path string) (ArtifactIdentity, error) {
	body, err := conformanceArtifacts.ReadFile(path)
	if err != nil {
		return ArtifactIdentity{}, err
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return ArtifactIdentity{}, fmt.Errorf("%s: %s", path, strictErr.Code)
	}
	return ArtifactIdentity{Path: strings.TrimPrefix(path, "artifacts/"), SHA256: hash(body)}, nil
}

func cloneRequest(request ReviewRequest) ReviewRequest {
	body, _ := json.Marshal(request)
	var clone ReviewRequest
	_ = json.Unmarshal(body, &clone)
	return clone
}

func conformanceCase(id string, classes []string, passed bool, success, failure string) ConformanceCase {
	classes = sortedUnique(classes)
	result := success
	if !passed {
		result = failure
		if result == "" {
			result = "failed"
		}
	}
	return ConformanceCase{CaseID: id, CaseClasses: classes, Passed: passed, Result: result}
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

func slicesContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
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
