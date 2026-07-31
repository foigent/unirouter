package provenancepassport

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

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	SchemaArtifactPath = "artifacts/provenance-passport.v1.schema.json"
	GoldenReportPath   = "artifacts/reports/core.v1.json"
)

//go:embed artifacts/provenance-passport.v1.schema.json artifacts/fixtures/*.json artifacts/reports/core.v1.json
var conformanceArtifacts embed.FS

type ArtifactIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type FixtureExpected struct {
	SourceHash             string `json:"source_hash"`
	EnvelopeHash           string `json:"envelope_hash"`
	IdempotencyKey         string `json:"idempotency_key"`
	DirectFields           int    `json:"direct_fields"`
	DerivedFields          int    `json:"derived_fields"`
	DeclaredFields         int    `json:"declared_fields"`
	MissingFields          int    `json:"missing_fields"`
	CaptureGaps            int    `json:"capture_gaps"`
	SecondConnectorCases   int    `json:"second_connector_cases"`
	SecondConnectorClasses int    `json:"second_connector_classes"`
	RuntimeMutation        string `json:"runtime_mutation"`
}

type ConformanceFixture struct {
	SchemaVersion string          `json:"schema_version"`
	FixtureID     string          `json:"fixture_id"`
	Request       Request         `json:"request"`
	Expected      FixtureExpected `json:"expected"`
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
		return nil, errors.New("provenancepassport: fixtures missing")
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
		return ConformanceReport{}, errors.New("provenancepassport: fixtures missing")
	}
	report := ConformanceReport{
		SchemaVersion:   ConformanceReportVersion,
		ContractVersion: ContractVersion,
		Schema:          schema,
		Fixtures:        make([]ArtifactIdentity, 0, len(fixtures)),
		Cases:           make([]ConformanceCase, 0, len(fixtures)*24),
	}
	for _, fixture := range fixtures {
		report.Fixtures = append(report.Fixtures, fixture.identity)
		report.Cases = append(report.Cases, runFixtureCases(fixture)...)
	}
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

	changed := fixture.value.Request
	changed.SourceInstance = "unirouter-development-conflict"
	_, conflictErr := service.Apply(mustMarshal(changed))

	duplicateJSON := bytes.Replace(
		body,
		[]byte(`"contract_version":`),
		[]byte(`"contract_version":"unirouter.provenance-passport.v1","contract_version":`),
		1,
	)
	_, duplicateJSONErr := Evaluate(duplicateJSON)
	unknownField := bytes.Replace(
		body,
		[]byte(`"passport_id":`),
		[]byte(`"cursor":"forbidden","passport_id":`),
		1,
	)
	_, unknownFieldErr := Evaluate(unknownField)
	badBase64 := fixture.value.Request
	badBase64.SourceSnapshotBase64 += "="
	_, badBase64Err := Evaluate(mustMarshal(badBase64))
	badRevision := fixture.value.Request
	badRevision.SourceRevision = "2"
	_, badRevisionErr := Evaluate(mustMarshal(badRevision))
	badProjection := fixture.value.Request
	badProjection.OpenInferenceEvidence = "sha256:" + strings.Repeat("0", 64)
	_, badProjectionErr := Evaluate(mustMarshal(badProjection))

	missingGap := fixture.value.Request
	missingGap.SourceSnapshotBase64 = mutateSnapshot(fixture.value.Request.SourceSnapshotBase64, func(source map[string]any) {
		gaps := source["capture_gaps"].([]any)
		source["capture_gaps"] = gaps[:3]
	})
	_, missingGapErr := Evaluate(mustMarshal(missingGap))

	rawContent := fixture.value.Request
	rawContent.SourceSnapshotBase64 = mutateSnapshot(fixture.value.Request.SourceSnapshotBase64, func(source map[string]any) {
		source["raw_prompt"] = "must-fail"
	})
	_, rawContentErr := Evaluate(mustMarshal(rawContent))

	gapClaims := make([]string, 0, len(first.CaptureGaps))
	gapProvenanceOK := true
	for _, gap := range first.CaptureGaps {
		gapClaims = append(gapClaims, gap.AffectedClaim)
		if gap.FieldProvenance["affected_claim"] != "direct" ||
			gap.FieldProvenance["gap_id"] != "derived" ||
			gap.FieldProvenance["gap_type"] != "declared" {
			gapProvenanceOK = false
		}
	}
	sort.Strings(gapClaims)
	expectedClaims := []string{
		"call_log",
		"client_ip_presence",
		"request_trace",
		"upstream_error_presence",
	}
	fieldCountsOK := len(first.FieldGroups.Direct) == fixture.value.Expected.DirectFields &&
		len(first.FieldGroups.Derived) == fixture.value.Expected.DerivedFields &&
		len(first.FieldGroups.Declared) == fixture.value.Expected.DeclaredFields &&
		len(first.FieldGroups.Missing) == fixture.value.Expected.MissingFields
	compatibilityOK := first.Compatibility.ExistingDTOUnchanged &&
		first.Compatibility.SourceBytesUnchanged &&
		first.Compatibility.EventIdentityUnchanged &&
		first.Compatibility.FeedWireUnchanged &&
		first.Compatibility.PageBehaviorUnchanged &&
		first.Compatibility.CursorBehaviorUnchanged &&
		first.Compatibility.CredentialUnchanged
	secondConnectorOK := first.SecondConnector.ConnectorID == LiteLLMConnectorID &&
		first.SecondConnector.ManifestSHA256 == LiteLLMManifestDigest &&
		first.SecondConnector.SourceMappingSHA256 == LiteLLMMappingDigest &&
		first.SecondConnector.CompatibilitySHA256 == LiteLLMCompatDigest &&
		first.SecondConnector.ConformanceContractSHA256 == LiteLLMContractDigest &&
		first.SecondConnector.VectorBundleSHA256 == LiteLLMVectorDigest

	return []ConformanceCase{
		conformanceCase(fixture.value.FixtureID+"/determinism", []string{"canonicalization", "determinism"}, firstErr == nil && secondErr == nil && bytes.Equal(firstBytes, secondBytes), "passport_bytes_stable", firstErrorCode(firstErr, secondErr)),
		conformanceCase(fixture.value.FixtureID+"/source-bytes", []string{"source_bytes", "hash"}, first.Source.ExactBytesPreserved && first.Source.SourceHash == fixture.value.Expected.SourceHash, "source_bytes_hash_exact", "source_bytes_hash_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/event-identity", []string{"identity", "hash"}, first.Source.EnvelopeHash == fixture.value.Expected.EnvelopeHash && first.Source.IdempotencyKey == fixture.value.Expected.IdempotencyKey, "reference_identity_exact", "reference_identity_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/field-groups", []string{"provenance", "classification"}, fieldCountsOK, "direct_derived_declared_missing_exact", "field_group_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/capture-gaps", []string{"capture_gap", "missing"}, len(first.CaptureGaps) == fixture.value.Expected.CaptureGaps && equalStrings(gapClaims, expectedClaims), "four_fixed_gaps_exact", "capture_gap_set_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/gap-provenance", []string{"capture_gap", "provenance"}, gapProvenanceOK, "gap_field_provenance_exact", "gap_provenance_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/compatibility", []string{"compatibility", "feed", "cursor"}, compatibilityOK, "feed_dto_identity_page_cursor_unchanged", "compatibility_invariant_failed"),
		conformanceCase(fixture.value.FixtureID+"/reference-connector", []string{"connector", "reference"}, first.ReferenceConnector.RuntimeRegistered && first.ReferenceConnector.ProductRuntimeOwned && first.ReferenceConnector.ConformanceCases == 30 && first.ReferenceConnector.ConformanceClasses == 14, "reference_connector_runtime_baseline", "reference_connector_evidence_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/second-connector-identity", []string{"connector", "heterogeneous", "manifest"}, secondConnectorOK, "litellm_authority_artifacts_exact", "second_connector_identity_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/second-connector-conformance", []string{"connector", "conformance", "cross_language"}, first.SecondConnector.Status == "pass" && first.SecondConnector.ConformanceCases == fixture.value.Expected.SecondConnectorCases && first.SecondConnector.ConformanceClasses == fixture.value.Expected.SecondConnectorClasses && len(first.SecondConnector.IndependentReports) == 2, "go_typescript_shared_conformance_pass", "second_connector_conformance_mismatch"),
		conformanceCase(fixture.value.FixtureID+"/connector-isolation", []string{"connector", "isolation", "runtime_boundary"}, !first.SecondConnector.RuntimeRegistered && !first.SecondConnector.ProductRuntimeOwned && !first.SecondConnector.CredentialOwned && !first.SecondConnector.CheckpointOwned && !first.SecondConnector.CoreVendorBranchRequired, "second_connector_conformance_only", "second_connector_runtime_boundary_failed"),
		conformanceCase(fixture.value.FixtureID+"/openinference-observer", []string{"openinference", "provenance"}, first.OpenInference.GoldenFixtureSHA256 == OpenInferenceGoldenDigest && !first.OpenInference.CreatesSourceTruth && !first.OpenInference.IsConnectorRuntime && !first.OpenInference.RawContentIncluded, "metadata_observer_not_source_connector", "openinference_boundary_failed"),
		conformanceCase(fixture.value.FixtureID+"/privacy", []string{"privacy", "raw_content"}, !containsRestrictedContent(first) && !first.Source.RawPayloadPresent && first.Source.PayloadCaptureMode == "metadata_only", "metadata_only_no_raw_content", "restricted_content_exposed"),
		conformanceCase(fixture.value.FixtureID+"/duplicate-noop", []string{"replay", "idempotency"}, acceptedErr == nil && duplicateErr == nil && accepted.ReplayStatus == ReplayAccepted && duplicate.ReplayStatus == ReplayDuplicateNoop, "duplicate_noop", firstErrorCode(acceptedErr, duplicateErr)),
		conformanceCase(fixture.value.FixtureID+"/identity-conflict", []string{"replay", "conflict"}, errors.Is(conflictErr, ErrConflict), "same_identity_different_semantics_conflict", errorCode(conflictErr)),
		conformanceCase(fixture.value.FixtureID+"/duplicate-key-negative", []string{"strict_json", "negative"}, errorCode(duplicateJSONErr) == "duplicate_json_key", "duplicate_key_rejected", errorCode(duplicateJSONErr)),
		conformanceCase(fixture.value.FixtureID+"/delivery-field-negative", []string{"delivery_state", "privacy", "negative"}, errorCode(unknownFieldErr) == "input_shape_invalid", "cursor_field_rejected", errorCode(unknownFieldErr)),
		conformanceCase(fixture.value.FixtureID+"/base64-negative", []string{"source_bytes", "negative"}, errorCode(badBase64Err) == "source_snapshot_base64_invalid", "noncanonical_base64_rejected", errorCode(badBase64Err)),
		conformanceCase(fixture.value.FixtureID+"/gap-negative", []string{"capture_gap", "negative"}, errorCode(missingGapErr) == "source_snapshot_invalid", "missing_fixed_gap_rejected", errorCode(missingGapErr)),
		conformanceCase(fixture.value.FixtureID+"/raw-content-negative", []string{"privacy", "negative"}, errorCode(rawContentErr) == "source_snapshot_invalid", "raw_prompt_rejected", errorCode(rawContentErr)),
		conformanceCase(fixture.value.FixtureID+"/revision-negative", []string{"revision", "negative"}, errorCode(badRevisionErr) == "source_revision_unsupported", "revision_mismatch_rejected", errorCode(badRevisionErr)),
		conformanceCase(fixture.value.FixtureID+"/projection-pin-negative", []string{"openinference", "digest", "negative"}, errorCode(badProjectionErr) == "openinference_evidence_mismatch", "projection_evidence_mismatch_rejected", errorCode(badProjectionErr)),
		conformanceCase(fixture.value.FixtureID+"/runtime-boundary", []string{"runtime_boundary"}, len(first.RuntimeBoundary.ProductRegistryConnectorIDs) == 1 && !first.RuntimeBoundary.SecondConnectorRegistered && !first.RuntimeBoundary.SecondConnectorRuntimeOwned && !first.RuntimeBoundary.SynchronousDependency && first.RuntimeBoundary.RuntimeMutation == fixture.value.Expected.RuntimeMutation, "runtime_mutation_none", "runtime_boundary_failed"),
	}
}

