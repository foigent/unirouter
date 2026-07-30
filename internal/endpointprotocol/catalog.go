package endpointprotocol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	ManifestSchemaVersion = "unirouter.endpoint-profile-manifest.v1"
	FixtureSchemaVersion  = "unirouter.endpoint-conformance-fixture.v1"
	ReportSchemaVersion   = "unirouter.endpoint-conformance-report.v1"
)

//go:embed artifacts/endpoint-profile.v1.schema.json artifacts/manifests/*.json artifacts/fixtures/*.json artifacts/reports/*.json
var artifactFS embed.FS

var (
	artifactIDPattern   = regexp.MustCompile(`^[a-z0-9.-]+$`)
	artifactPathPattern = regexp.MustCompile(`^fixtures/[a-z0-9.-]+\.json$`)
	artifactHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	versionPattern      = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	errorCodePattern    = regexp.MustCompile(`^[a-z0-9_]+$`)
)

type FixtureRef struct {
	FixtureID string `json:"fixture_id"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
}

type Manifest struct {
	SchemaVersion    string       `json:"schema_version"`
	ProfileID        string       `json:"profile_id"`
	ProfileVersion   string       `json:"profile_version"`
	Provider         string       `json:"provider"`
	RuntimeState     string       `json:"runtime_state"`
	ConformanceLevel string       `json:"conformance_level"`
	NetworkVerified  bool         `json:"network_verified"`
	Descriptor       Descriptor   `json:"descriptor"`
	Fixtures         []FixtureRef `json:"fixtures"`
}

type ArtifactProfile struct {
	Manifest       Manifest `json:"manifest"`
	ManifestSHA256 string   `json:"manifest_sha256"`
	SchemaSHA256   string   `json:"schema_sha256"`
}

type fixtureDocument struct {
	SchemaVersion     string                `json:"schema_version"`
	FixtureID         string                `json:"fixture_id"`
	ProfileID         string                `json:"profile_id"`
	Request           fixtureRequest        `json:"request"`
	Response          fixtureResponse       `json:"response"`
	NegativeRequests  []fixtureNegativeCase `json:"negative_requests"`
	NegativeResponses []fixtureNegativeCase `json:"negative_responses"`
	StatusCases       []fixtureStatusCase   `json:"status_cases"`
}

type fixtureRequest struct {
	BaseURL                 string `json:"base_url"`
	RequestedModel          string `json:"requested_model"`
	MappedModel             string `json:"mapped_model"`
	RequestID               string `json:"request_id"`
	Body                    string `json:"body"`
	ExpectedMethod          string `json:"expected_method"`
	ExpectedURL             string `json:"expected_url"`
	ExpectedRequestIDHeader string `json:"expected_request_id_header"`
	ExpectedBody            string `json:"expected_body"`
}

type fixtureResponse struct {
	StatusCode      int     `json:"status_code"`
	Body            string  `json:"body"`
	ExpectedOutcome Outcome `json:"expected_outcome"`
}

type fixtureNegativeCase struct {
	CaseID       string `json:"case_id"`
	Body         string `json:"body"`
	ExpectedCode string `json:"expected_code"`
}

type fixtureStatusCase struct {
	CaseID              string `json:"case_id"`
	StatusCode          int    `json:"status_code"`
	ExpectedFailureKind string `json:"expected_failure_kind"`
	ExpectedRetryable   bool   `json:"expected_retryable"`
}

type ConformanceCase struct {
	CaseID  string `json:"case_id"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

type ConformanceFixtureIdentity struct {
	FixtureID string `json:"fixture_id"`
	SHA256    string `json:"sha256"`
}

type ConformanceProfileResult struct {
	ProfileID      string                       `json:"profile_id"`
	ProfileVersion string                       `json:"profile_version"`
	RuntimeState   string                       `json:"runtime_state"`
	ManifestSHA256 string                       `json:"manifest_sha256"`
	Fixtures       []ConformanceFixtureIdentity `json:"fixtures"`
	Cases          []ConformanceCase            `json:"cases"`
}

type ConformanceSummary struct {
	Profiles int `json:"profiles"`
	Cases    int `json:"cases"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
}

type ConformanceReport struct {
	SchemaVersion string                     `json:"schema_version"`
	SchemaSHA256  string                     `json:"schema_sha256"`
	Profiles      []ConformanceProfileResult `json:"profiles"`
	Summary       ConformanceSummary         `json:"summary"`
}

func Catalog() ([]ArtifactProfile, error) {
	schema, err := artifactFS.ReadFile("artifacts/endpoint-profile.v1.schema.json")
	if err != nil {
		return nil, fmt.Errorf("read endpoint profile schema: %w", err)
	}
	if strictErr := eventconnector.ValidateStrictJSON(schema, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return nil, fmt.Errorf("endpoint profile schema is not strict JSON: %s", strictErr.Code)
	}
	schemaDigest := digest(schema)
	manifestPaths, err := fs.Glob(artifactFS, "artifacts/manifests/*.json")
	if err != nil {
		return nil, fmt.Errorf("list endpoint manifests: %w", err)
	}
	sort.Strings(manifestPaths)
	registry := NewRegistry()
	seen := make(map[string]struct{}, len(manifestPaths))
	profiles := make([]ArtifactProfile, 0, len(manifestPaths))
	for _, manifestPath := range manifestPaths {
		raw, readErr := artifactFS.ReadFile(manifestPath)
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", manifestPath, readErr)
		}
		var manifest Manifest
		if decodeErr := decodeStrictArtifact(raw, &manifest); decodeErr != nil {
			return nil, fmt.Errorf("decode %s: %w", manifestPath, decodeErr)
		}
		if _, duplicate := seen[manifest.ProfileID]; duplicate {
			return nil, fmt.Errorf("duplicate profile %q", manifest.ProfileID)
		}
		seen[manifest.ProfileID] = struct{}{}
		profile, getErr := registry.Get(manifest.ProfileID)
		if getErr != nil {
			return nil, fmt.Errorf("manifest %s: %w", manifest.ProfileID, getErr)
		}
		if validateErr := validateManifest(manifest, profile); validateErr != nil {
			return nil, fmt.Errorf("validate %s: %w", manifestPath, validateErr)
		}
		for _, fixture := range manifest.Fixtures {
			if _, fixtureErr := validateFixture(manifest, fixture); fixtureErr != nil {
				return nil, fixtureErr
			}
		}
		profiles = append(profiles, ArtifactProfile{
			Manifest:       manifest,
			ManifestSHA256: digest(raw),
			SchemaSHA256:   schemaDigest,
		})
	}
	if len(profiles) != len(registry.Descriptors()) {
		return nil, fmt.Errorf("artifact profiles=%d registry profiles=%d", len(profiles), len(registry.Descriptors()))
	}
	sort.Slice(profiles, func(left, right int) bool {
		return profiles[left].Manifest.ProfileID < profiles[right].Manifest.ProfileID
	})
	return profiles, nil
}

func SchemaDigest() (string, error) {
	raw, err := artifactFS.ReadFile("artifacts/endpoint-profile.v1.schema.json")
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}

func GoldenReportDigest() (string, error) {
	raw, err := artifactFS.ReadFile("artifacts/reports/core.v1.json")
	if err != nil {
		return "", err
	}
	if strictErr := eventconnector.ValidateStrictJSON(raw, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return "", fmt.Errorf("golden endpoint report is not strict JSON: %s", strictErr.Code)
	}
	return digest(raw), nil
}

func RunOffline(ctx context.Context, registry *Registry) (ConformanceReport, error) {
	if registry == nil {
		registry = NewRegistry()
	}
	profiles, err := Catalog()
	if err != nil {
		return ConformanceReport{}, err
	}
	schemaDigest, err := SchemaDigest()
	if err != nil {
		return ConformanceReport{}, err
	}
	report := ConformanceReport{
		SchemaVersion: ReportSchemaVersion,
		SchemaSHA256:  schemaDigest,
		Profiles:      make([]ConformanceProfileResult, 0, len(profiles)),
	}
	for _, artifact := range profiles {
		profile, getErr := registry.Get(artifact.Manifest.ProfileID)
		if getErr != nil {
			return ConformanceReport{}, getErr
		}
		result := ConformanceProfileResult{
			ProfileID:      artifact.Manifest.ProfileID,
			ProfileVersion: artifact.Manifest.ProfileVersion,
			RuntimeState:   artifact.Manifest.RuntimeState,
			ManifestSHA256: artifact.ManifestSHA256,
			Fixtures:       make([]ConformanceFixtureIdentity, 0, len(artifact.Manifest.Fixtures)),
		}
		for _, fixtureRef := range artifact.Manifest.Fixtures {
			fixture, loadErr := loadFixture(fixtureRef.Path)
			if loadErr != nil {
				return ConformanceReport{}, loadErr
			}
			result.Fixtures = append(result.Fixtures, ConformanceFixtureIdentity{
				FixtureID: fixtureRef.FixtureID,
				SHA256:    fixtureRef.SHA256,
			})
			result.Cases = append(result.Cases, runFixture(ctx, profile, fixture)...)
		}
		report.Profiles = append(report.Profiles, result)
	}
	report.Summary.Profiles = len(report.Profiles)
	for _, profile := range report.Profiles {
		for _, result := range profile.Cases {
			report.Summary.Cases++
			if result.Passed {
				report.Summary.Passed++
			} else {
				report.Summary.Failed++
			}
		}
	}
	return report, nil
}

func validateManifest(manifest Manifest, profile Profile) error {
	descriptor := profile.Descriptor()
	if manifest.SchemaVersion != ManifestSchemaVersion ||
		manifest.ProfileID != descriptor.ProfileID ||
		manifest.ProfileVersion != descriptor.ProfileVersion ||
		manifest.Provider != descriptor.Provider ||
		manifest.RuntimeState != descriptor.RuntimeState {
		return errors.New("manifest identity does not match registered descriptor")
	}
	if !versionPattern.MatchString(manifest.ProfileVersion) ||
		manifest.ConformanceLevel != "fixture_verified" ||
		manifest.NetworkVerified {
		return errors.New("manifest must be local fixture_verified evidence")
	}
	if manifest.RuntimeState != RuntimeContractOnly && manifest.RuntimeState != RuntimeDecisionGate {
		return errors.New("manifest runtime state is not allowed")
	}
	if !reflect.DeepEqual(manifest.Descriptor, descriptor) {
		return errors.New("manifest descriptor differs from executable profile")
	}
	if len(manifest.Fixtures) == 0 {
		return errors.New("manifest has no fixtures")
	}
	seen := make(map[string]struct{}, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		if !artifactIDPattern.MatchString(fixture.FixtureID) ||
			path.Clean(fixture.Path) != fixture.Path ||
			!artifactPathPattern.MatchString(fixture.Path) ||
			!artifactHashPattern.MatchString(fixture.SHA256) {
			return fmt.Errorf("invalid fixture reference %+v", fixture)
		}
		if _, duplicate := seen[fixture.FixtureID]; duplicate {
			return fmt.Errorf("duplicate fixture %q", fixture.FixtureID)
		}
		seen[fixture.FixtureID] = struct{}{}
	}
	return nil
}

func validateFixture(manifest Manifest, ref FixtureRef) (fixtureDocument, error) {
	raw, err := artifactFS.ReadFile("artifacts/" + ref.Path)
	if err != nil {
		return fixtureDocument{}, fmt.Errorf("read fixture %s: %w", ref.Path, err)
	}
	if got := digest(raw); got != ref.SHA256 {
		return fixtureDocument{}, fmt.Errorf("fixture %s digest=%s want %s", ref.Path, got, ref.SHA256)
	}
	var fixture fixtureDocument
	if err := decodeStrictArtifact(raw, &fixture); err != nil {
		return fixtureDocument{}, fmt.Errorf("decode fixture %s: %w", ref.Path, err)
	}
	if fixture.SchemaVersion != FixtureSchemaVersion ||
		fixture.FixtureID != ref.FixtureID ||
		fixture.ProfileID != manifest.ProfileID {
		return fixtureDocument{}, fmt.Errorf("fixture %s identity mismatch", ref.Path)
	}
	if fixture.Request.ExpectedMethod != http.MethodPost ||
		fixture.Request.BaseURL == "" ||
		fixture.Request.RequestedModel == "" ||
		fixture.Request.MappedModel == "" ||
		fixture.Request.ExpectedURL == "" {
		return fixtureDocument{}, fmt.Errorf("fixture %s request evidence is incomplete", ref.Path)
	}
	for name, body := range map[string]string{
		"request":       fixture.Request.Body,
		"expected_body": fixture.Request.ExpectedBody,
		"response":      fixture.Response.Body,
	} {
		if strictErr := eventconnector.ValidateStrictJSON([]byte(body), eventconnector.StrictJSONOptions{}); strictErr != nil {
			return fixtureDocument{}, fmt.Errorf("fixture %s %s is not strict JSON: %s", ref.Path, name, strictErr.Code)
		}
	}
	if fixture.Response.StatusCode < 200 || fixture.Response.StatusCode >= 300 ||
		fixture.Response.ExpectedOutcome.ProfileID != manifest.ProfileID ||
		fixture.Response.ExpectedOutcome.ContractVersion != ContractVersion {
		return fixtureDocument{}, fmt.Errorf("fixture %s expected outcome is inconsistent", ref.Path)
	}
	for _, unit := range fixture.Response.ExpectedOutcome.Units {
		if unit.Authority != "provider_reported" ||
			unit.Finality != "final" ||
			unit.ChargeState != ChargeObservedOnly ||
			!canonicalUintPattern.MatchString(unit.Quantity) {
			return fixtureDocument{}, fmt.Errorf("fixture %s unit evidence is unsafe", ref.Path)
		}
	}
	if len(fixture.NegativeRequests) == 0 ||
		len(fixture.NegativeResponses) == 0 ||
		len(fixture.StatusCases) == 0 {
		return fixtureDocument{}, fmt.Errorf("fixture %s lacks negative coverage", ref.Path)
	}
	seenCases := make(map[string]struct{})
	for class, cases := range map[string][]fixtureNegativeCase{
		"negative-request":  fixture.NegativeRequests,
		"negative-response": fixture.NegativeResponses,
	} {
		for _, testCase := range cases {
			caseKey := class + "/" + testCase.CaseID
			if !artifactIDPattern.MatchString(testCase.CaseID) ||
				!errorCodePattern.MatchString(testCase.ExpectedCode) ||
				testCase.Body == "" {
				return fixtureDocument{}, fmt.Errorf("fixture %s invalid %s", ref.Path, caseKey)
			}
			if _, duplicate := seenCases[caseKey]; duplicate {
				return fixtureDocument{}, fmt.Errorf("fixture %s duplicate case %s", ref.Path, caseKey)
			}
			seenCases[caseKey] = struct{}{}
		}
	}
	for _, statusCase := range fixture.StatusCases {
		caseKey := "status/" + statusCase.CaseID
		if !artifactIDPattern.MatchString(statusCase.CaseID) ||
			statusCase.StatusCode < 400 ||
			statusCase.ExpectedFailureKind == "" {
			return fixtureDocument{}, fmt.Errorf("fixture %s invalid %s", ref.Path, caseKey)
		}
		if _, duplicate := seenCases[caseKey]; duplicate {
			return fixtureDocument{}, fmt.Errorf("fixture %s duplicate case %s", ref.Path, caseKey)
		}
		seenCases[caseKey] = struct{}{}
	}
	return fixture, nil
}

func runFixture(ctx context.Context, profile Profile, fixture fixtureDocument) []ConformanceCase {
	results := make([]ConformanceCase, 0, 2+len(fixture.NegativeRequests)+len(fixture.NegativeResponses)+len(fixture.StatusCases))
	input := BuildInput{
		BaseURL:        fixture.Request.BaseURL,
		APIKey:         "fixture-secret",
		RequestID:      fixture.Request.RequestID,
		RequestedModel: fixture.Request.RequestedModel,
		MappedModel:    fixture.Request.MappedModel,
		Body:           []byte(fixture.Request.Body),
	}
	request, binding, err := profile.BuildRequest(ctx, input)
	results = append(results, conformanceResult(fixture.FixtureID+"/request", validateBuiltRequest(request, err, fixture.Request)))

	outcome, responseErr := profile.ParseResponse(ctx, binding, fixture.Response.StatusCode, []byte(fixture.Response.Body))
	if responseErr == nil && !reflect.DeepEqual(outcome, fixture.Response.ExpectedOutcome) {
		responseErr = fmt.Errorf("outcome differs from fixture")
	}
	results = append(results, conformanceResult(fixture.FixtureID+"/response", responseErr))

	for _, negative := range fixture.NegativeRequests {
		negativeInput := input
		negativeInput.Body = []byte(negative.Body)
		_, _, caseErr := profile.BuildRequest(ctx, negativeInput)
		results = append(results, conformanceResult(
			fixture.FixtureID+"/negative-request/"+negative.CaseID,
			expectProtocolCode(caseErr, negative.ExpectedCode, negative.Body),
		))
	}
	for _, negative := range fixture.NegativeResponses {
		_, caseErr := profile.ParseResponse(ctx, binding, http.StatusOK, []byte(negative.Body))
		results = append(results, conformanceResult(
			fixture.FixtureID+"/negative-response/"+negative.CaseID,
			expectProtocolCode(caseErr, negative.ExpectedCode, negative.Body),
		))
	}
	for _, statusCase := range fixture.StatusCases {
		failureKind, retryable := profile.ClassifyHTTPStatus(statusCase.StatusCode)
		var caseErr error
		if failureKind != statusCase.ExpectedFailureKind || retryable != statusCase.ExpectedRetryable {
			caseErr = fmt.Errorf(
				"status %d classified %s/%t want %s/%t",
				statusCase.StatusCode,
				failureKind,
				retryable,
				statusCase.ExpectedFailureKind,
				statusCase.ExpectedRetryable,
			)
		}
		results = append(results, conformanceResult(
			fixture.FixtureID+"/status/"+statusCase.CaseID,
			caseErr,
		))
	}
	return results
}

func validateBuiltRequest(request *http.Request, buildErr error, expected fixtureRequest) error {
	if buildErr != nil {
		return buildErr
	}
	if request == nil || request.Method != expected.ExpectedMethod || request.URL.String() != expected.ExpectedURL {
		return errors.New("request method or URL mismatch")
	}
	if request.Header.Get("Authorization") != "Bearer fixture-secret" ||
		request.Header.Get("Content-Type") != "application/json" ||
		request.Header.Get("X-Client-Request-Id") != expected.ExpectedRequestIDHeader {
		return errors.New("request headers mismatch")
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return err
	}
	if !bytes.Equal(body, []byte(expected.ExpectedBody)) {
		return errors.New("request body mismatch")
	}
	return nil
}

func expectProtocolCode(err error, expectedCode, sensitiveBody string) error {
	if err == nil {
		return errors.New("case unexpectedly succeeded")
	}
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Code != expectedCode {
		return fmt.Errorf("protocol code mismatch")
	}
	if strings.Contains(err.Error(), sensitiveBody) || strings.Contains(err.Error(), "fixture-secret") {
		return errors.New("safe error leaked fixture data")
	}
	return nil
}

func conformanceResult(caseID string, err error) ConformanceCase {
	if err != nil {
		return ConformanceCase{CaseID: caseID, Passed: false, Message: err.Error()}
	}
	return ConformanceCase{CaseID: caseID, Passed: true}
}

func loadFixture(fixturePath string) (fixtureDocument, error) {
	raw, err := artifactFS.ReadFile("artifacts/" + fixturePath)
	if err != nil {
		return fixtureDocument{}, err
	}
	var fixture fixtureDocument
	if err := decodeStrictArtifact(raw, &fixture); err != nil {
		return fixtureDocument{}, err
	}
	return fixture, nil
}

func decodeStrictArtifact(raw []byte, target any) error {
	if strictErr := eventconnector.ValidateStrictJSON(raw, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return fmt.Errorf("strict JSON: %s", strictErr.Code)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON content")
	}
	return nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
