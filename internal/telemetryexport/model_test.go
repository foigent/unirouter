package telemetryexport

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func fixedSourceEvent() SourceEvent {
	return SourceEvent{
		RequestID:          "op_00000000000000000000000000000001",
		OrgID:              42,
		ProjectID:          7,
		EventType:          "provider.response_accepted",
		Stage:              "provider",
		Severity:           "info",
		Provider:           "openai",
		StatusCode:         200,
		Latency:            125 * time.Millisecond,
		TraceID:            "op_00000000000000000000000000000001",
		SpanID:             "0123456789abcdef",
		ParentSpanID:       "fedcba9876543210",
		SchemaVersion:      "unirouter.evidence.v1",
		SourceComponent:    "provider",
		CaptureMode:        "native",
		CaptureGap:         "provider_not_returned",
		PayloadCaptureMode: "metadata_only",
		SourceEventHash:    strings.Repeat("a", 64),
		DecisionKind:       "provider",
		DecisionResult:     "selected",
		DecisionReason:     "provider.response_accepted",
		OperationKind:      OperationChatCompletion,
		OccurredAt:         time.Date(2026, 7, 31, 10, 11, 12, 123456000, time.UTC),
		Metadata: map[string]any{
			"mapped_model":        "gpt-4o-2026-07-01",
			"prompt_tokens":       json.Number("12"),
			"completion_tokens":   json.Number("7"),
			"total_tokens":        json.Number("19"),
			"estimated_tokens":    20,
			"stream":              false,
			"pricing_version":     "must-not-be-exported",
			"raw_payload_stored":  false,
			"payload_ref_present": false,
		},
	}
}

func TestBuildSpanProducesDeterministicMetadataOnlyOpenInferenceOTLP(t *testing.T) {
	event := fixedSourceEvent()
	record, err := buildSpan(event)
	if err != nil {
		t.Fatalf("build span: %v", err)
	}
	if len(record.TraceID) != 32 || record.SpanID != event.SpanID || record.ParentSpanID != event.ParentSpanID {
		t.Fatalf("unexpected normalized identifiers: trace=%s span=%s parent=%s", record.TraceID, record.SpanID, record.ParentSpanID)
	}
	if len(record.Attributes) > maxAttributeCount {
		t.Fatalf("attribute count exceeded bound: %d", len(record.Attributes))
	}
	attributes := attributeMap(record.Attributes)
	assertStringAttribute(t, attributes, "openinference.span.kind", "LLM")
	assertStringAttribute(t, attributes, "llm.provider", "openai")
	assertStringAttribute(t, attributes, "llm.model_name", "gpt-4o-2026-07-01")
	assertIntAttribute(t, attributes, "llm.token_count.prompt", "12")
	assertIntAttribute(t, attributes, "llm.token_count.completion", "7")
	if _, exists := attributes["pricing_version"]; exists {
		t.Fatal("non-allowlisted metadata was exported")
	}
	gaps := attributes["unirouter.export.capture_gaps"].ArrayValue
	if gaps == nil || !arrayContains(gaps.Values, "metadata_not_allowlisted") || !arrayContains(gaps.Values, "raw_content_policy_excluded") {
		t.Fatalf("missing explicit export gaps: %#v", gaps)
	}

	first, err := marshalBatch([]spanRecord{record}, resourceConfig{
		ServiceName:      "unirouter",
		ServiceNamespace: "foigent",
		ServiceVersion:   "1.2.3",
		DeploymentEnv:    "test",
	})
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}
	second, err := marshalBatch([]spanRecord{record}, resourceConfig{
		ServiceName:      "unirouter",
		ServiceNamespace: "foigent",
		ServiceVersion:   "1.2.3",
		DeploymentEnv:    "test",
	})
	if err != nil {
		t.Fatalf("marshal batch twice: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same source event produced different OTLP JSON bytes")
	}
	golden, err := os.ReadFile("testdata/chat-provider-otlp.json")
	if err != nil {
		t.Fatalf("read OTLP golden: %v", err)
	}
	golden = bytes.TrimSpace(golden)
	if !bytes.Equal(first, golden) {
		t.Fatalf("OTLP JSON differs from golden\nactual: %s\nwant:   %s", first, golden)
	}
	if bytes.Contains(first, []byte("must-not-be-exported")) ||
		bytes.Contains(first, []byte(event.RequestID)) {
		t.Fatalf("OTLP payload leaked non-allowlisted or raw source identity: %s", first)
	}
	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("OTLP JSON is invalid: %v", err)
	}
}

func TestBuildSpanRejectsRestrictedNestedContentAndUnboundedShape(t *testing.T) {
	event := fixedSourceEvent()
	event.Metadata["safe_wrapper"] = map[string]any{
		"nested": map[string]any{"input.value": "customer secret"},
	}
	if _, err := buildSpan(event); !errors.Is(err, ErrRestrictedContent) {
		t.Fatalf("expected restricted content rejection, got %v", err)
	}

	event = fixedSourceEvent()
	nested := map[string]any{}
	cursor := nested
	for i := 0; i < maxMetadataDepth+2; i++ {
		next := map[string]any{}
		cursor["safe"] = next
		cursor = next
	}
	event.Metadata = nested
	if _, err := buildSpan(event); !errors.Is(err, ErrInvalidSourceEvent) {
		t.Fatalf("expected metadata depth rejection, got %v", err)
	}
}