func mutateSnapshot(encoded string, mutate func(map[string]any)) string {
	sourceBytes, _ := base64.StdEncoding.DecodeString(encoded)
	decoder := json.NewDecoder(bytes.NewReader(sourceBytes))
	decoder.UseNumber()
	var source map[string]any
	_ = decoder.Decode(&source)
	mutate(source)
	changed, _ := json.Marshal(source)
	return base64.StdEncoding.EncodeToString(changed)
}

func conformanceCase(
	caseID string,
	classes []string,
	passed bool,
	success string,
	failure string,
) ConformanceCase {
	sort.Strings(classes)
	classes = uniqueStrings(classes)
	result := success
	if !passed {
		result = failure
	}
	return ConformanceCase{
		CaseID:      caseID,
		CaseClasses: classes,
		Passed:      passed,
		Result:      result,
	}
}

func loadFixtures() ([]loadedFixture, error) {
	entries, err := fs.Glob(conformanceArtifacts, "artifacts/fixtures/*.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	loaded := make([]loadedFixture, 0, len(entries))
	for _, path := range entries {
		body, readErr := conformanceArtifacts.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
			return nil, fmt.Errorf("%s: %w", path, strictErr)
		}
		var fixture ConformanceFixture
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if decodeErr := decoder.Decode(&fixture); decodeErr != nil {
			return nil, fmt.Errorf("%s: %w", path, decodeErr)
		}
		if fixture.SchemaVersion != FixtureSchemaVersion ||
			fixture.FixtureID == "" ||
			fixture.Expected.RuntimeMutation != RuntimeMutationNone {
			return nil, fmt.Errorf("%s: fixture contract invalid", path)
		}
		loaded = append(loaded, loadedFixture{
			identity: ArtifactIdentity{Path: path, SHA256: eventconnector.SHA256Reference(body)},
			value:    fixture,
		})
	}
	return loaded, nil
}

func artifactIdentity(path string) (ArtifactIdentity, error) {
	body, err := conformanceArtifacts.ReadFile(path)
	if err != nil {
		return ArtifactIdentity{}, err
	}
	return ArtifactIdentity{Path: path, SHA256: eventconnector.SHA256Reference(body)}, nil
}

func mustMarshal(value any) []byte {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return body
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	var contractErr *ContractError
	if errors.As(err, &contractErr) {
		return contractErr.Code
	}
	return err.Error()
}

func firstErrorCode(errs ...error) string {
	for _, err := range errs {
		if err != nil {
			return errorCode(err)
		}
	}
	return ""
}

func equalStrings(left, right []string) bool {
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

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	output := values[:0]
	for _, value := range values {
		if len(output) == 0 || output[len(output)-1] != value {
			output = append(output, value)
		}
	}
	return output
}
