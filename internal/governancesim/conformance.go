package governancesim

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
	FixtureSchemaVersion     = "unirouter.governance-simulation-fixture.v1"
	ConformanceReportVersion = "unirouter.governance-simulation-conformance-report.v1"

	SchemaArtifactPath = "artifacts/governance-simulation.v1.schema.json"
	GoldenReportPath   = "artifacts/reports/core.v1.json"
)

//go:embed artifacts/governance-simulation.v1.schema.json artifacts/fixtures/*.json artifacts/reports/core.v1.json
var conformanceArtifacts embed.FS

type ArtifactIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type FixtureExpected struct {
	OverallRisk         string `json:"overall_risk"`
	BaselineCandidates  int    `json:"baseline_candidates"`
	CandidateCandidates int    `json:"candidate_candidates"`
	ReviewRequired      int    `json:"review_required"`
	RuntimeMutation     string `json:"runtime_mutation"`
}

type ConformanceFixture struct {
	SchemaVersion string            `json:"schema_version"`
	FixtureID     string            `json:"fixture_id"`
	Request       SimulationRequest `json:"request"`
	Expected      FixtureExpected   `json:"expected"`
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

func ArtifactDigests() (schema ArtifactIdentity, fixtures []ArtifactIdentity, report ArtifactIdentity, err error) {
	schema, err = artifactIdentity(SchemaArtifactPath)
	if err != nil {
		return ArtifactIdentity{}, nil, ArtifactIdentity{}, err
	}
	loaded, err := loadFixtures()
	if err != nil {
		return ArtifactIdentity{}, nil, ArtifactIdentity{}, err
	}
	fixtures = make([]ArtifactIdentity, 0, len(loaded))
	for _, fixture := range loaded {
		fixtures = append(fixtures, fixture.identity)
	}
	report, err = artifactIdentity(GoldenReportPath)
	return schema, fixtures, report, err
}

func GoldenReportBytes() ([]byte, error) {
	return conformanceArtifacts.ReadFile(GoldenReportPath)
}

// SampleRequestBytes returns the first immutable synthetic request without its
// fixture expectations. It is intended for offline tests and local examples.
func SampleRequestBytes() ([]byte, error) {
	fixtures, err := loadFixtures()
	if err != nil {
		return nil, err
	}
	if len(fixtures) == 0 {
		return nil, errors.New("governancesim: fixtures missing")
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
		return ConformanceReport{}, errors.New("governancesim: fixtures missing")
	}
	report := ConformanceReport{
		SchemaVersion:   ConformanceReportVersion,
		ContractVersion: ContractVersion,
		Schema:          schema,
		Fixtures:        make([]ArtifactIdentity, 0, len(fixtures)),
		Cases:           make([]ConformanceCase, 0, len(fixtures)*6+10),
	}
	for _, fixture := range fixtures {
		report.Fixtures = append(report.Fixtures, fixture.identity)
		report.Cases = append(report.Cases, runFixtureCases(fixture)...)
	}
	report.Cases = append(report.Cases, runReplayAndReleaseCases(fixtures[0])...)
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
	body, err := json.Marshal(fixture.value.Request)
	if err != nil {
		return []ConformanceCase{failedCase(fixture.value.FixtureID+"/encode", []string{"schema"}, "fixture_encoding_failed")}
	}
	first, firstErr := Evaluate(body)
	second, secondErr := Evaluate(body)
	firstBytes, firstMarshalErr := json.Marshal(first)
	secondBytes, secondMarshalErr := json.Marshal(second)
	expected := fixture.value.Expected
	currentPacks, catalogErr := CurrentProviderPackSnapshots()
	catalogMatch := catalogErr == nil && len(currentPacks) > 0 &&
		packSnapshotEqual(currentPacks, fixture.value.Request.Baseline.ProviderPacks[0])
	return []ConformanceCase{
		conformanceCase(fixture.value.FixtureID+"/evaluation", []string{"route", "policy", "provider_firewall"}, firstErr == nil, "evaluation_accepted", errorCode(firstErr)),
		conformanceCase(fixture.value.FixtureID+"/determinism", []string{"determinism", "hash"}, secondErr == nil && firstMarshalErr == nil && secondMarshalErr == nil && bytes.Equal(firstBytes, secondBytes), "report_bytes_stable", "report_bytes_drifted"),
		conformanceCase(fixture.value.FixtureID+"/route-outcome", []string{"route", "shared_kernel"}, firstErr == nil && len(first.Baseline.Candidates) == expected.BaselineCandidates && len(first.Candidate.Candidates) == expected.CandidateCandidates, "route_outcome_exact", "route_outcome_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/firewall", []string{"provider_firewall", "semver"}, firstErr == nil && first.OverallRisk == expected.OverallRisk && first.Summary.ReviewRequired == expected.ReviewRequired, "change_risk_exact", "change_risk_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/runtime-boundary", []string{"privacy", "runtime_boundary"}, firstErr == nil && first.RuntimeMutation == expected.RuntimeMutation && !first.Release.RuntimePromotionAuthorized, "runtime_mutation_none", "runtime_boundary_failed"),
		conformanceCase(fixture.value.FixtureID+"/current-pack-identity", []string{"provider_firewall", "artifact_identity"}, catalogMatch, "baseline_pack_matches_catalog", "baseline_pack_identity_drift"),
	}
}

func runReplayAndReleaseCases(fixture loadedFixture) []ConformanceCase {
	service := NewService(NewMemoryStore())
	firstBody, _ := json.Marshal(fixture.value.Request)
	first, firstErr := service.Apply(firstBody)
	pretty, _ := json.MarshalIndent(fixture.value.Request, "", "  ")
	duplicate, duplicateErr := service.Apply(pretty)
	conflict := cloneRequest(fixture.value.Request)
	conflict.SelectionKey = "conflict-selection-key"
	_, conflictErr := service.Apply(mustMarshal(conflict))

	second := advanceRequest(fixture.value.Request, "sim-b8-conformance-002", StageValidated, StageShadow)
	_, secondErr := service.Apply(mustMarshal(second))
	third := advanceRequest(second, "sim-b8-conformance-003", StageShadow, StageCanary)
	_, thirdErr := service.Apply(mustMarshal(third))
	rollback := cloneRequest(third)
	rollback.SimulationID = "sim-b8-conformance-004"
	rollback.Release.Revision = "4"
	rollback.Release.ParentRevision = "3"
	rollback.Release.Action = ActionRollback
	rollback.Release.FromStage = StageCanary
	rollback.Release.ToStage = StageShadow
	rollback.Release.RollbackToRevision = "2"
	rollbackReport, rollbackErr := service.Apply(mustMarshal(rollback))
	stale := advanceRequest(third, "sim-b8-conformance-005", StageCanary, StageExternalGate)
	stale.Release.Revision = "5"
	stale.Release.ParentRevision = "4"
	_, staleErr := service.Apply(mustMarshal(stale))

	blockedService := NewService(NewMemoryStore())
	blocked := cloneRequest(fixture.value.Request)
	blocked.SimulationID = "sim-b8-conformance-blocked"
	for index := range blocked.Candidate.RouteSnapshot.Channels {
		blocked.Candidate.RouteSnapshot.Channels[index].Status = "disabled"
	}
	blockedReport, blockedErr := blockedService.Apply(mustMarshal(blocked))
	afterBlocked := cloneRequest(fixture.value.Request)
	afterBlocked.SimulationID = "sim-b8-conformance-after-blocked"
	_, afterBlockedErr := blockedService.Apply(mustMarshal(afterBlocked))
	return []ConformanceCase{
		conformanceCase("replay/accepted", []string{"replay"}, firstErr == nil && first.ReplayStatus == ReplayAccepted, "accepted", errorCode(firstErr)),
		conformanceCase("replay/duplicate-noop", []string{"replay", "canonicalization"}, duplicateErr == nil && duplicate.ReplayStatus == ReplayDuplicateNoop && duplicate.InputPayloadHash == first.InputPayloadHash, "duplicate_noop", errorCode(duplicateErr)),
		conformanceCase("replay/conflict", []string{"replay", "conflict"}, errors.Is(conflictErr, ErrConflict) && errorCode(conflictErr) == "simulation_identity_conflict", "identity_conflict", errorCode(conflictErr)),
		conformanceCase("release/progression", []string{"lifecycle", "cas"}, secondErr == nil && thirdErr == nil, "draft_validated_shadow_canary", firstErrorCode(secondErr, thirdErr)),
		conformanceCase("release/rollback", []string{"lifecycle", "rollback", "audit"}, rollbackErr == nil && rollbackReport.Release.SimulatedStage == StageShadow && rollbackReport.RuntimeMutation == RuntimeMutationNone, "rollback_simulated", errorCode(rollbackErr)),
		conformanceCase("release/stale-cas", []string{"lifecycle", "conflict"}, errors.Is(staleErr, ErrConflict) && errorCode(staleErr) == "release_compare_and_swap_conflict", "stale_revision_rejected", errorCode(staleErr)),
		conformanceCase("release/breaking-no-advance", []string{"breaking", "lifecycle", "runtime_boundary"}, blockedErr == nil && blockedReport.Release.Decision == "blocked_breaking_change" && afterBlockedErr == nil, "blocked_change_did_not_advance", firstErrorCode(blockedErr, afterBlockedErr)),
	}
}

func runNegativeCases(fixture loadedFixture) []ConformanceCase {
	body := mustMarshal(fixture.value.Request)
	duplicate := bytes.Replace(body, []byte(`"contract_version":`), []byte(`"contract_version":"unirouter.governance-simulation.v1","contract_version":`), 1)
	_, duplicateErr := Evaluate(duplicate)
	unknown := bytes.Replace(body, []byte(`"simulation_id":`), []byte(`"credential":"forbidden","simulation_id":`), 1)
	_, unknownErr := Evaluate(unknown)
	subMicro := cloneRequest(fixture.value.Request)
	subMicro.CreatedAt = "2026-07-31T08:00:00.0000001Z"
	_, subMicroErr := Evaluate(mustMarshal(subMicro))
	exhausted := cloneRequest(fixture.value.Request)
	for index := range exhausted.Candidate.RouteSnapshot.Channels {
		exhausted.Candidate.RouteSnapshot.Channels[index].Status = "disabled"
	}
	exhaustedReport, exhaustedErr := Evaluate(mustMarshal(exhausted))
	return []ConformanceCase{
		conformanceCase("negative/duplicate-key", []string{"strict_json", "negative"}, errorCode(duplicateErr) == "duplicate_json_key", "duplicate_rejected", errorCode(duplicateErr)),
		conformanceCase("negative/unknown-field", []string{"schema", "privacy", "negative"}, errorCode(unknownErr) == "input_shape_invalid", "credential_field_rejected", errorCode(unknownErr)),
		conformanceCase("negative/submicrosecond-time", []string{"time", "precision", "negative"}, errorCode(subMicroErr) == "created_at_invalid", "submicrosecond_rejected", errorCode(subMicroErr)),
		conformanceCase("negative/route-exhaustion", []string{"route", "breaking", "negative"}, exhaustedErr == nil && exhaustedReport.OverallRisk == RiskBreaking && exhaustedReport.Release.Decision == "blocked_breaking_change", "breaking_change_blocked", errorCode(exhaustedErr)),
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
		if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
			return nil, fmt.Errorf("%s: %s", path, strictErr.Code)
		}
		var fixture ConformanceFixture
		if err := json.Unmarshal(body, &fixture); err != nil {
			return nil, err
		}
		if fixture.SchemaVersion != FixtureSchemaVersion || !validPortableID(fixture.FixtureID, 160) {
			return nil, fmt.Errorf("%s: invalid fixture identity", path)
		}
		if err := validateRequest(fixture.Request); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
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

func packSnapshotEqual(current []ProviderPackSnapshot, expected ProviderPackSnapshot) bool {
	for _, pack := range current {
		if pack.PackID == expected.PackID {
			left, _ := json.Marshal(pack)
			right, _ := json.Marshal(expected)
			return bytes.Equal(left, right)
		}
	}
	return false
}

func conformanceCase(id string, classes []string, passed bool, success, failure string) ConformanceCase {
	sort.Strings(classes)
	result := success
	if !passed {
		result = failure
		if result == "" {
			result = "failed"
		}
	}
	return ConformanceCase{CaseID: id, CaseClasses: classes, Passed: passed, Result: result}
}

func failedCase(id string, classes []string, result string) ConformanceCase {
	return conformanceCase(id, classes, false, "", result)
}

func errorCode(err error) string {
	var contractErr *ContractError
	if errors.As(err, &contractErr) {
		return contractErr.Code
	}
	if err != nil {
		return "unexpected_error"
	}
	return ""
}

func firstErrorCode(errors ...error) string {
	for _, err := range errors {
		if err != nil {
			return errorCode(err)
		}
	}
	return ""
}

func cloneRequest(request SimulationRequest) SimulationRequest {
	body, _ := json.Marshal(request)
	var cloned SimulationRequest
	_ = json.Unmarshal(body, &cloned)
	return cloned
}

func advanceRequest(previous SimulationRequest, simulationID, from, to string) SimulationRequest {
	next := cloneRequest(previous)
	next.SimulationID = simulationID
	next.Release.Revision = nextRevision(previous.Release.Revision)
	next.Release.ParentRevision = previous.Release.Revision
	next.Release.Action = ActionAdvance
	next.Release.FromStage = from
	next.Release.ToStage = to
	next.Release.RollbackToRevision = ""
	return next
}

func mustMarshal(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}
