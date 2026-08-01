package providercogs

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	FixtureSchemaVersion     = "unirouter.provider-cogs-conformance-fixture.v1"
	ConformanceReportVersion = "unirouter.provider-cogs-conformance-report.v1"

	SchemaArtifactPath = "artifacts/provider-cogs-offline.v1.schema.json"
	GoldenReportPath   = "artifacts/reports/core.v1.json"
)

//go:embed artifacts/provider-cogs-offline.v1.schema.json artifacts/fixtures/*.json artifacts/reports/core.v1.json
var conformanceArtifacts embed.FS

type FixtureExpected struct {
	Pages                   int    `json:"pages"`
	UniqueSupplierLines     int    `json:"unique_supplier_lines"`
	DuplicateNoopLines      int    `json:"duplicate_noop_lines"`
	SourceAttempts          int    `json:"source_attempts"`
	ExactRequestMatches     int    `json:"exact_request_matches"`
	AggregateUnattributed   int    `json:"aggregate_unattributed"`
	SourceOnly              int    `json:"source_only"`
	SupplierEvidencePending int    `json:"supplier_evidence_pending"`
	SupplierTotalMinor      string `json:"supplier_total_minor"`
	MatchedSupplierMinor    string `json:"matched_supplier_minor"`
	UnattributedMinor       string `json:"unattributed_minor"`
	CustomerLedgerMutation  string `json:"customer_ledger_mutation"`
}

type ConformanceFixture struct {
	SchemaVersion string          `json:"schema_version"`
	FixtureID     string          `json:"fixture_id"`
	Request       ImportRequest   `json:"request"`
	Expected      FixtureExpected `json:"expected"`
}

type ArtifactIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ConformanceCase struct {
	CaseID      string   `json:"case_id"`
	CaseClasses []string `json:"case_classes"`
	Passed      bool     `json:"passed"`
	Result      string   `json:"result"`
}

type ConformanceReport struct {
	SchemaVersion         string             `json:"schema_version"`
	ContractVersion       string             `json:"contract_version"`
	SourceSnapshotVersion string             `json:"source_snapshot_version"`
	Schema                ArtifactIdentity   `json:"schema"`
	Fixtures              []ArtifactIdentity `json:"fixtures"`
	Cases                 []ConformanceCase  `json:"cases"`
	Summary               struct {
		Fixtures int `json:"fixtures"`
		Cases    int `json:"cases"`
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
	} `json:"summary"`
}

type loadedFixture struct {
	identity ArtifactIdentity
	value    ConformanceFixture
	body     []byte
}

func ArtifactDigests() (schema ArtifactIdentity, fixtures []ArtifactIdentity, report ArtifactIdentity, err error) {
	schema, err = identityFor(SchemaArtifactPath)
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
	report, err = identityFor(GoldenReportPath)
	if err != nil {
		return ArtifactIdentity{}, nil, ArtifactIdentity{}, err
	}
	return schema, fixtures, report, nil
}

func GoldenReportBytes() ([]byte, error) {
	return conformanceArtifacts.ReadFile(GoldenReportPath)
}

