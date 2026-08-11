package sourceevidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	RequestSetSchemaVersion   = "unirouter.source-evidence.request-set.v1"
	SourceScopeSchemaVersion  = "unirouter.source-evidence.source-scope.v1"
	SourceFactsSchemaVersion  = "unirouter.poc-source-facts.v1"
	CompatibilityBundleSchema = "unirouter.execution-evidence.bundle.v2"

	SourceIdentityAuthority  = "neutral_campaign_authorization"
	SourceRuntimeObservation = "not_claimed"

	CompatibilityProfileID              = "unirouter.execution-evidence.v2"
	CompatibilityProfileManifestVersion = "2.0.0"
	CompatibilityProfileManifestSHA256  = "sha256:834bbaae05936af76abc4608ab16547581656e415c7d73f79d5d55ef6eca9547"
	CompatibilityBundleSchemaSHA256     = "sha256:e47c5e074e3b7de8c16be9615c5ea073348017c58e07b9ba0566e2a66185f33a"

	MaxRequests          = 20
	MaxFacts             = 200
	MaxTraceFacts        = 200
	MaxProviderAttempts  = 200
	MaxCompatibilityGaps = 201
	MaxContractBytes     = 8 << 20

	CompatibilityGapRuntimeCloseMarker = "runtime_close_marker_unavailable"

	ProviderAttemptAccessBindingLogical = "logical_access_credential"
	ProviderAttemptAccessBindingLegacy  = "legacy_api_key"
	logicalCredentialCorrelationDomain  = "unirouter.source-evidence.logical-credential.v1"
)

var (
	portableIDPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	sourceInstancePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	hashRefPattern           = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	hashPattern              = regexp.MustCompile(`^[0-9a-f]{64}$`)
	signerPublicKeyIDPattern = regexp.MustCompile(`^ed25519:sha256:[0-9a-f]{64}$`)
	gitObjectPattern         = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	stagePattern             = regexp.MustCompile(`^[a-z][a-z0-9_]{0,79}$`)
	metadataKeyPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
	credentialRefPattern     = regexp.MustCompile(`^cred_[a-z0-9][a-z0-9_-]{7,66}$`)
)

var compatibilityStages = [...]string{
	"identity",
	"routing",
	"provider",
	"retry_fallback",
	"billing",
	"usage",
	"logging",
}

var sourceCaptureGapContract = map[string]SourceCaptureGap{
	"call_log_unavailable": {
		Code:   "call_log_unavailable",
		Source: "call_logs",
		Reason: "no call_logs row matched the frozen tenant request identity",
	},
	"request_trace_unavailable": {
		Code:   "request_trace_unavailable",
		Source: "request_trace_events",
		Reason: "no request_trace_events row matched the frozen tenant request identity",
	},
	"provider_attempt_unavailable": {
		Code:   "provider_attempt_unavailable",
		Source: "billing_provider_attempts",
		Reason: "no billing_provider_attempts row matched the frozen tenant request identity",
	},
	"completed_feed_item_unavailable": {
		Code:   "completed_feed_item_unavailable",
		Source: "completed_execution_feed_items",
		Reason: "no completed_execution_feed_items row matched the frozen tenant request identity",
	},
}

var providerFailureKindContract = map[string]string{
	"":                                "",
	"auth":                            "auth",
	"channel_failure":                 "channel_failure",
	"client_request":                  "client_request",
	"configuration":                   "configuration",
	"delivery_failed":                 "delivery_failure",
	"downstream_write_failed":         "delivery_failure",
	"process_lost":                    "delivery_failure",
	"payload_capacity":                "payload_capacity",
	"rate_limit":                      "rate_limit",
	"request_build":                   "request_build",
	"response_encode_failed":          "delivery_failure",
	"response_parse":                  "response_parse",
	"response_read":                   "response_read",
	"response_too_large":              "response_too_large",
	"settlement_usage_persist_failed": "delivery_failure",
	"transport":                       "transport",
	"unknown":                         "unknown",
}

var exportedProviderFailureKinds = map[string]bool{
	"":                   true,
	"auth":               true,
	"channel_failure":    true,
	"client_request":     true,
	"configuration":      true,
	"delivery_failure":   true,
	"payload_capacity":   true,
	"rate_limit":         true,
	"request_build":      true,
	"response_parse":     true,
	"response_read":      true,
	"response_too_large": true,
	"transport":          true,
	"unclassified":       true,
	"unknown":            true,
}

type RequestSet struct {
	SchemaVersion  string             `json:"schema_version"`
	CampaignID     string             `json:"campaign_id"`
	SourceInstance string             `json:"source_instance"`
	FrozenAt       string             `json:"frozen_at"`
	Requests       []RequestSelection `json:"requests"`
	Limits         RequestSetLimits   `json:"limits"`
}

type RequestSelection struct {
	ScenarioID string `json:"scenario_id"`
	RequestID  string `json:"request_id"`
	OrgID      int64  `json:"org_id"`
	ProjectID  int64  `json:"project_id"`
}

type RequestSetLimits struct {
	MaxRequests int `json:"max_requests"`
	MaxFacts    int `json:"max_facts"`
}

// SourceScope is the product-local projection of the Source identity already
// authorized by the neutral campaign. It deliberately does not claim that the
// running process configuration was observed by this offline generator.
type SourceScope struct {
	SchemaVersion      string `json:"schema_version"`
	Product            string `json:"product"`
	SourceInstance     string `json:"source_instance"`
	IdentityAuthority  string `json:"identity_authority"`
	RuntimeObservation string `json:"runtime_observation"`
	FeedSnapshotSchema string `json:"feed_snapshot_schema"`
}

// NeutralAuthorizationBinding is the product-local projection of the frozen
// one-way neutral authorization chain. UniRouter never embeds the User literal
// and never reverse-binds a terminal package/import/result into M or A.
type NeutralAuthorizationBinding struct {
	AuthorizationManifestSHA256  string `json:"authorization_manifest_sha256"`
	AuthorizationStatementSHA256 string `json:"authorization_statement_sha256"`
	AuthorizationSignatureSHA256 string `json:"authorization_signature_sha256"`
	SourceBindingSHA256          string `json:"source_binding_sha256"`
}

type ProtocolFacts struct {
	PinVersion      string              `json:"pin_version"`
	ProtocolVersion string              `json:"protocol_version"`
	ReleaseDigest   string              `json:"release_digest"`
	PinSHA256       string              `json:"pin_sha256"`
	Components      []ProtocolComponent `json:"components"`
}

