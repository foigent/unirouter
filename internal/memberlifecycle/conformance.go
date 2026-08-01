package memberlifecycle

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
	SchemaArtifactPath = "artifacts/member-lifecycle.v1.schema.json"
	GoldenReportPath   = "artifacts/reports/core.v1.json"
)

//go:embed artifacts/member-lifecycle.v1.schema.json artifacts/fixtures/*.json artifacts/reports/core.v1.json
var conformanceArtifacts embed.FS

type ArtifactIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ConformanceFixture struct {
	SchemaVersion string  `json:"schema_version"`
	FixtureID     string  `json:"fixture_id"`
	Events        []Event `json:"events"`
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
	report, err := artifactIdentity(GoldenReportPath)
	return schema, fixtures, report, err
}

func GoldenReportBytes() ([]byte, error) {
	return conformanceArtifacts.ReadFile(GoldenReportPath)
}

func SampleEventsBytes() ([]byte, error) {
	fixtures, err := loadFixtures()
	if err != nil {
		return nil, err
	}
	if len(fixtures) == 0 {
		return nil, errors.New("memberlifecycle: fixtures missing")
	}
	return json.Marshal(fixtures[0].value.Events)
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
		return ConformanceReport{}, errors.New("memberlifecycle: fixtures missing")
	}
	report := ConformanceReport{
		SchemaVersion:   ConformanceReportVersion,
		ContractVersion: ContractVersion,
		Schema:          schema,
		Fixtures:        make([]ArtifactIdentity, 0, len(fixtures)),
		Cases:           make([]ConformanceCase, 0, 20),
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
	if len(fixture.value.Events) != 5 {
		return []ConformanceCase{failedCase(fixture.value.FixtureID+"/chain", []string{"fixture"}, "event_count_mismatch")}
	}
	service := NewService(NewMemoryStore())
	reports := make([]Report, 0, len(fixture.value.Events))
	var chainErr error
	for _, event := range fixture.value.Events {
		report, err := service.Apply(mustMarshal(event))
		if err != nil {
			chainErr = err
			break
		}
		reports = append(reports, report)
	}
	firstBody := mustMarshal(fixture.value.Events[0])
	firstA, firstAErr := Evaluate(firstBody)
	firstB, firstBErr := Evaluate(firstBody)
	firstABytes, _ := json.Marshal(firstA)
	firstBBytes, _ := json.Marshal(firstB)

	pretty, _ := json.MarshalIndent(fixture.value.Events[0], "", "  ")
	replayService := NewService(NewMemoryStore())
	accepted, acceptedErr := replayService.Apply(firstBody)
	duplicate, duplicateErr := replayService.Apply(pretty)
	conflict := cloneEvent(fixture.value.Events[0])
	conflict.SourceRef = redactedRef("different-source")
	_, conflictErr := replayService.Apply(mustMarshal(conflict))

	staleService := NewService(NewMemoryStore())
	_, staleCreateErr := staleService.Apply(firstBody)
	stale := cloneEvent(fixture.value.Events[1])
	stale.Before.Capabilities[0].Status = "disabled"
	_, staleErr := staleService.Apply(mustMarshal(stale))

	terminal := cloneEvent(fixture.value.Events[4])
	terminal.EventID = "member-event-after-terminal"
	terminal.Operation = OperationUpdate
	terminal.ParentRevision = "5"
	terminal.Revision = "6"
	terminal.Before = &fixture.value.Events[4].After
	terminal.After.Revision = "6"
	terminal.After.Status = StatusActive
	_, terminalErr := service.Apply(mustMarshal(terminal))

	updateOK := len(reports) == 5 && len(reports[1].Revocations) == 2 &&
		reports[1].Revocations[0].Kind == "capability" &&
		reports[1].Revocations[1].Kind == "membership"
	suspendOK := len(reports) == 5 && len(reports[2].EffectiveMemberships) == 0 &&
		len(reports[2].EffectiveCapabilities) == 0 &&
		reports[2].Controls.DenyAdminSessions &&
		reports[2].Controls.RevokeLiveAdminSessions
	reactivateOK := len(reports) == 5 && reports[3].StateStatus == StatusActive &&
		len(reports[3].EffectiveMemberships) == 1 &&
		len(reports[3].EffectiveCapabilities) == 2
	deprovisionOK := len(reports) == 5 && reports[4].StateStatus == StatusDeprovisioned &&
		reports[4].Controls.DisablePersonalKeysBeforeDelete &&
		reports[4].LocalProjection.MembershipStorage == "cascade_deleted"
	return []ConformanceCase{
		conformanceCase(fixture.value.FixtureID+"/chain", []string{"cas", "lifecycle"}, chainErr == nil && len(reports) == 5, "create_update_suspend_reactivate_deprovision", errorCode(chainErr)),
		conformanceCase(fixture.value.FixtureID+"/determinism", []string{"canonicalization", "determinism", "hash"}, firstAErr == nil && firstBErr == nil && bytes.Equal(firstABytes, firstBBytes), "report_bytes_stable", firstErrorCode(firstAErr, firstBErr)),
		conformanceCase(fixture.value.FixtureID+"/update-revocation", []string{"capability", "membership", "revocation"}, updateOK, "removed_assignments_revoked", "revocation_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/suspend", []string{"lifecycle", "revocation", "session"}, suspendOK, "suspended_access_zero", "suspend_boundary_failed"),
		conformanceCase(fixture.value.FixtureID+"/reactivate", []string{"lifecycle", "resume"}, reactivateOK, "suspended_subject_reactivated", "reactivation_failed"),
		conformanceCase(fixture.value.FixtureID+"/deprovision", []string{"capability", "lifecycle", "terminal"}, deprovisionOK, "deprovision_terminal_controls_exact", "deprovision_boundary_failed"),
		conformanceCase("replay/duplicate-noop", []string{"canonicalization", "replay"}, acceptedErr == nil && duplicateErr == nil && accepted.ReplayStatus == ReplayAccepted && duplicate.ReplayStatus == ReplayDuplicateNoop && accepted.InputSemanticHash == duplicate.InputSemanticHash, "duplicate_noop", firstErrorCode(acceptedErr, duplicateErr)),
		conformanceCase("replay/conflict", []string{"conflict", "replay"}, errors.Is(conflictErr, ErrConflict) && errorCode(conflictErr) == "event_identity_conflict", "event_identity_conflict", errorCode(conflictErr)),
		conformanceCase("lifecycle/stale-cas", []string{"cas", "conflict"}, staleCreateErr == nil && errors.Is(staleErr, ErrConflict), "stale_revision_rejected", firstErrorCode(staleCreateErr, staleErr)),
		conformanceCase("lifecycle/terminal", []string{"conflict", "terminal"}, errors.Is(terminalErr, ErrConflict) && errorCode(terminalErr) == "subject_deprovisioned", "deprovision_is_terminal", errorCode(terminalErr)),
		conformanceCase("boundary/runtime", []string{"privacy", "runtime_boundary", "vendor_gate"}, len(reports) == 5 && reports[4].RuntimeMutation == RuntimeMutationNone && !reports[4].VendorAdapterAuthorized, "runtime_mutation_none_vendor_unbound", "runtime_boundary_failed"),
	}
}