func RunConformance() (ConformanceReport, error) {
	schema, err := identityFor(SchemaArtifactPath)
	if err != nil {
		return ConformanceReport{}, err
	}
	fixtures, err := loadFixtures()
	if err != nil {
		return ConformanceReport{}, err
	}
	report := ConformanceReport{
		SchemaVersion:         ConformanceReportVersion,
		ContractVersion:       ContractVersion,
		SourceSnapshotVersion: SourceSnapshotVersion,
		Schema:                schema,
		Fixtures:              make([]ArtifactIdentity, 0, len(fixtures)),
		Cases:                 make([]ConformanceCase, 0, len(fixtures)*5+5),
	}
	for _, fixture := range fixtures {
		report.Fixtures = append(report.Fixtures, fixture.identity)
		report.Cases = append(report.Cases, runFixtureCases(fixture)...)
	}
	if len(fixtures) == 0 {
		return ConformanceReport{}, errors.New("providercogs: conformance fixtures missing")
	}
	report.Cases = append(report.Cases, runReplayCases(fixtures[0])...)
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
	prefix := fixture.value.FixtureID
	cases := make([]ConformanceCase, 0, 5)
	requestBody, err := json.Marshal(fixture.value.Request)
	cases = append(cases, conformanceCase(
		prefix+"/strict-input",
		[]string{"strict_json", "schema_shape"},
		err == nil,
		"strict_input_accepted",
		"fixture_encoding_failed",
	))
	if err != nil {
		return append(cases,
			failedCase(prefix+"/evaluation", []string{"financial_authority"}, "fixture_encoding_failed"),
			failedCase(prefix+"/summary", []string{"exact_decimal"}, "fixture_encoding_failed"),
			failedCase(prefix+"/attribution", []string{"attribution", "privacy"}, "fixture_encoding_failed"),
			failedCase(prefix+"/determinism", []string{"determinism"}, "fixture_encoding_failed"),
		)
	}
	first, evalErr := Evaluate(requestBody)
	cases = append(cases, conformanceCase(
		prefix+"/evaluation",
		[]string{"cursor", "gap", "financial_authority"},
		evalErr == nil,
		"evaluation_accepted",
		safeErrorCode(evalErr),
	))
	if evalErr != nil {
		return append(cases,
			failedCase(prefix+"/summary", []string{"exact_decimal"}, "evaluation_unavailable"),
			failedCase(prefix+"/attribution", []string{"attribution", "privacy"}, "evaluation_unavailable"),
			failedCase(prefix+"/determinism", []string{"determinism"}, "evaluation_unavailable"),
		)
	}
	cases = append(cases, conformanceCase(
		prefix+"/summary",
		[]string{"exact_decimal", "duplicate", "finality"},
		summaryMatches(first, fixture.value.Expected),
		"summary_exact",
		"summary_mismatch",
	))
	cases = append(cases, conformanceCase(
		prefix+"/attribution",
		[]string{"attribution", "privacy", "customer_ledger_separation"},
		attributionBoundaryHolds(first),
		"authority_separated",
		"authority_boundary_failed",
	))
	second, secondErr := Evaluate(requestBody)
	firstBytes, firstMarshalErr := json.Marshal(first)
	secondBytes, secondMarshalErr := json.Marshal(second)
	cases = append(cases, conformanceCase(
		prefix+"/determinism",
		[]string{"determinism", "hash"},
		secondErr == nil && firstMarshalErr == nil && secondMarshalErr == nil &&
			bytes.Equal(firstBytes, secondBytes),
		"report_bytes_stable",
		"report_bytes_drifted",
	))
	return cases
}

func runReplayCases(fixture loadedFixture) []ConformanceCase {
	body, err := json.Marshal(fixture.value.Request)
	if err != nil {
		return []ConformanceCase{
			failedCase("replay/accepted", []string{"replay"}, "fixture_encoding_failed"),
			failedCase("replay/duplicate-noop", []string{"replay", "duplicate"}, "fixture_encoding_failed"),
			failedCase("replay/semantic-noop", []string{"replay", "canonicalization"}, "fixture_encoding_failed"),
			failedCase("replay/conflict", []string{"replay", "conflict"}, "fixture_encoding_failed"),
			failedCase("negative/cursor", []string{"cursor", "negative"}, "fixture_encoding_failed"),
		}
	}
	service := NewService(NewMemoryStore())
	first, firstErr := service.Import(body)
	second, secondErr := service.Import(body)
	pretty, prettyErr := json.MarshalIndent(fixture.value.Request, "", "  ")
	semantic, semanticErr := service.Import(pretty)

	mutated := cloneImport(fixture.value.Request)
	mutated.Pages[0].Lines[0].AmountMinor = "121"
	if len(mutated.Pages) > 1 && len(mutated.Pages[1].Lines) > 0 &&
		mutated.Pages[1].Lines[0].LineID == mutated.Pages[0].Lines[0].LineID {
		mutated.Pages[1].Lines[0].AmountMinor = "121"
	}
	mutatedBody, mutatedMarshalErr := json.Marshal(mutated)
	_, conflictErr := service.Import(mutatedBody)

	brokenCursor := cloneImport(fixture.value.Request)
	if len(brokenCursor.Pages) > 1 {
		brokenCursor.Pages[1].CursorIn = "wrong"
	} else {
		brokenCursor.Pages[0].CursorOut = "unexpected"
	}
	brokenBody, brokenMarshalErr := json.Marshal(brokenCursor)
	_, cursorErr := Evaluate(brokenBody)

	return []ConformanceCase{
		conformanceCase(
			"replay/accepted",
			[]string{"replay"},
			firstErr == nil && first.ReplayStatus == ReplayAccepted,
			"accepted",
			safeErrorCode(firstErr),
		),
		conformanceCase(
			"replay/duplicate-noop",
			[]string{"duplicate", "replay"},
			secondErr == nil && second.ReplayStatus == ReplayDuplicateNoop,
			"duplicate_noop",
			safeErrorCode(secondErr),
		),
		conformanceCase(
			"replay/semantic-noop",
			[]string{"canonicalization", "replay"},
			prettyErr == nil && semanticErr == nil &&
				semantic.ReplayStatus == ReplayDuplicateNoop &&
				semantic.ImportPayloadHash == first.ImportPayloadHash,
			"semantic_duplicate_noop",
			safeErrorCode(semanticErr),
		),
		conformanceCase(
			"replay/conflict",
			[]string{"conflict", "replay"},
			mutatedMarshalErr == nil && errors.Is(conflictErr, ErrConflict),
			"identity_conflict",
			safeErrorCode(conflictErr),
		),
		conformanceCase(
			"negative/cursor",
			[]string{"cursor", "negative"},
			brokenMarshalErr == nil && errors.Is(cursorErr, ErrInvalid) &&
				oneOf(
					safeErrorCode(cursorErr),
					"page_cursor_chain_invalid",
					"final_page_cursor_invalid",
				),
			"cursor_rejected",
			safeErrorCode(cursorErr),
		),
	}
}