type ProtocolComponent struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type SourceFacts struct {
	SchemaVersion       string             `json:"schema_version"`
	GeneratedAt         string             `json:"generated_at"`
	RequestSetSHA256    string             `json:"request_set_sha256"`
	SourceBindingSHA256 string             `json:"source_binding_sha256"`
	CampaignID          string             `json:"campaign_id"`
	Source              SourceIdentity     `json:"source"`
	Release             ReleaseFacts       `json:"release"`
	Protocol            ProtocolFacts      `json:"protocol"`
	Snapshot            SnapshotFacts      `json:"snapshot"`
	Requests            []RequestFacts     `json:"requests"`
	FeedRange           FeedRangeFacts     `json:"feed_range"`
	Counts              SourceFactCounts   `json:"counts"`
	Completeness        SourceCompleteness `json:"completeness"`
	Safety              SourceSafety       `json:"safety"`
}

type SourceIdentity struct {
	Product            string `json:"product"`
	Instance           string `json:"instance"`
	IdentityAuthority  string `json:"identity_authority"`
	RuntimeObservation string `json:"runtime_observation"`
	FeedSnapshotSchema string `json:"feed_snapshot_schema"`
}

type ReleaseFacts struct {
	ReleaseSequence int64  `json:"release_sequence"`
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	PackageSHA256   string `json:"package_sha256"`
	TrustedKeyID    string `json:"trusted_key_id"`
	RecordedSchema  string `json:"recorded_schema"`
	CurrentSchema   string `json:"current_schema"`
	RecordedAt      string `json:"recorded_at"`
}

type SnapshotFacts struct {
	Isolation             string `json:"isolation"`
	ReadOnly              bool   `json:"read_only"`
	TransactionObservedAt string `json:"transaction_observed_at"`
	TransactionIDAssigned bool   `json:"transaction_id_assigned"`
	SelectionMode         string `json:"selection_mode"`
}

type RequestFacts struct {
	ScenarioID       string                `json:"scenario_id"`
	RequestID        string                `json:"request_id"`
	OrgID            int64                 `json:"org_id"`
	ProjectID        int64                 `json:"project_id"`
	CallLog          *CallLogFact          `json:"call_log"`
	TraceEvents      []TraceFact           `json:"trace_events"`
	ProviderAttempts []ProviderAttemptFact `json:"provider_attempts"`
	CompletedFeed    *CompletedFeedFact    `json:"completed_feed"`
	CaptureGaps      []SourceCaptureGap    `json:"capture_gaps"`
}

type CallLogFact struct {
	FactSHA256         string `json:"fact_sha256"`
	SourceRef          string `json:"source_ref"`
	ID                 int64  `json:"id"`
	Model              string `json:"model"`
	RequestedModel     string `json:"requested_model"`
	Provider           string `json:"provider"`
	Channel            string `json:"channel"`
	PromptTokens       int64  `json:"prompt_tokens"`
	CompletionTokens   int64  `json:"completion_tokens"`
	TotalTokens        int64  `json:"total_tokens"`
	CostMicros         string `json:"cost_micros"`
	Currency           string `json:"currency"`
	AmountScale        int64  `json:"amount_scale"`
	UsageSource        string `json:"usage_source"`
	UsageAuthority     string `json:"usage_authority"`
	UsageFinality      string `json:"usage_finality"`
	StatusCode         int64  `json:"status_code"`
	UpstreamStatusCode *int64 `json:"upstream_status_code,omitempty"`
	LatencyMS          int64  `json:"latency_ms"`
	TraceID            string `json:"trace_id,omitempty"`
	AttemptID          string `json:"attempt_id,omitempty"`
	ActorType          string `json:"actor_type,omitempty"`
	PayloadCaptureMode string `json:"payload_capture_mode"`
	UpstreamErrorSeen  bool   `json:"upstream_error_was_present"`
	IPAddressSeen      bool   `json:"ip_address_was_present"`
	CreatedAt          string `json:"created_at"`
}

type TraceFact struct {
	FactSHA256         string   `json:"fact_sha256"`
	SourceRef          string   `json:"source_ref"`
	ID                 int64    `json:"id"`
	EventType          string   `json:"event_type"`
	Stage              string   `json:"stage"`
	CompatibilityStage string   `json:"compatibility_stage"`
	Severity           string   `json:"severity"`
	Model              string   `json:"model,omitempty"`
	RequestedModel     string   `json:"requested_model,omitempty"`
	Provider           string   `json:"provider,omitempty"`
	Channel            string   `json:"channel,omitempty"`
	StatusCode         *int64   `json:"status_code,omitempty"`
	LatencyMS          int64    `json:"latency_ms"`
	TraceID            string   `json:"trace_id,omitempty"`
	SpanID             string   `json:"span_id,omitempty"`
	ParentSpanID       string   `json:"parent_span_id,omitempty"`
	AttemptID          string   `json:"attempt_id,omitempty"`
	ActorType          string   `json:"actor_type,omitempty"`
	DecisionKind       string   `json:"decision_kind,omitempty"`
	DecisionResult     string   `json:"decision_result,omitempty"`
	DecisionReason     string   `json:"decision_reason,omitempty"`
	CaptureGap         string   `json:"capture_gap,omitempty"`
	MetadataKeys       []string `json:"metadata_keys"`
	PayloadCaptureMode string   `json:"payload_capture_mode"`
	CreatedAt          string   `json:"created_at"`
}

type ProviderAttemptFact struct {
	FactSHA256                         string  `json:"fact_sha256"`
	SourceRef                          string  `json:"source_ref"`
	AttemptID                          string  `json:"attempt_id"`
	AccessBindingType                  string  `json:"access_binding_type"`
	LogicalCredentialCorrelationSHA256 string  `json:"logical_credential_correlation_sha256,omitempty"`
	RouteSequence                      int     `json:"route_sequence"`
	RouteTotal                         int     `json:"route_total"`
	Endpoint                           string  `json:"endpoint"`
	Provider                           string  `json:"provider"`
	State                              string  `json:"state"`
	OutcomeCertainty                   string  `json:"outcome_certainty"`
	RetryAuthorized                    bool    `json:"retry_authorized"`
	HTTPStatus                         *int64  `json:"http_status,omitempty"`
	FailureKind                        string  `json:"failure_kind,omitempty"`
	CalculatedCostMicros               *string `json:"calculated_cost_micros,omitempty"`
	ChargedCostMicros                  *string `json:"charged_cost_micros,omitempty"`
	StartedAt                          string  `json:"started_at"`
	ResponseReceivedAt                 string  `json:"response_received_at,omitempty"`
	FinancialResolvedAt                string  `json:"financial_resolved_at,omitempty"`
	UpdatedAt                          string  `json:"updated_at"`
}

