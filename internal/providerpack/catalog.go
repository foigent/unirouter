package providerpack

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
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/models"
)

const (
	SchemaVersion        = "unirouter.provider-pack.v1"
	FixtureSchemaVersion = "unirouter.provider-conformance-fixture.v1"
	ReportSchemaVersion  = "unirouter.provider-conformance-report.v1"
)

//go:embed artifacts/provider-pack.v1.schema.json artifacts/manifests/*.json artifacts/fixtures/*.json artifacts/reports/*.json
var artifactFS embed.FS

var (
	packIDPattern      = regexp.MustCompile(`^unirouter\.[a-z0-9.-]+$`)
	versionPattern     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	fixturePattern     = regexp.MustCompile(`^[a-z0-9.-]+$`)
	fixturePathPattern = regexp.MustCompile(`^fixtures/[a-z0-9.-]+\.json$`)
	digestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type AuthProfile struct {
	Type       string `json:"type"`
	WireHeader string `json:"wire_header"`
}

type UsageProfile struct {
	AuthorityWhenComplete string   `json:"authority_when_complete"`
	MissingBehavior       string   `json:"missing_behavior"`
	CacheClasses          []string `json:"cache_classes"`
}

type ErrorProfile struct {
	Classifier      string `json:"classifier"`
	UnknownBehavior string `json:"unknown_behavior"`
}

type FixtureRef struct {
	FixtureID string `json:"fixture_id"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
}

type Manifest struct {
	SchemaVersion    string        `json:"schema_version"`
	PackID           string        `json:"pack_id"`
	PackVersion      string        `json:"pack_version"`
	Provider         string        `json:"provider"`
	AdapterType      string        `json:"adapter_type"`
	ConformanceLevel string        `json:"conformance_level"`
	NetworkVerified  bool          `json:"network_verified"`
	Endpoints        []string      `json:"endpoints"`
	Modalities       []string      `json:"modalities"`
	AuthProfiles     []AuthProfile `json:"auth_profiles"`
	UsageProfile     UsageProfile  `json:"usage_profile"`
	ErrorProfile     ErrorProfile  `json:"error_profile"`
	Fixtures         []FixtureRef  `json:"fixtures"`
}

type Pack struct {
	Manifest       Manifest `json:"manifest"`
	ManifestSHA256 string   `json:"manifest_sha256"`
	SchemaSHA256   string   `json:"schema_sha256"`
}

// ConformanceMatchScope describes what an embedded fixture can actually
// establish for a configured channel. An exact provider match remains
// distinct from an adapter-protocol reference match; neither is live network
// evidence because V1 manifests require network_verified=false.
type ConformanceMatchScope string

const (
	ConformanceMatchProvider ConformanceMatchScope = "provider_fixture"
	ConformanceMatchProtocol ConformanceMatchScope = "adapter_protocol_fixture"
)

type ConformanceCase struct {
	CaseID  string `json:"case_id"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

type ConformancePackResult struct {
	PackID         string                       `json:"pack_id"`
	PackVersion    string                       `json:"pack_version"`
	AdapterType    string                       `json:"adapter_type"`
	ManifestSHA256 string                       `json:"manifest_sha256"`
	Fixtures       []ConformanceFixtureIdentity `json:"fixtures"`
	Cases          []ConformanceCase            `json:"cases"`
}

type ConformanceFixtureIdentity struct {
	FixtureID string `json:"fixture_id"`
	SHA256    string `json:"sha256"`
}

type ConformanceSummary struct {
	Packs  int `json:"packs"`
	Cases  int `json:"cases"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

type ConformanceReport struct {
	SchemaVersion string                  `json:"schema_version"`
	SchemaSHA256  string                  `json:"schema_sha256"`
	Packs         []ConformancePackResult `json:"packs"`
	Summary       ConformanceSummary      `json:"summary"`
}

type fixtureDocument struct {
	SchemaVersion string          `json:"schema_version"`
	FixtureID     string          `json:"fixture_id"`
	AdapterType   string          `json:"adapter_type"`
	Request       fixtureRequest  `json:"request"`
	Response      fixtureResponse `json:"response"`
	MissingUsage  fixtureMissing  `json:"missing_usage_response"`
	ErrorCases    []fixtureError  `json:"error_cases"`
}

type fixtureRequest struct {
	Endpoint       string          `json:"endpoint"`
	BaseURL        string          `json:"base_url"`
	RequestedModel string          `json:"requested_model"`
	MappedModel    string          `json:"mapped_model"`
	RequestID      string          `json:"request_id"`
	AdapterConfig  json.RawMessage `json:"adapter_config"`
	Body           json.RawMessage `json:"body"`
	Expected       fixtureExpected `json:"expected"`
}

type fixtureExpected struct {
	Method        string `json:"method"`
	URL           string `json:"url"`
	AuthHeader    string `json:"auth_header"`
	VersionHeader string `json:"version_header,omitempty"`
	ModelOnWire   string `json:"model_on_wire"`
}

type fixtureResponse struct {
	StatusCode int                  `json:"status_code"`
	Body       json.RawMessage      `json:"body"`
	Usage      fixtureExpectedUsage `json:"expected_usage"`
}

type fixtureExpectedUsage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Authority        string `json:"authority"`
	Finality         string `json:"finality"`
}

type fixtureMissing struct {
	StatusCode        int             `json:"status_code"`
	Body              json.RawMessage `json:"body"`
	ExpectedAuthority string          `json:"expected_authority"`
	ExpectedFinality  string          `json:"expected_finality"`
}

type fixtureError struct {
	StatusCode          int             `json:"status_code"`
	Body                json.RawMessage `json:"body"`
	ExpectedFailureKind string          `json:"expected_failure_kind"`
	ExpectedRetryable   bool            `json:"expected_retryable"`
}

func Catalog() ([]Pack, error) {
	schemaBytes, err := artifactFS.ReadFile("artifacts/provider-pack.v1.schema.json")
	if err != nil {
		return nil, fmt.Errorf("read provider pack schema: %w", err)
	}
	if strictErr := eventconnector.ValidateStrictJSON(schemaBytes, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return nil, fmt.Errorf("provider pack schema is not strict JSON: %s", strictErr.Code)
	}
	schemaDigest := digest(schemaBytes)

	manifestPaths, err := fs.Glob(artifactFS, "artifacts/manifests/*.json")
	if err != nil {
		return nil, fmt.Errorf("list provider manifests: %w", err)
	}
	sort.Strings(manifestPaths)
	packs := make([]Pack, 0, len(manifestPaths))
	seenPacks := make(map[string]struct{}, len(manifestPaths))
	for _, manifestPath := range manifestPaths {
		raw, readErr := artifactFS.ReadFile(manifestPath)
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", manifestPath, readErr)
		}
		var manifest Manifest
		if decodeErr := decodeStrict(raw, &manifest); decodeErr != nil {
			return nil, fmt.Errorf("decode %s: %w", manifestPath, decodeErr)
		}
		if validateErr := validateManifest(manifest); validateErr != nil {
			return nil, fmt.Errorf("validate %s: %w", manifestPath, validateErr)
		}
		if _, duplicate := seenPacks[manifest.PackID]; duplicate {
			return nil, fmt.Errorf("duplicate provider pack %q", manifest.PackID)
		}
		seenPacks[manifest.PackID] = struct{}{}
		coveredEndpoints := make(map[string]struct{}, len(manifest.Endpoints))
		coveredAuthHeaders := make(map[string]struct{}, len(manifest.AuthProfiles))
		for _, fixture := range manifest.Fixtures {
			document, fixtureErr := validateFixture(manifest, fixture)
			if fixtureErr != nil {
				return nil, fixtureErr
			}
			coveredEndpoints[document.Request.Endpoint] = struct{}{}
			coveredAuthHeaders[document.Request.Expected.AuthHeader] = struct{}{}
		}
		for _, endpoint := range manifest.Endpoints {
			if _, covered := coveredEndpoints[endpoint]; !covered {
				return nil, fmt.Errorf("pack %s endpoint %s has no fixture", manifest.PackID, endpoint)
			}
		}
		for _, profile := range manifest.AuthProfiles {
			if _, covered := coveredAuthHeaders[profile.WireHeader]; !covered {
				return nil, fmt.Errorf("pack %s auth header %s has no fixture", manifest.PackID, profile.WireHeader)
			}
		}
		packs = append(packs, Pack{
			Manifest:       manifest,
			ManifestSHA256: digest(raw),
			SchemaSHA256:   schemaDigest,
		})
	}
	sort.Slice(packs, func(left, right int) bool {
		return packs[left].Manifest.PackID < packs[right].Manifest.PackID
	})
	return packs, nil
}

func SchemaDigest() (string, error) {
	raw, err := artifactFS.ReadFile("artifacts/provider-pack.v1.schema.json")
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
		return "", fmt.Errorf("golden provider report is not strict JSON: %s", strictErr.Code)
	}
	return digest(raw), nil
}

func Find(provider, adapterType string) (Pack, bool, error) {
	packs, err := Catalog()
	if err != nil {
		return Pack{}, false, err
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	adapterType = adapters.NormalizeType(adapterType)
	for _, pack := range packs {
		if strings.ToLower(pack.Manifest.Provider) == provider &&
			pack.Manifest.AdapterType == adapterType {
			return pack, true, nil
		}
	}
	return Pack{}, false, nil
}

// FindConformance first looks for an exact provider identity and then, for a
// supported built-in adapter, returns that adapter's canonical offline
// protocol fixture. The protocol fallback is not a claim that the configured
// supplier, account, endpoint or network has been verified.
func FindConformance(provider, adapterType string) (Pack, ConformanceMatchScope, bool, error) {
	pack, found, err := Find(provider, adapterType)
	if err != nil || found {
		return pack, ConformanceMatchProvider, found, err
	}
	adapterType = adapters.NormalizeType(adapterType)
	referenceProvider := map[string]string{
		adapters.TypeOpenAICompatible: "openai",
		adapters.TypeAnthropicNative:  "anthropic",
		adapters.TypeAzureOpenAI:      "azure",
	}[adapterType]
	if referenceProvider == "" {
		return Pack{}, "", false, nil
	}
	pack, found, err = Find(referenceProvider, adapterType)
	return pack, ConformanceMatchProtocol, found, err
}

func RunOffline(ctx context.Context, registry *adapters.Registry) (ConformanceReport, error) {
	if registry == nil {
		registry = adapters.DefaultRegistry()
	}
	packs, err := Catalog()
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
		Packs:         make([]ConformancePackResult, 0, len(packs)),
	}
	for _, pack := range packs {
		adapter, getErr := registry.Get(pack.Manifest.AdapterType)
		if getErr != nil {
			return ConformanceReport{}, fmt.Errorf("pack %s: %w", pack.Manifest.PackID, getErr)
		}
		result := ConformancePackResult{
			PackID:         pack.Manifest.PackID,
			PackVersion:    pack.Manifest.PackVersion,
			AdapterType:    pack.Manifest.AdapterType,
			ManifestSHA256: pack.ManifestSHA256,
			Fixtures:       make([]ConformanceFixtureIdentity, 0, len(pack.Manifest.Fixtures)),
			Cases:          make([]ConformanceCase, 0, len(pack.Manifest.Fixtures)*4),
		}
		for _, fixtureRef := range pack.Manifest.Fixtures {
			fixture, loadErr := loadFixture(fixtureRef.Path)
			if loadErr != nil {
				return ConformanceReport{}, loadErr
			}
			result.Fixtures = append(result.Fixtures, ConformanceFixtureIdentity{
				FixtureID: fixtureRef.FixtureID,
				SHA256:    fixtureRef.SHA256,
			})
			result.Cases = append(result.Cases, runFixture(ctx, adapter, pack.Manifest.Provider, fixture)...)
		}
		report.Packs = append(report.Packs, result)
	}
	report.Summary.Packs = len(report.Packs)
	for _, pack := range report.Packs {
		for _, result := range pack.Cases {
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

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version=%q", manifest.SchemaVersion)
	}
	if !packIDPattern.MatchString(manifest.PackID) {
		return fmt.Errorf("invalid pack_id %q", manifest.PackID)
	}
	if !versionPattern.MatchString(manifest.PackVersion) {
		return fmt.Errorf("invalid pack_version %q", manifest.PackVersion)
	}
	if strings.TrimSpace(manifest.Provider) == "" || len(manifest.Provider) > 64 ||
		strings.TrimSpace(manifest.AdapterType) == "" || len(manifest.AdapterType) > 64 {
		return errors.New("provider and adapter_type are required")
	}
	if !adapters.DefaultRegistry().Has(manifest.AdapterType) {
		return fmt.Errorf("adapter_type %q is not registered", manifest.AdapterType)
	}
	if manifest.ConformanceLevel != "fixture_verified" || manifest.NetworkVerified {
		return errors.New("V1 packs must be fixture_verified and network_verified=false")
	}
	if len(manifest.Endpoints) == 0 || len(manifest.Modalities) == 0 ||
		len(manifest.AuthProfiles) == 0 || len(manifest.Fixtures) == 0 {
		return errors.New("endpoints, modalities, auth_profiles and fixtures are required")
	}
	if err := uniqueStrings("endpoints", manifest.Endpoints); err != nil {
		return err
	}
	if err := uniqueStrings("modalities", manifest.Modalities); err != nil {
		return err
	}
	if err := uniqueStrings("cache_classes", manifest.UsageProfile.CacheClasses); err != nil {
		return err
	}
	for _, endpoint := range manifest.Endpoints {
		switch endpoint {
		case string(adapters.EndpointChatCompletions):
			if !containsString(manifest.Modalities, "text") {
				return errors.New("chat completions endpoint requires text modality")
			}
		case string(adapters.EndpointEmbeddings):
			if !containsString(manifest.Modalities, "embeddings") {
				return errors.New("embeddings endpoint requires embeddings modality")
			}
		default:
			return fmt.Errorf("unsupported endpoint %q", endpoint)
		}
	}
	for _, modality := range manifest.Modalities {
		switch modality {
		case "text":
			if !containsString(manifest.Endpoints, string(adapters.EndpointChatCompletions)) {
				return errors.New("text modality requires chat completions")
			}
		case "embeddings":
			if !containsString(manifest.Endpoints, string(adapters.EndpointEmbeddings)) {
				return errors.New("embeddings modality requires embeddings endpoint")
			}
		default:
			return fmt.Errorf("unsupported modality %q", modality)
		}
	}
	seenAuthTypes := make(map[string]struct{}, len(manifest.AuthProfiles))
	seenAuthHeaders := make(map[string]struct{}, len(manifest.AuthProfiles))
	for _, profile := range manifest.AuthProfiles {
		expectedHeader, supported := expectedAuthHeader(manifest.AdapterType, profile.Type)
		if !supported {
			return fmt.Errorf("adapter_type %q does not support auth profile %q", manifest.AdapterType, profile.Type)
		}
		if profile.WireHeader != expectedHeader {
			return fmt.Errorf(
				"auth profile %q wire_header=%q want %q",
				profile.Type,
				profile.WireHeader,
				expectedHeader,
			)
		}
		if _, duplicate := seenAuthTypes[profile.Type]; duplicate {
			return fmt.Errorf("duplicate auth profile %q", profile.Type)
		}
		if _, duplicate := seenAuthHeaders[profile.WireHeader]; duplicate {
			return fmt.Errorf("duplicate auth wire_header %q", profile.WireHeader)
		}
		seenAuthTypes[profile.Type] = struct{}{}
		seenAuthHeaders[profile.WireHeader] = struct{}{}
	}
	if manifest.UsageProfile.AuthorityWhenComplete != "provider_reported" ||
		manifest.UsageProfile.MissingBehavior != "missing_not_zero" {
		return errors.New("invalid usage authority or missing behavior")
	}
	if manifest.ErrorProfile.Classifier != "unirouter.http-failure.v1" ||
		manifest.ErrorProfile.UnknownBehavior != "unknown_not_retryable_without_status_evidence" {
		return errors.New("invalid error profile")
	}
	for _, cacheClass := range manifest.UsageProfile.CacheClasses {
		switch cacheClass {
		case "uncached_input", "cache_read", "cache_creation", "output":
		default:
			return fmt.Errorf("unsupported cache class %q", cacheClass)
		}
	}
	seenFixture := make(map[string]struct{}, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		if !fixturePattern.MatchString(fixture.FixtureID) {
			return fmt.Errorf("invalid fixture_id %q", fixture.FixtureID)
		}
		if _, duplicate := seenFixture[fixture.FixtureID]; duplicate {
			return fmt.Errorf("duplicate fixture_id %q", fixture.FixtureID)
		}
		seenFixture[fixture.FixtureID] = struct{}{}
		if path.Clean(fixture.Path) != fixture.Path ||
			!fixturePathPattern.MatchString(fixture.Path) {
			return fmt.Errorf("invalid fixture path %q", fixture.Path)
		}
		if !digestPattern.MatchString(fixture.SHA256) {
			return fmt.Errorf("invalid fixture digest %q", fixture.SHA256)
		}
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
	if err := decodeStrict(raw, &fixture); err != nil {
		return fixtureDocument{}, fmt.Errorf("decode fixture %s: %w", ref.Path, err)
	}
	if fixture.SchemaVersion != FixtureSchemaVersion ||
		fixture.FixtureID != ref.FixtureID ||
		fixture.AdapterType != manifest.AdapterType {
		return fixtureDocument{}, fmt.Errorf("fixture %s identity does not match manifest", ref.Path)
	}
	if !containsString(manifest.Endpoints, fixture.Request.Endpoint) {
		return fixtureDocument{}, fmt.Errorf("fixture %s endpoint is not declared", ref.Path)
	}
	if !manifestHasAuthHeader(manifest, fixture.Request.Expected.AuthHeader) {
		return fixtureDocument{}, fmt.Errorf("fixture %s auth header is not declared", ref.Path)
	}
	if strings.TrimSpace(fixture.Request.BaseURL) == "" ||
		strings.TrimSpace(fixture.Request.RequestedModel) == "" ||
		strings.TrimSpace(fixture.Request.MappedModel) == "" ||
		strings.TrimSpace(fixture.Request.RequestID) == "" {
		return fixtureDocument{}, fmt.Errorf("fixture %s request identity is incomplete", ref.Path)
	}
	if fixture.Request.Expected.Method != http.MethodPost {
		return fixtureDocument{}, fmt.Errorf("fixture %s expected method must be POST", ref.Path)
	}
	expectedURL, err := url.Parse(fixture.Request.Expected.URL)
	if err != nil || expectedURL.Scheme != "https" || expectedURL.Host == "" || expectedURL.User != nil {
		return fixtureDocument{}, fmt.Errorf("fixture %s expected URL must be a credential-free HTTPS URL", ref.Path)
	}
	for name, document := range map[string]json.RawMessage{
		"adapter_config":     fixture.Request.AdapterConfig,
		"request body":       fixture.Request.Body,
		"response body":      fixture.Response.Body,
		"missing-usage body": fixture.MissingUsage.Body,
	} {
		if strictErr := eventconnector.ValidateStrictJSON(document, eventconnector.StrictJSONOptions{}); strictErr != nil {
			return fixtureDocument{}, fmt.Errorf("fixture %s %s is not strict JSON: %s", ref.Path, name, strictErr.Code)
		}
	}
	if err := adapters.DefaultRegistry().ValidateConfig(fixture.AdapterType, fixture.Request.AdapterConfig); err != nil {
		return fixtureDocument{}, fmt.Errorf("fixture %s adapter config: %w", ref.Path, err)
	}
	if fixture.Response.StatusCode < 200 || fixture.Response.StatusCode >= 300 ||
		fixture.MissingUsage.StatusCode < 200 || fixture.MissingUsage.StatusCode >= 300 {
		return fixtureDocument{}, fmt.Errorf("fixture %s success statuses must be 2xx", ref.Path)
	}
	expectedUsage := fixture.Response.Usage
	if expectedUsage.PromptTokens < 0 || expectedUsage.CompletionTokens < 0 ||
		expectedUsage.TotalTokens != expectedUsage.PromptTokens+expectedUsage.CompletionTokens ||
		expectedUsage.Authority != "provider_reported" || expectedUsage.Finality != "final" {
		return fixtureDocument{}, fmt.Errorf("fixture %s expected usage is inconsistent", ref.Path)
	}
	if fixture.MissingUsage.ExpectedAuthority != "none" ||
		fixture.MissingUsage.ExpectedFinality != "missing" {
		return fixtureDocument{}, fmt.Errorf("fixture %s missing-usage expectation is inconsistent", ref.Path)
	}
	if len(fixture.ErrorCases) == 0 {
		return fixtureDocument{}, fmt.Errorf("fixture %s has no error case", ref.Path)
	}
	for index, errorCase := range fixture.ErrorCases {
		if errorCase.StatusCode >= 200 && errorCase.StatusCode < 300 ||
			strings.TrimSpace(errorCase.ExpectedFailureKind) == "" {
			return fixtureDocument{}, fmt.Errorf("fixture %s error case %d is inconsistent", ref.Path, index+1)
		}
		if strictErr := eventconnector.ValidateStrictJSON(errorCase.Body, eventconnector.StrictJSONOptions{}); strictErr != nil {
			return fixtureDocument{}, fmt.Errorf("fixture %s error case %d is not strict JSON: %s", ref.Path, index+1, strictErr.Code)
		}
	}
	return fixture, nil
}

func loadFixture(fixturePath string) (fixtureDocument, error) {
	raw, err := artifactFS.ReadFile("artifacts/" + fixturePath)
	if err != nil {
		return fixtureDocument{}, err
	}
	var fixture fixtureDocument
	if err := decodeStrict(raw, &fixture); err != nil {
		return fixtureDocument{}, err
	}
	return fixture, nil
}

func runFixture(ctx context.Context, adapter adapters.Adapter, provider string, fixture fixtureDocument) []ConformanceCase {
	results := make([]ConformanceCase, 0, 3+len(fixture.ErrorCases))
	endpoint := adapters.Endpoint(fixture.Request.Endpoint)
	channel := models.Channel{
		BaseURL:       fixture.Request.BaseURL,
		Provider:      provider,
		AdapterType:   fixture.AdapterType,
		AdapterConfig: fixture.Request.AdapterConfig,
	}
	request, err := adapter.BuildRequest(ctx, adapters.RequestInput{
		Endpoint:       endpoint,
		DownstreamBody: fixture.Request.Body,
		RequestedModel: fixture.Request.RequestedModel,
		MappedModel:    fixture.Request.MappedModel,
		Channel:        channel,
		APIKey:         "fixture-secret",
		RequestID:      fixture.Request.RequestID,
	})
	results = append(results, conformanceResult(fixture.FixtureID+"/request", validateFixtureRequest(request, err, fixture.Request.Expected)))

	response, responseErr := adapter.ParseResponse(ctx, adapters.ResponseInput{
		Endpoint:       endpoint,
		StatusCode:     fixture.Response.StatusCode,
		Header:         make(http.Header),
		Body:           fixture.Response.Body,
		Channel:        channel,
		RequestedModel: fixture.Request.RequestedModel,
		UpstreamModel:  fixture.Request.MappedModel,
	})
	results = append(results, conformanceResult(
		fixture.FixtureID+"/usage",
		validateExpectedUsage(response, responseErr, fixture.Response.Usage),
	))

	missing, missingErr := adapter.ParseResponse(ctx, adapters.ResponseInput{
		Endpoint:       endpoint,
		StatusCode:     fixture.MissingUsage.StatusCode,
		Header:         make(http.Header),
		Body:           fixture.MissingUsage.Body,
		Channel:        channel,
		RequestedModel: fixture.Request.RequestedModel,
		UpstreamModel:  fixture.Request.MappedModel,
	})
	results = append(results, conformanceResult(
		fixture.FixtureID+"/missing-usage",
		validateMissingUsage(missing, missingErr, fixture.MissingUsage),
	))

	for index, errorCase := range fixture.ErrorCases {
		normalized := adapter.ClassifyHTTPStatus(errorCase.StatusCode, errorCase.Body)
		var caseErr error
		if normalized == nil {
			caseErr = errors.New("adapter returned no normalized error")
		} else if string(normalized.FailureKind) != errorCase.ExpectedFailureKind ||
			normalized.Retryable != errorCase.ExpectedRetryable {
			caseErr = fmt.Errorf(
				"failure kind/retryable=%s/%t want %s/%t",
				normalized.FailureKind,
				normalized.Retryable,
				errorCase.ExpectedFailureKind,
				errorCase.ExpectedRetryable,
			)
		}
		results = append(results, conformanceResult(
			fmt.Sprintf("%s/error-%d", fixture.FixtureID, index+1),
			caseErr,
		))
	}
	return results
}

func validateFixtureRequest(request *http.Request, buildErr error, expected fixtureExpected) error {
	if buildErr != nil {
		return buildErr
	}
	if request == nil {
		return errors.New("adapter returned nil request")
	}
	if request.Method != expected.Method || request.URL.String() != expected.URL {
		return fmt.Errorf("method/url=%s/%s want %s/%s", request.Method, request.URL, expected.Method, expected.URL)
	}
	authValue := request.Header.Get(expected.AuthHeader)
	if expected.AuthHeader == "Authorization" {
		if authValue != "Bearer fixture-secret" {
			return fmt.Errorf("authorization=%q", authValue)
		}
	} else if authValue != "fixture-secret" {
		return fmt.Errorf("%s=%q", expected.AuthHeader, authValue)
	}
	if expected.VersionHeader != "" && request.Header.Get("anthropic-version") != expected.VersionHeader {
		return fmt.Errorf("anthropic-version=%q want %q", request.Header.Get("anthropic-version"), expected.VersionHeader)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return err
	}
	var payload map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	model, present := payload["model"]
	if expected.ModelOnWire == "" {
		if present {
			return fmt.Errorf("model unexpectedly present on wire: %v", model)
		}
		return nil
	}
	if !present || model != expected.ModelOnWire {
		return fmt.Errorf("model=%v want %q", model, expected.ModelOnWire)
	}
	return nil
}

func validateExpectedUsage(response *adapters.NormalizedResponse, parseErr error, expected fixtureExpectedUsage) error {
	if parseErr != nil {
		return parseErr
	}
	if response == nil {
		return errors.New("adapter returned nil response")
	}
	usage := response.Usage
	if usage.PromptTokens != expected.PromptTokens ||
		usage.CompletionTokens != expected.CompletionTokens ||
		usage.TotalTokens != expected.TotalTokens ||
		string(usage.Authority) != expected.Authority ||
		string(usage.Finality) != expected.Finality {
		return fmt.Errorf("usage=%+v want=%+v", usage, expected)
	}
	return nil
}

func validateMissingUsage(response *adapters.NormalizedResponse, parseErr error, expected fixtureMissing) error {
	if parseErr != nil {
		return parseErr
	}
	if response == nil {
		return errors.New("adapter returned nil response")
	}
	if string(response.Usage.Authority) != expected.ExpectedAuthority ||
		string(response.Usage.Finality) != expected.ExpectedFinality {
		return fmt.Errorf(
			"missing usage authority/finality=%s/%s want %s/%s",
			response.Usage.Authority,
			response.Usage.Finality,
			expected.ExpectedAuthority,
			expected.ExpectedFinality,
		)
	}
	return nil
}

func conformanceResult(caseID string, err error) ConformanceCase {
	if err != nil {
		return ConformanceCase{CaseID: caseID, Passed: false, Message: err.Error()}
	}
	return ConformanceCase{CaseID: caseID, Passed: true}
}

func decodeStrict(raw []byte, target interface{}) error {
	if strictErr := eventconnector.ValidateStrictJSON(raw, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return fmt.Errorf("strict JSON: %s", strictErr.Code)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}

func uniqueStrings(name string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s contains an empty value", name)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%s contains duplicate %q", name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func manifestHasAuthHeader(manifest Manifest, header string) bool {
	for _, profile := range manifest.AuthProfiles {
		if profile.WireHeader == header {
			return true
		}
	}
	return false
}

func expectedAuthHeader(adapterType, profileType string) (string, bool) {
	switch adapterType {
	case adapters.TypeOpenAICompatible:
		if profileType == "bearer" {
			return "Authorization", true
		}
	case adapters.TypeAnthropicNative:
		if profileType == "api_key" {
			return "x-api-key", true
		}
	case adapters.TypeAzureOpenAI:
		switch profileType {
		case "api_key":
			return "api-key", true
		case "entra_bearer":
			return "Authorization", true
		}
	}
	return "", false
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
