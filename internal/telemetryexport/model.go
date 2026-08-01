package telemetryexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	SchemaVersion = "unirouter.telemetry-export.v1"

	OperationChatCompletion = "chat_completion"
	OperationEmbedding      = "embedding"

	maxAttributeCount = 32
	maxStringBytes    = 256
	maxMetadataNodes  = 256
	maxMetadataDepth  = 8
	maxLatency        = 24 * time.Hour
	maxSafeFloatInt   = 1<<53 - 1
)

var (
	ErrRestrictedContent  = errors.New("telemetry export rejected restricted content")
	ErrInvalidSourceEvent = errors.New("telemetry export rejected invalid source event")

	tokenValuePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$`)
	hashPattern       = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// SourceEvent is an immutable, metadata-only projection of one persisted
// UniRouter request trace fact. It deliberately excludes payload bodies,
// credentials, actor references, channel names, upstream request identifiers
// and cursor/checkpoint state.
type SourceEvent struct {
	RequestID          string
	OrgID              int64
	ProjectID          int64
	EventType          string
	Stage              string
	Severity           string
	Provider           string
	StatusCode         int
	Latency            time.Duration
	TraceID            string
	SpanID             string
	ParentSpanID       string
	SchemaVersion      string
	SourceComponent    string
	CaptureMode        string
	CaptureGap         string
	PayloadCaptureMode string
	SourceEventHash    string
	DecisionKind       string
	DecisionResult     string
	DecisionReason     string
	OperationKind      string
	OccurredAt         time.Time
	Metadata           map[string]any
}

type anyValue struct {
	StringValue string      `json:"stringValue,omitempty"`
	BoolValue   *bool       `json:"boolValue,omitempty"`
	IntValue    string      `json:"intValue,omitempty"`
	ArrayValue  *arrayValue `json:"arrayValue,omitempty"`
}

type arrayValue struct {
	Values []anyValue `json:"values"`
}

type keyValue struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type spanRecord struct {
	TraceID           string
	SpanID            string
	ParentSpanID      string
	Name              string
	StartTimeUnixNano string
	EndTimeUnixNano   string
	Attributes        []keyValue
	StatusCode        int
}

type attributeBuilder struct {
	values map[string]anyValue
	gaps   map[string]struct{}
}

func newAttributeBuilder() *attributeBuilder {
	return &attributeBuilder{
		values: make(map[string]anyValue, maxAttributeCount),
		gaps: map[string]struct{}{
			"raw_content_policy_excluded": {},
		},
	}
}

func (b *attributeBuilder) addString(key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if len(value) > maxStringBytes {
		b.gaps["attribute_value_exceeded"] = struct{}{}
		return
	}
	if len(b.values) >= maxAttributeCount-1 {
		b.gaps["attribute_count_exceeded"] = struct{}{}
		return
	}
	b.values[key] = anyValue{StringValue: value}
}

func (b *attributeBuilder) addToken(key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if len(value) > 128 {
		b.gaps["attribute_value_exceeded"] = struct{}{}
		return
	}
	if !tokenValuePattern.MatchString(value) {
		b.gaps["attribute_value_not_portable"] = struct{}{}
		return
	}
	b.addString(key, value)
}

func (b *attributeBuilder) addInt(key string, value int64) {
	if len(b.values) >= maxAttributeCount-1 {
		b.gaps["attribute_count_exceeded"] = struct{}{}
		return
	}
	b.values[key] = anyValue{IntValue: strconv.FormatInt(value, 10)}
}

func (b *attributeBuilder) addBool(key string, value bool) {
	if len(b.values) >= maxAttributeCount-1 {
		b.gaps["attribute_count_exceeded"] = struct{}{}
		return
	}
	copied := value
	b.values[key] = anyValue{BoolValue: &copied}
}

func (b *attributeBuilder) addGaps() {
	values := make([]string, 0, len(b.gaps))
	for gap := range b.gaps {
		values = append(values, gap)
	}
	sort.Strings(values)
	array := make([]anyValue, 0, len(values))
	for _, gap := range values {
		array = append(array, anyValue{StringValue: gap})
	}
	b.values["unirouter.export.capture_gaps"] = anyValue{
		ArrayValue: &arrayValue{Values: array},
	}
}

func (b *attributeBuilder) sorted() []keyValue {
	keys := make([]string, 0, len(b.values))
	for key := range b.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	attributes := make([]keyValue, 0, len(keys))
	for _, key := range keys {
		attributes = append(attributes, keyValue{Key: key, Value: b.values[key]})
	}
	return attributes
}

func buildSpan(event SourceEvent) (spanRecord, error) {
	if event.OrgID <= 0 ||
		event.ProjectID < 0 ||
		strings.TrimSpace(event.RequestID) == "" ||
		len(event.RequestID) > maxStringBytes ||
		strings.TrimSpace(event.TraceID) == "" ||
		len(event.TraceID) > maxStringBytes ||
		strings.TrimSpace(event.SpanID) == "" ||
		len(event.SpanID) > maxStringBytes ||
		len(event.ParentSpanID) > maxStringBytes ||
		!tokenValuePattern.MatchString(strings.TrimSpace(event.EventType)) ||
		!tokenValuePattern.MatchString(strings.TrimSpace(event.Stage)) ||
		!tokenValuePattern.MatchString(strings.TrimSpace(event.SourceComponent)) ||
		event.StatusCode < 0 ||
		event.StatusCode > 999 ||
		event.OccurredAt.IsZero() ||
		event.OccurredAt.Before(time.Unix(0, 0)) ||
		event.OccurredAt.After(time.Unix(0, math.MaxInt64)) ||
		event.Latency < 0 ||
		event.Latency > maxLatency {
		return spanRecord{}, ErrInvalidSourceEvent
	}
	if err := rejectRestrictedMetadata(event.Metadata); err != nil {
		return spanRecord{}, err
	}

	traceID := normalizeTraceID(event.TraceID)
	spanID := normalizeSpanID(event.SpanID)
	parentSpanID := ""
	if strings.TrimSpace(event.ParentSpanID) != "" {
		parentSpanID = normalizeSpanID(event.ParentSpanID)
	}
	end := event.OccurredAt.UTC()
	start := end.Add(-event.Latency)

	attributes := newAttributeBuilder()
	attributes.addString("openinference.span.kind", openInferenceSpanKind(event))
	attributes.addString("unirouter.export.schema_version", SchemaVersion)
	attributes.addToken("unirouter.source.schema_version", event.SchemaVersion)
	attributes.addToken("unirouter.event.type", event.EventType)
	attributes.addToken("unirouter.event.stage", event.Stage)
	attributes.addToken("unirouter.event.severity", normalizeSeverity(event.Severity))
	attributes.addToken("unirouter.source.component", event.SourceComponent)
	attributes.addToken("unirouter.source.capture_mode", event.CaptureMode)
	attributes.addToken("unirouter.payload.capture_mode", event.PayloadCaptureMode)
	if event.CaptureGap == "" {
		attributes.addString("unirouter.source.capture_gap", "none")
	} else {
		attributes.addToken("unirouter.source.capture_gap", event.CaptureGap)
	}
	if hashPattern.MatchString(strings.TrimSpace(event.SourceEventHash)) {
		attributes.addString("unirouter.source.event_hash", event.SourceEventHash)
	} else {
		attributes.gaps["source_event_hash_unavailable"] = struct{}{}
	}
	attributes.addString("unirouter.request.ref_sha256", requestReference(event))
	attributes.addInt("unirouter.scope.org_id", event.OrgID)
	if event.ProjectID > 0 {
		attributes.addInt("unirouter.scope.project_id", event.ProjectID)
	}
	if event.StatusCode > 0 {
		attributes.addInt("http.response.status_code", int64(event.StatusCode))
	}
	attributes.addInt("unirouter.latency_ms", event.Latency.Milliseconds())
	addModelAttributes(attributes, event)
	attributes.addToken("unirouter.decision.kind", event.DecisionKind)
	attributes.addToken("unirouter.decision.result", event.DecisionResult)
	attributes.addToken("unirouter.decision.reason", event.DecisionReason)
	addAllowlistedMetadata(attributes, event)
	attributes.addGaps()

	statusCode := 1
	if eventHasErrorStatus(event) {
		statusCode = 2
	}
	return spanRecord{
		TraceID:           traceID,
		SpanID:            spanID,
		ParentSpanID:      parentSpanID,
		Name:              "unirouter." + strings.TrimSpace(event.EventType),
		StartTimeUnixNano: strconv.FormatInt(start.UnixNano(), 10),
		EndTimeUnixNano:   strconv.FormatInt(end.UnixNano(), 10),
		Attributes:        attributes.sorted(),
		StatusCode:        statusCode,
	}, nil
}

func eventHasErrorStatus(event SourceEvent) bool {
	if normalizeSeverity(event.Severity) == "error" || event.StatusCode >= 400 {
		return true
	}
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	for _, suffix := range []string{
		".failed",
		"_failed",
		".rejected",
		".denied",
		".unusable",
		".no_route",
	} {
		if strings.HasSuffix(eventType, suffix) {
			return true
		}
	}
	return false
}

func openInferenceSpanKind(event SourceEvent) string {
	if strings.EqualFold(event.SourceComponent, "guardrail") {
		return "GUARDRAIL"
	}
	if strings.EqualFold(event.SourceComponent, "provider") && providerExecutionObserved(event.EventType) {
		switch event.OperationKind {
		case OperationChatCompletion:
			return "LLM"
		case OperationEmbedding:
			return "EMBEDDING"
		}
	}
	return "CHAIN"
}

func providerExecutionObserved(eventType string) bool {
	switch eventType {
	case "provider.request_failed",
		"provider.response_rejected",
		"provider.response_parse_failed",
		"provider.response_unusable",
		"provider.response_accepted",
		"provider.usage.observed":
		return true
	default:
		return false
	}
}

func addModelAttributes(attributes *attributeBuilder, event SourceEvent) {
	kind := openInferenceSpanKind(event)
	mappedModel, _ := event.Metadata["mapped_model"].(string)
	switch kind {
	case "LLM":
		attributes.addToken("llm.provider", normalizeProvider(event.Provider))
		attributes.addToken("llm.system", modelSystem(event.Provider))
		if strings.TrimSpace(mappedModel) == "" {
			attributes.gaps["provider_model_unavailable"] = struct{}{}
		} else {
			attributes.addToken("llm.model_name", mappedModel)
		}
	case "EMBEDDING":
		if strings.TrimSpace(mappedModel) == "" {
			attributes.gaps["provider_model_unavailable"] = struct{}{}
		} else {
			attributes.addToken("embedding.model_name", mappedModel)
		}
	default:
		if strings.EqualFold(event.SourceComponent, "provider") {
			attributes.gaps["provider_execution_not_observed"] = struct{}{}
		}
	}
	attributes.addToken("unirouter.provider", normalizeProvider(event.Provider))
	attributes.addToken("unirouter.operation.kind", event.OperationKind)
}

func addAllowlistedMetadata(attributes *attributeBuilder, event SourceEvent) {
	unknown := false
	for key, value := range event.Metadata {
		switch key {
		case "estimated_tokens":
			addMetadataInt(attributes, "unirouter.token_count.estimated", value)
		case "prompt_tokens":
			if event.OperationKind == OperationChatCompletion {
				addMetadataInt(attributes, "llm.token_count.prompt", value)
			} else {
				addMetadataInt(attributes, "unirouter.token_count.input", value)
			}
		case "completion_tokens":
			if event.OperationKind == OperationChatCompletion {
				addMetadataInt(attributes, "llm.token_count.completion", value)
			} else {
				addMetadataInt(attributes, "unirouter.token_count.output", value)
			}
		case "total_tokens":
			addMetadataInt(attributes, "unirouter.token_count.total", value)
		case "uncached_prompt_tokens":
			addMetadataInt(attributes, "unirouter.token_count.prompt_uncached", value)
		case "cache_read_tokens":
			addMetadataInt(attributes, "llm.token_count.prompt_details.cache_read", value)
		case "cache_creation_tokens":
			addMetadataInt(attributes, "llm.token_count.prompt_details.cache_write", value)
		case "stream":
			if parsed, ok := value.(bool); ok {
				attributes.addBool("unirouter.stream", parsed)
			} else {
				attributes.gaps["metadata_type_invalid"] = struct{}{}
			}
		case "raw_payload_stored":
			if parsed, ok := value.(bool); ok {
				attributes.addBool("unirouter.raw_payload_stored", parsed)
			} else {
				attributes.gaps["metadata_type_invalid"] = struct{}{}
			}
		case "payload_ref_present":
			if parsed, ok := value.(bool); ok {
				attributes.addBool("unirouter.payload_ref_present", parsed)
			} else {
				attributes.gaps["metadata_type_invalid"] = struct{}{}
			}
		case "mapped_model":
			// Consumed by addModelAttributes only for a provider span. It is
			// never emitted as an arbitrary dynamic attribute.
		default:
			unknown = true
		}
	}
	if unknown {
		attributes.gaps["metadata_not_allowlisted"] = struct{}{}
	}
}

func addMetadataInt(attributes *attributeBuilder, key string, value any) {
	parsed, ok := exactNonNegativeInt64(value)
	if !ok {
		attributes.gaps["metadata_type_invalid"] = struct{}{}
		return
	}
	attributes.addInt(key, parsed)
}

func exactNonNegativeInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		if typed < 0 {
			return 0, false
		}
		return int64(typed), true
	case int8:
		return exactNonNegativeInt64(int64(typed))
	case int16:
		return exactNonNegativeInt64(int64(typed))
	case int32:
		return exactNonNegativeInt64(int64(typed))
	case int64:
		if typed < 0 {
			return 0, false
		}
		return typed, true
	case uint:
		if uint64(typed) > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		if err != nil || parsed < 0 {
			return 0, false
		}
		return parsed, true
	case float64:
		if typed < 0 || typed > maxSafeFloatInt || math.Trunc(typed) != typed {
			return 0, false
		}
		return int64(typed), true
	default:
		return 0, false
	}
}

func rejectRestrictedMetadata(metadata map[string]any) error {
	nodes := 0
	var walk func(any, int) error
	walk = func(value any, depth int) error {
		if depth > maxMetadataDepth {
			return ErrInvalidSourceEvent
		}
		nodes++
		if nodes > maxMetadataNodes {
			return ErrInvalidSourceEvent
		}
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if len(key) > maxStringBytes {
					return ErrInvalidSourceEvent
				}
				if isRestrictedKey(key) {
					return fmt.Errorf("%w: key=%s", ErrRestrictedContent, key)
				}
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range typed {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		case string:
			if len(typed) > maxStringBytes {
				return ErrInvalidSourceEvent
			}
		case json.Number:
			if len(typed) > 32 {
				return ErrInvalidSourceEvent
			}
		}
		return nil
	}
	return walk(metadata, 0)
}

func isRestrictedKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	switch normalized {
	case "authorization", "api_key", "access_token", "refresh_token", "secret", "password",
		"request_body", "response_body", "body", "prompt", "prompts", "messages", "message",
		"content", "input", "output", "input.value", "output.value", "llm.input_messages",
		"llm.output_messages", "llm.prompts", "llm.choices", "llm.invocation_parameters",
		"tool_arguments", "arguments":
		return true
	default:
		return false
	}
}

func normalizeSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error", "critical":
		return "error"
	case "warning":
		return "warning"
	default:
		return "info"
	}
}

func normalizeProvider(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func modelSystem(provider string) string {
	normalized := normalizeProvider(provider)
	switch normalized {
	case "azure", "azure-openai", "azure_openai":
		return "openai"
	default:
		return normalized
	}
}

func requestReference(event SourceEvent) string {
	input := strconv.FormatInt(event.OrgID, 10) + "\x00" +
		strconv.FormatInt(event.ProjectID, 10) + "\x00" +
		event.RequestID
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func normalizeTraceID(value string) string {
	if normalized, ok := portableHexID(value, 32); ok {
		return normalized
	}
	sum := sha256.Sum256([]byte("unirouter:otel:trace:" + value))
	return hex.EncodeToString(sum[:16])
}

func normalizeSpanID(value string) string {
	if normalized, ok := portableHexID(value, 16); ok {
		return normalized
	}
	sum := sha256.Sum256([]byte("unirouter:otel:span:" + value))
	return hex.EncodeToString(sum[:8])
}

func portableHexID(value string, length int) (string, bool) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if len(normalized) != length {
		return "", false
	}
	decoded, err := hex.DecodeString(normalized)
	if err != nil {
		return "", false
	}
	allZero := true
	for _, b := range decoded {
		if b != 0 {
			allZero = false
			break
		}
	}
	return normalized, !allZero
}