func conformanceCase(id string, classes []string, passed bool, success, failure string) ConformanceCase {
	sort.Strings(classes)
	result := failure
	if passed {
		result = success
	}
	return ConformanceCase{
		CaseID:      id,
		CaseClasses: classes,
		Passed:      passed,
		Result:      result,
	}
}

func failedCase(id string, classes []string, result string) ConformanceCase {
	return conformanceCase(id, classes, false, "", result)
}

func summaryMatches(report Report, expected FixtureExpected) bool {
	return report.Summary.Pages == expected.Pages &&
		report.Summary.UniqueSupplierLines == expected.UniqueSupplierLines &&
		report.Summary.DuplicateNoopLines == expected.DuplicateNoopLines &&
		report.Summary.SourceAttempts == expected.SourceAttempts &&
		report.Summary.ExactRequestMatches == expected.ExactRequestMatches &&
		report.Summary.AggregateUnattributed == expected.AggregateUnattributed &&
		report.Summary.SourceOnly == expected.SourceOnly &&
		report.Summary.SupplierEvidencePending == expected.SupplierEvidencePending &&
		report.Summary.SupplierTotalMinor == expected.SupplierTotalMinor &&
		report.Summary.MatchedSupplierMinor == expected.MatchedSupplierMinor &&
		report.Summary.UnattributedMinor == expected.UnattributedMinor &&
		report.CustomerLedgerMutation == expected.CustomerLedgerMutation
}

func attributionBoundaryHolds(report Report) bool {
	if report.CustomerLedgerMutation != CustomerLedgerMutationNone {
		return false
	}
	for _, match := range report.Matches {
		if match.CustomerDebtAction != "none" {
			return false
		}
		if match.Status == StatusAggregateUnattributed &&
			(match.RequestID != "" || match.AttemptID != "" || match.CustomerRevenue != nil) {
			return false
		}
	}
	return true
}

func cloneImport(request ImportRequest) ImportRequest {
	body, _ := json.Marshal(request)
	var clone ImportRequest
	_ = json.Unmarshal(body, &clone)
	return clone
}

func loadFixtures() ([]loadedFixture, error) {
	paths, err := fs.Glob(conformanceArtifacts, "artifacts/fixtures/*.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	fixtures := make([]loadedFixture, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		body, readErr := conformanceArtifacts.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
			return nil, fmt.Errorf("providercogs: fixture strict JSON %s: %w", path, strictErr)
		}
		var fixture ConformanceFixture
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if decodeErr := decoder.Decode(&fixture); decodeErr != nil {
			return nil, fmt.Errorf("providercogs: fixture shape %s: %w", path, decodeErr)
		}
		var extra any
		if decodeErr := decoder.Decode(&extra); !errors.Is(decodeErr, io.EOF) {
			return nil, fmt.Errorf("providercogs: fixture trailing JSON %s", path)
		}
		if fixture.SchemaVersion != FixtureSchemaVersion ||
			!validPortableID(fixture.FixtureID, 160) ||
			fixture.Expected.CustomerLedgerMutation != CustomerLedgerMutationNone {
			return nil, fmt.Errorf("providercogs: fixture identity invalid %s", path)
		}
		if _, duplicate := seen[fixture.FixtureID]; duplicate {
			return nil, fmt.Errorf("providercogs: duplicate fixture id %s", fixture.FixtureID)
		}
		seen[fixture.FixtureID] = struct{}{}
		fixtures = append(fixtures, loadedFixture{
			identity: ArtifactIdentity{Path: path, SHA256: hash(body)},
			value:    fixture,
			body:     body,
		})
	}
	return fixtures, nil
}

func identityFor(path string) (ArtifactIdentity, error) {
	body, err := conformanceArtifacts.ReadFile(path)
	if err != nil {
		return ArtifactIdentity{}, err
	}
	sum := sha256.Sum256(body)
	return ArtifactIdentity{
		Path:   path,
		SHA256: "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}