type CompletedFeedFact struct {
	FactSHA256          string             `json:"fact_sha256"`
	SourceRef           string             `json:"source_ref"`
	SequenceID          int64              `json:"sequence_id"`
	TerminalStatus      string             `json:"terminal_status"`
	TerminalAvailableAt string             `json:"terminal_available_at"`
	Revision            string             `json:"revision"`
	SnapshotSHA256      string             `json:"snapshot_sha256"`
	ContentSummary      FeedContentSummary `json:"content_summary"`
}

type FeedContentSummary struct {
	Model                string `json:"model"`
	PromptTokens         int    `json:"prompt_tokens"`
	CompletionTokens     int    `json:"completion_tokens"`
	UsageAuthority       string `json:"usage_authority"`
	UsageFinality        string `json:"usage_finality"`
	CalculatedCostMicros string `json:"calculated_cost_micros"`
	ChargedCostMicros    string `json:"charged_cost_micros"`
	PricingVersion       string `json:"pricing_version"`
}

type SourceCaptureGap struct {
	Code   string `json:"code"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type FeedRangeFacts struct {
	Selection          string `json:"selection"`
	SelectedItems      int    `json:"selected_items"`
	MinSequenceID      *int64 `json:"min_sequence_id,omitempty"`
	MaxSequenceID      *int64 `json:"max_sequence_id,omitempty"`
	CursorClaim        string `json:"cursor_claim"`
	HighWatermarkClaim string `json:"high_watermark_claim"`
}

type SourceFactCounts struct {
	RequestedRequests  int `json:"requested_requests"`
	CallLogs           int `json:"call_logs"`
	TraceEvents        int `json:"trace_events"`
	ProviderAttempts   int `json:"provider_attempts"`
	CompletedFeedItems int `json:"completed_feed_items"`
	CaptureGaps        int `json:"capture_gaps"`
	TotalFacts         int `json:"total_facts"`
}

type SourceCompleteness struct {
	ClaimScope                    string `json:"claim_scope"`
	State                         string `json:"state"`
	BoundedRequestSetComplete     bool   `json:"bounded_request_set_complete"`
	ExecutionEvidenceV2Projection string `json:"execution_evidence_v2_projection"`
	RuntimeCloseMarkerAvailable   bool   `json:"runtime_close_marker_available"`
}

type SourceSafety struct {
	DatabaseMutation       string `json:"database_mutation"`
	AdditionalGatewayCalls int    `json:"additional_gateway_calls"`
	ProviderCalls          int    `json:"provider_calls"`
	FeedCalls              int    `json:"feed_calls"`
	UpstreamCalls          int    `json:"upstream_calls"`
	RawPromptsIncluded     bool   `json:"raw_prompts_included"`
	RawResponsesIncluded   bool   `json:"raw_responses_included"`
	SecretsIncluded        bool   `json:"secrets_included"`
	RawIPsIncluded         bool   `json:"raw_ips_included"`
	RawErrorsIncluded      bool   `json:"raw_errors_included"`
}

type CompatibilityProfileRef struct {
	ID                 string `json:"id"`
	ManifestVersion    string `json:"manifest_version"`
	ManifestSHA256     string `json:"manifest_sha256"`
	BundleSchemaSHA256 string `json:"bundle_schema_sha256"`
}

type CompatibilityCompleteness struct {
	State                    string   `json:"state"`
	SourceProfileComplete    bool     `json:"source_profile_complete"`
	ExpectedTraceEventHashes []string `json:"expected_trace_event_hashes"`
	ExpectedCallLogHashes    []string `json:"expected_call_log_hashes"`
	ObservedTraceEventHashes []string `json:"observed_trace_event_hashes"`
	ObservedCallLogHashes    []string `json:"observed_call_log_hashes"`
	MissingEventHashes       []string `json:"missing_event_hashes"`
}

type CompatibilityStageCoverage struct {
	Stage             string   `json:"stage"`
	Status            string   `json:"status"`
	SourceEventHashes []string `json:"source_event_hashes"`
}

type CompatibilityTraceFact struct {
	SourceRef          string         `json:"source_ref"`
	SourceEventHash    string         `json:"source_event_hash"`
	EventType          string         `json:"event_type"`
	Stage              string         `json:"stage"`
	Severity           string         `json:"severity"`
	CreatedAt          string         `json:"created_at"`
	TraceID            string         `json:"trace_id,omitempty"`
	SpanID             string         `json:"span_id,omitempty"`
	ParentSpanID       string         `json:"parent_span_id,omitempty"`
	AttemptID          string         `json:"attempt_id,omitempty"`
	ActorType          string         `json:"actor_type,omitempty"`
	ActorRef           string         `json:"actor_ref,omitempty"`
	OnBehalfOf         string         `json:"on_behalf_of,omitempty"`
	Model              string         `json:"model,omitempty"`
	RequestedModel     string         `json:"requested_model,omitempty"`
	Provider           string         `json:"provider,omitempty"`
	Channel            string         `json:"channel,omitempty"`
	StatusCode         *int64         `json:"status_code,omitempty"`
	LatencyMS          int64          `json:"latency_ms,omitempty"`
	DecisionKind       string         `json:"decision_kind,omitempty"`
	DecisionResult     string         `json:"decision_result,omitempty"`
	DecisionReason     string         `json:"decision_reason,omitempty"`
	CaptureGap         string         `json:"capture_gap,omitempty"`
	PayloadCaptureMode string         `json:"payload_capture_mode"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type CompatibilityCallLogFact struct {
	SourceRef          string `json:"source_ref"`
	SourceEventHash    string `json:"source_event_hash"`
	CreatedAt          string `json:"created_at"`
	TraceID            string `json:"trace_id,omitempty"`
	AttemptID          string `json:"attempt_id,omitempty"`
	ActorType          string `json:"actor_type,omitempty"`
	ActorRef           string `json:"actor_ref,omitempty"`
	Model              string `json:"model"`
	RequestedModel     string `json:"requested_model,omitempty"`
	Provider           string `json:"provider"`
	Channel            string `json:"channel,omitempty"`
	StatusCode         int64  `json:"status_code"`
	UpstreamStatusCode *int64 `json:"upstream_status_code,omitempty"`
	LatencyMS          int64  `json:"latency_ms"`
	PromptTokens       int64  `json:"prompt_tokens"`
	CompletionTokens   int64  `json:"completion_tokens"`
	CostMicros         string `json:"cost_micros"`
	Currency           string `json:"currency"`
	AmountScale        int64  `json:"amount_scale"`
	UsageSource        string `json:"usage_source,omitempty"`
	UsageAuthority     string `json:"usage_authority,omitempty"`
	UsageFinality      string `json:"usage_finality,omitempty"`
	PayloadCaptureMode string `json:"payload_capture_mode"`
}

type CompatibilityGap struct {
	Code            string `json:"code"`
	Stage           string `json:"stage"`
	Reason          string `json:"reason"`
	SourceEventHash string `json:"source_event_hash,omitempty"`
}

type CompatibilityBundle struct {
	SchemaVersion  string                       `json:"schema_version"`
	Profile        CompatibilityProfileRef      `json:"profile"`
	SourceInstance string                       `json:"source_instance"`
	RequestID      string                       `json:"request_id"`
	OrgID          int64                        `json:"org_id"`
	ProjectID      *int64                       `json:"project_id,omitempty"`
	ClosedAt       string                       `json:"closed_at"`
	Completeness   CompatibilityCompleteness    `json:"completeness"`
	StageCoverage  []CompatibilityStageCoverage `json:"stage_coverage"`
	TraceEvents    []CompatibilityTraceFact     `json:"trace_events"`
	CallLogs       []CompatibilityCallLogFact   `json:"call_logs"`
	CaptureGaps    []CompatibilityGap           `json:"capture_gaps"`
}

func DecodeRequestSet(body []byte) (RequestSet, error) {
	var requestSet RequestSet
	if err := decodeStrict(body, &requestSet); err != nil {
		return RequestSet{}, fmt.Errorf("decode request set: %w", err)
	}
	if err := ValidateRequestSet(requestSet); err != nil {
		return RequestSet{}, err
	}
	return requestSet, nil
}

func DecodeSourceFacts(body []byte) (SourceFacts, error) {
	var facts SourceFacts
	if err := decodeStrict(body, &facts); err != nil {
		return SourceFacts{}, fmt.Errorf("decode source facts: %w", err)
	}
	if err := ValidateSourceFacts(facts); err != nil {
		return SourceFacts{}, err
	}
	return facts, nil
}

func DecodeSourceScope(body []byte) (SourceScope, error) {
	var scope SourceScope
	if err := decodeStrict(body, &scope); err != nil {
		return SourceScope{}, fmt.Errorf("decode Source scope: %w", err)
	}
	if err := ValidateSourceScope(scope); err != nil {
		return SourceScope{}, err
	}
	return scope, nil
}

func DecodeCompatibilityBundle(body []byte) (CompatibilityBundle, error) {
	var bundle CompatibilityBundle
	if err := decodeStrict(body, &bundle); err != nil {
		return CompatibilityBundle{}, fmt.Errorf("decode execution-evidence-v2 compatibility member: %w", err)
	}
	if err := ValidateCompatibilityBundle(bundle); err != nil {
		return CompatibilityBundle{}, err
	}
	return bundle, nil
}

func ValidateRequestSet(value RequestSet) error {
	if value.SchemaVersion != RequestSetSchemaVersion {
		return fmt.Errorf("request set schema_version must be %q", RequestSetSchemaVersion)
	}
	if !portableWithin(value.CampaignID, 128) {
		return errors.New("request set campaign_id is invalid")
	}
	if !sourceInstancePattern.MatchString(value.SourceInstance) {
		return errors.New("request set source_instance is invalid")
	}
	if _, err := parseCanonicalTime("request set frozen_at", value.FrozenAt); err != nil {
		return err
	}
	if value.Limits.MaxRequests != MaxRequests || value.Limits.MaxFacts != MaxFacts {
		return fmt.Errorf("request set limits must be max_requests=%d and max_facts=%d", MaxRequests, MaxFacts)
	}
	if len(value.Requests) == 0 || len(value.Requests) > MaxRequests {
		return fmt.Errorf("request set must contain 1..%d requests", MaxRequests)
	}
	seenScenario := make(map[string]bool, len(value.Requests))
	seenRequest := make(map[string]bool, len(value.Requests))
	for index, request := range value.Requests {
		if !portableWithin(request.ScenarioID, 120) || !portableWithin(request.RequestID, 64) || request.OrgID <= 0 || request.ProjectID <= 0 {
			return fmt.Errorf("request set requests[%d] is invalid", index)
		}
		identity := fmt.Sprintf("%d/%s", request.OrgID, request.RequestID)
		if seenScenario[request.ScenarioID] || seenRequest[identity] {
			return fmt.Errorf("request set requests[%d] duplicates scenario or tenant request identity", index)
		}
		seenScenario[request.ScenarioID] = true
		seenRequest[identity] = true
	}
	return nil
}

func SourceScopeForRequestSet(requestSet RequestSet) SourceScope {
	return SourceScope{
		SchemaVersion:      SourceScopeSchemaVersion,
		Product:            "unirouter",
		SourceInstance:     requestSet.SourceInstance,
		IdentityAuthority:  SourceIdentityAuthority,
		RuntimeObservation: SourceRuntimeObservation,
		FeedSnapshotSchema: "unirouter.completed-execution.v1",
	}
}

func ValidateSourceScope(value SourceScope) error {
	if value.SchemaVersion != SourceScopeSchemaVersion || value.Product != "unirouter" || !sourceInstancePattern.MatchString(value.SourceInstance) {
		return errors.New("Source scope identity is invalid")
	}
	if value.IdentityAuthority != SourceIdentityAuthority || value.RuntimeObservation != SourceRuntimeObservation || value.FeedSnapshotSchema != "unirouter.completed-execution.v1" {
		return errors.New("Source scope authority or claim boundary is invalid")
	}
	return nil
}

func ValidateSourceScopeForRequestSet(value SourceScope, requestSet RequestSet) error {
	if err := ValidateSourceScope(value); err != nil {
		return err
	}
	if value.SourceInstance != requestSet.SourceInstance {
		return errors.New("Source scope instance does not match request set")
	}
	return nil
}

func ValidateNeutralAuthorizationBinding(value NeutralAuthorizationBinding, sourceScope SourceScope) error {
	if !hashRefPattern.MatchString(value.AuthorizationManifestSHA256) || !hashRefPattern.MatchString(value.AuthorizationStatementSHA256) || !hashRefPattern.MatchString(value.AuthorizationSignatureSHA256) || !hashRefPattern.MatchString(value.SourceBindingSHA256) {
		return errors.New("neutral authorization binding digests are invalid")
	}
	expectedSourceBinding, err := SemanticSHA256(sourceScope)
	if err != nil {
		return err
	}
	if value.SourceBindingSHA256 != expectedSourceBinding {
		return errors.New("neutral authorization source_binding_sha256 does not match the canonical Source scope")
	}
	return nil
}

func ValidateSourceFacts(value SourceFacts) error {
	if value.SchemaVersion != SourceFactsSchemaVersion || !hashRefPattern.MatchString(value.RequestSetSHA256) || !hashRefPattern.MatchString(value.SourceBindingSHA256) {
		return errors.New("source facts contract identity is invalid")
	}
	if _, err := parseCanonicalTime("source facts generated_at", value.GeneratedAt); err != nil {
		return err
	}
	if !portableWithin(value.CampaignID, 128) || value.Source.Product != "unirouter" || !sourceInstancePattern.MatchString(value.Source.Instance) {
		return errors.New("source facts campaign or Source identity is invalid")
	}
	if value.Source.IdentityAuthority != SourceIdentityAuthority || value.Source.RuntimeObservation != SourceRuntimeObservation || value.Source.FeedSnapshotSchema != "unirouter.completed-execution.v1" {
		return errors.New("source facts Source authority or claim boundary is invalid")
	}
	if !gitObjectPattern.MatchString(value.Release.Commit) || value.Release.ReleaseSequence <= 0 || !portableWithin(value.Release.Version, 128) {
		return errors.New("source facts release identity is invalid")
	}
	if !hashPattern.MatchString(value.Release.PackageSHA256) || !metadataKeyPattern.MatchString(value.Release.TrustedKeyID) || !portableWithin(value.Release.TrustedKeyID, 64) {
		return errors.New("source facts release package or trusted key identity is invalid")
	}
	if value.Release.CurrentSchema == "" || value.Release.RecordedSchema == "" || value.Release.CurrentSchema != value.Release.RecordedSchema {
		return errors.New("source facts current and recorded schema must be equal and nonempty")
	}
	if _, err := parseCanonicalTime("source facts release recorded_at", value.Release.RecordedAt); err != nil {
		return err
	}
	if err := ValidateProtocolFacts(value.Protocol); err != nil {
		return err
	}
	if value.Snapshot.Isolation != "repeatable read" || !value.Snapshot.ReadOnly || value.Snapshot.TransactionIDAssigned || value.Snapshot.SelectionMode != "bounded_tenant_request_set" {
		return errors.New("source facts snapshot must prove read-only repeatable-read without transaction ID assignment")
	}
	if _, err := parseCanonicalTime("source facts transaction_observed_at", value.Snapshot.TransactionObservedAt); err != nil {
		return err
	}
	if len(value.Requests) == 0 || len(value.Requests) > MaxRequests || value.Counts.RequestedRequests != len(value.Requests) {
		return errors.New("source facts request count is invalid")
	}
	counts := SourceFactCounts{RequestedRequests: len(value.Requests)}
	seen := make(map[string]bool, len(value.Requests))
	for index := range value.Requests {
		request := &value.Requests[index]
		if !portableWithin(request.ScenarioID, 120) || !portableWithin(request.RequestID, 64) || request.OrgID <= 0 || request.ProjectID <= 0 {
			return fmt.Errorf("source facts requests[%d] identity is invalid", index)
		}
		key := fmt.Sprintf("%d/%s", request.OrgID, request.RequestID)
		if seen[key] {
			return fmt.Errorf("source facts requests[%d] is duplicated", index)
		}
		seen[key] = true
		if request.CallLog != nil {
			if err := validateCallLogFact(*request.CallLog); err != nil {
				return fmt.Errorf("source facts requests[%d].call_log: %w", index, err)
			}
			counts.CallLogs++
		}
		for traceIndex, trace := range request.TraceEvents {
			if err := validateTraceFact(trace); err != nil {
				return fmt.Errorf("source facts requests[%d].trace_events[%d]: %w", index, traceIndex, err)
			}
		}
		for attemptIndex, attempt := range request.ProviderAttempts {
			if err := validateProviderAttemptFact(attempt); err != nil {
				return fmt.Errorf("source facts requests[%d].provider_attempts[%d]: %w", index, attemptIndex, err)
			}
		}
		if request.CompletedFeed != nil {
			if err := validateCompletedFeedFact(*request.CompletedFeed); err != nil {
				return fmt.Errorf("source facts requests[%d].completed_feed: %w", index, err)
			}
			counts.CompletedFeedItems++
		}
		if request.CallLog != nil && request.CompletedFeed != nil {
			call := request.CallLog
			feed := request.CompletedFeed.ContentSummary
			if call.Model != feed.Model || call.PromptTokens != int64(feed.PromptTokens) || call.CompletionTokens != int64(feed.CompletionTokens) || call.CostMicros != feed.ChargedCostMicros || call.UsageAuthority != feed.UsageAuthority || call.UsageFinality != feed.UsageFinality {
				return fmt.Errorf("source facts requests[%d] call log and completed-feed terminal facts conflict", index)
			}
		}
		for gapIndex, gap := range request.CaptureGaps {
			expected, ok := sourceCaptureGapContract[gap.Code]
			if !ok || gap != expected {
				return fmt.Errorf("source facts requests[%d].capture_gaps[%d] is invalid", index, gapIndex)
			}
		}
		counts.TraceEvents += len(request.TraceEvents)
		counts.ProviderAttempts += len(request.ProviderAttempts)
		counts.CaptureGaps += len(request.CaptureGaps)
	}
	counts.TotalFacts = counts.CallLogs + counts.TraceEvents + counts.ProviderAttempts + counts.CompletedFeedItems + counts.CaptureGaps
	if counts.TraceEvents > MaxTraceFacts || counts.ProviderAttempts > MaxProviderAttempts || counts.TotalFacts > MaxFacts || counts != value.Counts {
		return errors.New("source facts declared counts or bounded fact total is invalid")
	}
	if value.FeedRange.Selection != "bounded_request_set" || value.FeedRange.SelectedItems != counts.CompletedFeedItems || value.FeedRange.CursorClaim != "not_part_of_source_facts" || value.FeedRange.HighWatermarkClaim != "not_part_of_source_facts" {
		return errors.New("source facts feed range exceeds the bounded Source-facts claim")
	}
	if counts.CompletedFeedItems == 0 {
		if value.FeedRange.MinSequenceID != nil || value.FeedRange.MaxSequenceID != nil {
			return errors.New("empty feed range must not claim sequence bounds")
		}
	} else if value.FeedRange.MinSequenceID == nil || value.FeedRange.MaxSequenceID == nil || *value.FeedRange.MinSequenceID <= 0 || *value.FeedRange.MinSequenceID > *value.FeedRange.MaxSequenceID {
		return errors.New("nonempty feed range has invalid sequence bounds")
	}
	if value.Completeness.ClaimScope != SourceFactsSchemaVersion || value.Completeness.State != "complete" || !value.Completeness.BoundedRequestSetComplete || value.Completeness.ExecutionEvidenceV2Projection != "partial" || value.Completeness.RuntimeCloseMarkerAvailable {
		return errors.New("source facts completeness must remain complete only for the bounded Source-facts scope")
	}
	if value.Safety.DatabaseMutation != "none" || value.Safety.AdditionalGatewayCalls != 0 || value.Safety.ProviderCalls != 0 || value.Safety.FeedCalls != 0 || value.Safety.UpstreamCalls != 0 || value.Safety.RawPromptsIncluded || value.Safety.RawResponsesIncluded || value.Safety.SecretsIncluded || value.Safety.RawIPsIncluded || value.Safety.RawErrorsIncluded {
		return errors.New("source facts safety boundary is invalid")
	}
	return nil
}

func ValidateProtocolFacts(value ProtocolFacts) error {
	if value.PinVersion == "" || value.ProtocolVersion == "" || !hashRefPattern.MatchString(value.ReleaseDigest) || !hashRefPattern.MatchString(value.PinSHA256) || len(value.Components) == 0 || len(value.Components) > 64 {
		return errors.New("protocol facts identity is invalid")
	}
	last := ""
	for index, component := range value.Components {
		if !stagePattern.MatchString(component.Name) || !hashRefPattern.MatchString(component.SHA256) || component.Name <= last {
			return fmt.Errorf("protocol facts components[%d] is invalid or not strictly sorted", index)
		}
		last = component.Name
	}
	return nil
}

func ValidateCompatibilityBundle(value CompatibilityBundle) error {
	if value.SchemaVersion != CompatibilityBundleSchema || value.Profile.ID != CompatibilityProfileID || value.Profile.ManifestVersion != CompatibilityProfileManifestVersion || value.Profile.ManifestSHA256 != CompatibilityProfileManifestSHA256 || value.Profile.BundleSchemaSHA256 != CompatibilityBundleSchemaSHA256 {
		return errors.New("execution-evidence-v2 compatibility profile identity is invalid")
	}
	if !sourceInstancePattern.MatchString(value.SourceInstance) || !portableWithin(value.RequestID, 64) || value.OrgID <= 0 || value.ProjectID == nil || *value.ProjectID <= 0 {
		return errors.New("execution-evidence-v2 compatibility identity is invalid")
	}
	closedAt, err := parseCanonicalTime("execution-evidence-v2 closed_at", value.ClosedAt)
	if err != nil {
		return err
	}
	if len(value.TraceEvents) > MaxTraceFacts || len(value.CallLogs) > 1 || len(value.CaptureGaps) == 0 || len(value.CaptureGaps) > MaxCompatibilityGaps {
		return errors.New("execution-evidence-v2 compatibility member violates bounded partial shape")
	}
	observedTrace := make([]string, 0, len(value.TraceEvents))
	observedCalls := make([]string, 0, len(value.CallLogs))
	observedAll := make(map[string]bool, len(value.TraceEvents)+len(value.CallLogs))
	for index, fact := range value.TraceEvents {
		if err := validateCompatibilityTrace(index, fact, closedAt); err != nil {
			return err
		}
		observedTrace = append(observedTrace, fact.SourceEventHash)
		observedAll[fact.SourceEventHash] = true
	}
	for index, fact := range value.CallLogs {
		if err := validateCompatibilityCall(index, fact, closedAt); err != nil {
			return err
		}
		observedCalls = append(observedCalls, fact.SourceEventHash)
		if observedAll[fact.SourceEventHash] {
			return errors.New("execution-evidence-v2 compatibility hash reused across trace and call log")
		}
		observedAll[fact.SourceEventHash] = true
	}
	if !sameNormalizedHashes(observedTrace, value.Completeness.ExpectedTraceEventHashes) || !sameNormalizedHashes(observedTrace, value.Completeness.ObservedTraceEventHashes) || !sameNormalizedHashes(observedCalls, value.Completeness.ExpectedCallLogHashes) || !sameNormalizedHashes(observedCalls, value.Completeness.ObservedCallLogHashes) || len(value.Completeness.MissingEventHashes) != 0 {
		return errors.New("execution-evidence-v2 compatibility expected and observed hashes must describe only the frozen observed subset")
	}
	if value.Completeness.State != "partial" || value.Completeness.SourceProfileComplete {
		return errors.New("execution-evidence-v2 compatibility member cannot claim complete without a runtime close marker")
	}
	if len(value.StageCoverage) != len(compatibilityStages) {
		return errors.New("execution-evidence-v2 compatibility stage coverage must contain seven stages")
	}
	for index, coverage := range value.StageCoverage {
		if coverage.Stage != compatibilityStages[index] || (coverage.Status != "present" && coverage.Status != "gap" && coverage.Status != "unknown" && coverage.Status != "not_applicable") {
			return fmt.Errorf("execution-evidence-v2 stage_coverage[%d] is invalid", index)
		}
		hashes := normalizedHashes(coverage.SourceEventHashes)
		if (coverage.Status == "present") != (len(hashes) > 0) {
			return fmt.Errorf("execution-evidence-v2 stage_coverage[%d] contradicts cited facts", index)
		}
		for _, hash := range hashes {
			if !observedAll[hash] {
				return fmt.Errorf("execution-evidence-v2 stage_coverage[%d] cites unobserved fact", index)
			}
		}
	}
	markerFound := false
	for index, gap := range value.CaptureGaps {
		if !stagePattern.MatchString(gap.Code) || !validCompatibilityGapStage(gap.Stage) || strings.TrimSpace(gap.Reason) == "" || len(gap.Reason) > 512 || (gap.SourceEventHash != "" && !hashPattern.MatchString(gap.SourceEventHash)) {
			return fmt.Errorf("execution-evidence-v2 capture_gaps[%d] is invalid", index)
		}
		if gap.Code == CompatibilityGapRuntimeCloseMarker && gap.Stage == "source" {
			markerFound = true
		}
	}
	if !markerFound {
		return errors.New("execution-evidence-v2 compatibility member must declare runtime_close_marker_unavailable")
	}
	return nil
}

func CanonicalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func SemanticSHA256(value any) (string, error) {
	body, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return hashBytes(body), nil
}

func ContentSHA256(body []byte) string {
	return hashBytes(body)
}

func ValidateExternalSchemaVersion(body []byte, expected string) error {
	if !portableWithin(expected, 160) || len(body) == 0 || len(body) > MaxContractBytes {
		return errors.New("external contract identity is invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return fmt.Errorf("external contract strict JSON: %s", strictErr.Code)
	}
	var header struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(body, &header); err != nil || header.SchemaVersion != expected {
		return fmt.Errorf("external contract schema_version must be %q", expected)
	}
	return nil
}

func hashBytes(body []byte) string {
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func decodeStrict(body []byte, destination any) error {
	if len(body) == 0 || len(body) > MaxContractBytes {
		return errors.New("contract JSON size is invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return fmt.Errorf("strict JSON: %s", strictErr.Code)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("contract JSON contains trailing content")
	}
	return nil
}

func parseCanonicalTime(field, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339: %w", field, err)
	}
	if parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, fmt.Errorf("%s must be canonical UTC RFC3339", field)
	}
	return parsed, nil
}

func canonicalTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func portableWithin(value string, maximum int) bool {
	return len(value) > 0 && len(value) <= maximum && portableIDPattern.MatchString(value)
}

func validateCallLogFact(value CallLogFact) error {
	if !hashRefPattern.MatchString(value.FactSHA256) || !portableWithin(value.SourceRef, 512) || value.ID <= 0 || strings.TrimSpace(value.Model) == "" || len(value.Model) > 200 || strings.TrimSpace(value.Provider) == "" || len(value.Provider) > 120 || value.PromptTokens < 0 || value.CompletionTokens < 0 || value.TotalTokens < 0 || value.TotalTokens != value.PromptTokens+value.CompletionTokens || value.Currency != "CNY" || value.AmountScale != 1_000_000 || !unsignedDecimal(value.CostMicros) || value.StatusCode < 0 || value.StatusCode > 999 || value.LatencyMS < 0 || value.PayloadCaptureMode != "metadata_only" {
		return errors.New("call log violates payload-neutral execution and money shape")
	}
	if value.ActorType != "" && !isSourceActorType(value.ActorType) {
		return errors.New("call log actor_type is not an allowlisted source-owned category")
	}
	declaredHash := value.FactSHA256
	value.FactSHA256 = ""
	if !matchesSemanticHash(value, declaredHash) {
		return errors.New("call log fact_sha256 does not match canonical fact content")
	}
	if value.UpstreamStatusCode != nil && (*value.UpstreamStatusCode < 0 || *value.UpstreamStatusCode > 999) {
		return errors.New("call log upstream status is invalid")
	}
	_, err := parseCanonicalTime("call log created_at", value.CreatedAt)
	return err
}

func validateTraceFact(value TraceFact) error {
	if !hashRefPattern.MatchString(value.FactSHA256) || !portableWithin(value.SourceRef, 512) || value.ID <= 0 || !portableWithin(value.EventType, 120) || !stagePattern.MatchString(value.Stage) || !isCompatibilityStage(value.CompatibilityStage) || (value.Severity != "info" && value.Severity != "warning" && value.Severity != "error") || value.LatencyMS < 0 || value.PayloadCaptureMode != "metadata_only" || len(value.MetadataKeys) > 64 {
		return errors.New("trace fact violates payload-neutral shape")
	}
	if value.StatusCode != nil && (*value.StatusCode < 0 || *value.StatusCode > 999) {
		return errors.New("trace fact status code is invalid")
	}
	if value.ActorType != "" && !isSourceActorType(value.ActorType) {
		return errors.New("trace fact actor_type is not an allowlisted source-owned category")
	}
	for name, field := range map[string]struct {
		value   string
		maximum int
	}{
		"decision_kind":   {value.DecisionKind, 40},
		"decision_result": {value.DecisionResult, 40},
		"decision_reason": {value.DecisionReason, 120},
		"capture_gap":     {value.CaptureGap, 80},
	} {
		if field.value != "" && !portableWithin(field.value, field.maximum) {
			return fmt.Errorf("trace fact %s is not a bounded machine token", name)
		}
	}
	declaredHash := value.FactSHA256
	value.FactSHA256 = ""
	if !matchesSemanticHash(value, declaredHash) {
		return errors.New("trace fact_sha256 does not match canonical fact content")
	}
	last := ""
	for _, key := range value.MetadataKeys {
		if !metadataKeyPattern.MatchString(key) || key <= last {
			return errors.New("trace metadata_keys must be safe and strictly sorted")
		}
		last = key
	}
	_, err := parseCanonicalTime("trace fact created_at", value.CreatedAt)
	return err
}

func validateProviderAttemptFact(value ProviderAttemptFact) error {
	if !hashRefPattern.MatchString(value.FactSHA256) || !portableWithin(value.SourceRef, 512) || !portableWithin(value.AttemptID, 64) || value.RouteSequence <= 0 || value.RouteTotal <= 0 || value.RouteSequence > value.RouteTotal || strings.TrimSpace(value.Endpoint) == "" || strings.TrimSpace(value.Provider) == "" || strings.TrimSpace(value.State) == "" || strings.TrimSpace(value.OutcomeCertainty) == "" {
		return errors.New("provider attempt fact is invalid")
	}
	if !exportedProviderFailureKinds[value.FailureKind] {
		return errors.New("provider attempt failure_kind is not an exported machine classification")
	}
	switch value.AccessBindingType {
	case ProviderAttemptAccessBindingLogical:
		if !hashRefPattern.MatchString(value.LogicalCredentialCorrelationSHA256) {
			return errors.New("provider attempt logical credential correlation is invalid")
		}
	case ProviderAttemptAccessBindingLegacy:
		if value.LogicalCredentialCorrelationSHA256 != "" {
			return errors.New("legacy provider attempt must not expose a credential correlation")
		}
	default:
		return errors.New("provider attempt access binding type is invalid")
	}
	declaredHash := value.FactSHA256
	value.FactSHA256 = ""
	if !matchesSemanticHash(value, declaredHash) {
		return errors.New("provider attempt fact_sha256 does not match canonical fact content")
	}
	for _, amount := range []*string{value.CalculatedCostMicros, value.ChargedCostMicros} {
		if amount != nil && !unsignedDecimal(*amount) {
			return errors.New("provider attempt cost is invalid")
		}
	}
	for field, instant := range map[string]string{"started_at": value.StartedAt, "updated_at": value.UpdatedAt} {
		if _, err := parseCanonicalTime("provider attempt "+field, instant); err != nil {
			return err
		}
	}
	for field, instant := range map[string]string{"response_received_at": value.ResponseReceivedAt, "financial_resolved_at": value.FinancialResolvedAt} {
		if instant != "" {
			if _, err := parseCanonicalTime("provider attempt "+field, instant); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateCompletedFeedFact(value CompletedFeedFact) error {
	if !hashRefPattern.MatchString(value.FactSHA256) || !portableWithin(value.SourceRef, 512) || value.SequenceID <= 0 || (value.TerminalStatus != "settled" && value.TerminalStatus != "cancelled") || value.Revision != "1" || !hashRefPattern.MatchString(value.SnapshotSHA256) || strings.TrimSpace(value.ContentSummary.Model) == "" || value.ContentSummary.PromptTokens < 0 || value.ContentSummary.CompletionTokens < 0 || !unsignedDecimal(value.ContentSummary.CalculatedCostMicros) || !unsignedDecimal(value.ContentSummary.ChargedCostMicros) {
		return errors.New("completed feed fact is invalid")
	}
	declaredHash := value.FactSHA256
	value.FactSHA256 = ""
	if !matchesSemanticHash(value, declaredHash) {
		return errors.New("completed feed fact_sha256 does not match canonical fact content")
	}
	_, err := parseCanonicalTime("completed feed terminal_available_at", value.TerminalAvailableAt)
	return err
}

func matchesSemanticHash(value any, declared string) bool {
	calculated, err := SemanticSHA256(value)
	return err == nil && calculated == declared
}

func validateCompatibilityTrace(index int, value CompatibilityTraceFact, closedAt time.Time) error {
	createdAt, err := parseCanonicalTime("execution-evidence-v2 trace created_at", value.CreatedAt)
	if err != nil {
		return err
	}
	if createdAt.After(closedAt) || !portableWithin(value.SourceRef, 512) || !hashPattern.MatchString(value.SourceEventHash) || !portableWithin(value.EventType, 120) || !stagePattern.MatchString(value.Stage) || (value.Severity != "info" && value.Severity != "warning" && value.Severity != "error") || value.PayloadCaptureMode != "metadata_only" || value.LatencyMS < 0 || len(value.Metadata) > 64 {
		return fmt.Errorf("execution-evidence-v2 trace_events[%d] is invalid", index)
	}
	if value.StatusCode != nil && (*value.StatusCode < 0 || *value.StatusCode > 999) {
		return fmt.Errorf("execution-evidence-v2 trace_events[%d] status is invalid", index)
	}
	if value.ActorRef != "" || value.OnBehalfOf != "" {
		return fmt.Errorf("execution-evidence-v2 trace_events[%d] contains a private actor locator", index)
	}
	if value.ActorType != "" && !isSourceActorType(value.ActorType) {
		return fmt.Errorf("execution-evidence-v2 trace_events[%d] actor_type is not allowlisted", index)
	}
	for key, scalar := range value.Metadata {
		if !allowedCompatibilityMetadataKey(key) || !isJSONScalar(scalar) {
			return fmt.Errorf("execution-evidence-v2 trace_events[%d] metadata is not allowlisted scalar evidence", index)
		}
	}
	return nil
}

func validateCompatibilityCall(index int, value CompatibilityCallLogFact, closedAt time.Time) error {
	createdAt, err := parseCanonicalTime("execution-evidence-v2 call created_at", value.CreatedAt)
	if err != nil {
		return err
	}
	if createdAt.After(closedAt) || !portableWithin(value.SourceRef, 512) || !hashPattern.MatchString(value.SourceEventHash) || strings.TrimSpace(value.Model) == "" || strings.TrimSpace(value.Provider) == "" || value.StatusCode < 0 || value.StatusCode > 999 || value.LatencyMS < 0 || value.PromptTokens < 0 || value.CompletionTokens < 0 || !unsignedDecimal(value.CostMicros) || value.Currency != "CNY" || value.AmountScale != 1_000_000 || value.PayloadCaptureMode != "metadata_only" {
		return fmt.Errorf("execution-evidence-v2 call_logs[%d] is invalid", index)
	}
	if value.ActorRef != "" {
		return fmt.Errorf("execution-evidence-v2 call_logs[%d] contains a private actor locator", index)
	}
	if value.ActorType != "" && !isSourceActorType(value.ActorType) {
		return fmt.Errorf("execution-evidence-v2 call_logs[%d] actor_type is not allowlisted", index)
	}
	return nil
}

func canonicalSourceActorType(value string) string {
	if isSourceActorType(value) {
		return value
	}
	return "unknown"
}

func isSourceActorType(value string) bool {
	switch value {
	case "human", "agent", "service", "api_key", "unknown":
		return true
	default:
		return false
	}
}

func canonicalProviderFailureKind(value string) string {
	if canonical, ok := providerFailureKindContract[strings.TrimSpace(value)]; ok {
		return canonical
	}
	return "unclassified"
}

func logicalCredentialCorrelation(credentialRef string) (string, error) {
	if !credentialRefPattern.MatchString(credentialRef) {
		return "", errors.New("logical credential reference is not a canonical source-owned opaque reference")
	}
	digest := sha256.Sum256([]byte(logicalCredentialCorrelationDomain + "\x00" + credentialRef))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func unsignedDecimal(value string) bool {
	if value == "0" {
		return true
	}
	if value == "" || value[0] < '1' || value[0] > '9' {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func isCompatibilityStage(value string) bool {
	for _, stage := range compatibilityStages {
		if value == stage {
			return true
		}
	}
	return false
}

func validCompatibilityGapStage(value string) bool {
	return value == "source" || isCompatibilityStage(value)
}

func normalizedHashes(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func sameNormalizedHashes(left, right []string) bool {
	left = normalizedHashes(left)
	right = normalizedHashes(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !hashPattern.MatchString(left[index]) || left[index] != right[index] || (index > 0 && left[index] == left[index-1]) {
			return false
		}
	}
	return true
}

func allowedCompatibilityMetadataKey(value string) bool {
	switch value {
	case "route_sequence", "route_total", "estimated_tokens", "prompt_tokens", "completion_tokens", "total_tokens", "cost_micros", "currency", "amount_scale", "usage_source", "outbox_version", "materialization_pending":
		return true
	default:
		return false
	}
}

func isJSONScalar(value any) bool {
	switch value.(type) {
	case nil, bool, string, float64, json.Number:
		return true
	default:
		return false
	}
}