func TestBuildSpanUsesEmbeddingSemanticsWithoutGuessingFromModelName(t *testing.T) {
	event := fixedSourceEvent()
	event.OperationKind = OperationEmbedding
	event.Metadata["mapped_model"] = "text-embedding-3-small"
	record, err := buildSpan(event)
	if err != nil {
		t.Fatalf("build embedding span: %v", err)
	}
	attributes := attributeMap(record.Attributes)
	assertStringAttribute(t, attributes, "openinference.span.kind", "EMBEDDING")
	assertStringAttribute(t, attributes, "embedding.model_name", "text-embedding-3-small")
	if _, exists := attributes["llm.model_name"]; exists {
		t.Fatal("embedding event silently emitted LLM model semantics")
	}

	event.OperationKind = ""
	record, err = buildSpan(event)
	if err != nil {
		t.Fatalf("build unknown-operation span: %v", err)
	}
	attributes = attributeMap(record.Attributes)
	assertStringAttribute(t, attributes, "openinference.span.kind", "CHAIN")

	event = fixedSourceEvent()
	event.EventType = "provider.key_decrypt_failed"
	record, err = buildSpan(event)
	if err != nil {
		t.Fatalf("build pre-dispatch provider span: %v", err)
	}
	attributes = attributeMap(record.Attributes)
	assertStringAttribute(t, attributes, "openinference.span.kind", "CHAIN")
	if _, exists := attributes["llm.model_name"]; exists {
		t.Fatal("pre-dispatch provider failure was presented as an observed LLM call")
	}
	gaps := attributes["unirouter.export.capture_gaps"].ArrayValue
	if gaps == nil || !arrayContains(gaps.Values, "provider_execution_not_observed") {
		t.Fatalf("missing pre-dispatch operation gap: %#v", gaps)
	}
	if record.StatusCode != 2 {
		t.Fatalf("pre-dispatch provider failure must have OTLP error status, got %d", record.StatusCode)
	}
}

func TestBuildSpanDropsUnsafeCardinalityValuesAndNonIntegralCounters(t *testing.T) {
	event := fixedSourceEvent()
	event.Provider = "provider with spaces"
	event.Metadata = map[string]any{
		"mapped_model":  strings.Repeat("m", 200),
		"prompt_tokens": 1.5,
	}
	record, err := buildSpan(event)
	if err != nil {
		t.Fatalf("build span: %v", err)
	}
	attributes := attributeMap(record.Attributes)
	if _, exists := attributes["llm.model_name"]; exists {
		t.Fatal("overlong model cardinality value was exported")
	}
	if _, exists := attributes["llm.provider"]; exists {
		t.Fatal("non-portable provider cardinality value was exported")
	}
	if _, exists := attributes["llm.token_count.prompt"]; exists {
		t.Fatal("fractional token count was exported as an integer")
	}
	gaps := attributes["unirouter.export.capture_gaps"].ArrayValue
	if gaps == nil ||
		!arrayContains(gaps.Values, "attribute_value_exceeded") ||
		!arrayContains(gaps.Values, "attribute_value_not_portable") ||
		!arrayContains(gaps.Values, "metadata_type_invalid") {
		t.Fatalf("missing cardinality/type gaps: %#v", gaps)
	}
}

func TestExactTokenCountersPreserveInt64BoundaryWithoutFloatCoercion(t *testing.T) {
	if value, ok := exactNonNegativeInt64(json.Number("9223372036854775807")); !ok || value != int64(9223372036854775807) {
		t.Fatalf("int64 maximum was not preserved exactly: value=%d ok=%t", value, ok)
	}
	for _, value := range []any{
		json.Number("9223372036854775808"),
		json.Number("-1"),
		json.Number("1.5"),
		float64(1.5),
		float64(9007199254740992),
		uint64(^uint64(0)),
	} {
		if parsed, ok := exactNonNegativeInt64(value); ok {
			t.Fatalf("unsafe counter %#v was accepted as %d", value, parsed)
		}
	}
	if value, ok := exactNonNegativeInt64(float64(9007199254740991)); !ok || value != 9007199254740991 {
		t.Fatalf("maximum safe float integer was not preserved: value=%d ok=%t", value, ok)
	}
}

func TestBuildSpanCapsAttributesAndDeclaresOverflow(t *testing.T) {
	event := fixedSourceEvent()
	event.Metadata["uncached_prompt_tokens"] = 9
	event.Metadata["cache_read_tokens"] = 2
	event.Metadata["cache_creation_tokens"] = 3
	record, err := buildSpan(event)
	if err != nil {
		t.Fatalf("build attribute-boundary span: %v", err)
	}
	if len(record.Attributes) != maxAttributeCount {
		t.Fatalf("expected exact attribute cap %d, got %d", maxAttributeCount, len(record.Attributes))
	}
	gaps := attributeMap(record.Attributes)["unirouter.export.capture_gaps"].ArrayValue
	if gaps == nil || !arrayContains(gaps.Values, "attribute_count_exceeded") {
		t.Fatalf("attribute overflow was not declared: %#v", gaps)
	}
}

func attributeMap(attributes []keyValue) map[string]anyValue {
	result := make(map[string]anyValue, len(attributes))
	for _, attribute := range attributes {
		result[attribute.Key] = attribute.Value
	}
	return result
}

func assertStringAttribute(t *testing.T, attributes map[string]anyValue, key, expected string) {
	t.Helper()
	if actual := attributes[key].StringValue; actual != expected {
		t.Fatalf("%s: expected %q, got %q", key, expected, actual)
	}
}

func assertIntAttribute(t *testing.T, attributes map[string]anyValue, key, expected string) {
	t.Helper()
	if actual := attributes[key].IntValue; actual != expected {
		t.Fatalf("%s: expected %q, got %q", key, expected, actual)
	}
}

func arrayContains(values []anyValue, expected string) bool {
	for _, value := range values {
		if value.StringValue == expected {
			return true
		}
	}
	return false
}