func runNegativeCases(fixture loadedFixture) []ConformanceCase {
	first := fixture.value.Events[0]
	body := mustMarshal(first)
	duplicate := bytes.Replace(body, []byte(`"contract_version":`), []byte(`"contract_version":"unirouter.member-lifecycle.v1","contract_version":`), 1)
	_, duplicateErr := Evaluate(duplicate)
	vendor := bytes.Replace(body, []byte(`"event_id":`), []byte(`"scim_vendor":"forbidden","event_id":`), 1)
	_, vendorErr := Evaluate(vendor)
	missing := bytes.Replace(body, []byte(`"source_ref":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",`), nil, 1)
	_, missingErr := Evaluate(missing)
	subMicro := cloneEvent(first)
	subMicro.OccurredAt = "2026-07-31T09:00:00.0000001Z"
	_, subMicroErr := Evaluate(mustMarshal(subMicro))

	maximum := cloneEvent(fixture.value.Events[1])
	maximum.ParentRevision = "9223372036854775806"
	maximum.Revision = "9223372036854775807"
	maximum.Before.Revision = maximum.ParentRevision
	maximum.After.Revision = maximum.Revision
	_, maximumErr := Evaluate(mustMarshal(maximum))
	overflow := cloneEvent(maximum)
	overflow.ParentRevision = "9223372036854775807"
	overflow.Revision = "9223372036854775808"
	overflow.Before.Revision = overflow.ParentRevision
	overflow.After.Revision = overflow.Revision
	_, overflowErr := Evaluate(mustMarshal(overflow))
	return []ConformanceCase{
		conformanceCase("negative/duplicate-key", []string{"negative", "strict_json"}, errorCode(duplicateErr) == "duplicate_json_key", "duplicate_rejected", errorCode(duplicateErr)),
		conformanceCase("negative/vendor-field", []string{"negative", "privacy", "vendor_gate"}, errorCode(vendorErr) == "input_shape_invalid", "vendor_field_rejected", errorCode(vendorErr)),
		conformanceCase("negative/missing-required", []string{"negative", "schema"}, errorCode(missingErr) == "input_shape_invalid", "missing_field_rejected", errorCode(missingErr)),
		conformanceCase("negative/submicrosecond-time", []string{"negative", "precision", "time"}, errorCode(subMicroErr) == "occurred_at_invalid", "submicrosecond_rejected", errorCode(subMicroErr)),
		conformanceCase("boundary/int64-maximum", []string{"decimal", "precision"}, maximumErr == nil, "int64_maximum_accepted", errorCode(maximumErr)),
		conformanceCase("negative/int64-overflow", []string{"decimal", "negative", "precision"}, errorCode(overflowErr) == "revision_invalid", "int64_overflow_rejected", errorCode(overflowErr)),
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
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&fixture); err != nil {
			return nil, err
		}
		if fixture.SchemaVersion != FixtureSchemaVersion || !portableIDPattern.MatchString(fixture.FixtureID) || len(fixture.Events) == 0 {
			return nil, fmt.Errorf("%s: invalid fixture identity", path)
		}
		for _, event := range fixture.Events {
			if err := validateEvent(event); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
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

func failedCase(id string, classes []string, result string) ConformanceCase {
	return conformanceCase(id, classes, false, "", result)
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
