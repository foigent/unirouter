package traceutil

import (
	"strings"
	"testing"
)

func TestSanitizeMetadata(t *testing.T) {
	longUsageSource := strings.Repeat("x", 300)

	metadata := SanitizeMetadata(map[string]interface{}{
		"estimated_tokens": float64(100),
		"route_count":      2,
		"is_stream":        true,
		"usage_source":     " " + longUsageSource + " ",
		"api_key":          "sk-secret",
		"authorization":    "Bearer secret",
		"prompt":           "secret prompt",
		"request_body":     map[string]interface{}{"messages": []interface{}{"secret"}},
		"mapped_model":     map[string]interface{}{"nested": "dropped"},
	})

	if metadata["estimated_tokens"] != float64(100) || metadata["route_count"] != 2 || metadata["is_stream"] != true {
		t.Fatalf("expected safe scalar metadata to remain, got %#v", metadata)
	}
	if usageSource, ok := metadata["usage_source"].(string); !ok || len(usageSource) != 256 || strings.Contains(usageSource, " ") {
		t.Fatalf("expected usage_source to be trimmed and truncated, got %#v", metadata["usage_source"])
	}
	for _, key := range []string{"api_key", "authorization", "prompt", "request_body", "mapped_model"} {
		if _, ok := metadata[key]; ok {
			t.Fatalf("metadata leaked or kept unsupported field %s: %#v", key, metadata)
		}
	}
}

func TestSanitizeMetadataWithGap(t *testing.T) {
	metadata, gap := SanitizeMetadataWithGap(map[string]interface{}{
		"estimated_tokens": 100,
		"usage_source":     "upstream",
	})
	if gap != "" {
		t.Fatalf("expected no capture gap for fully retained safe metadata, got %q", gap)
	}
	if metadata["estimated_tokens"] != 100 || metadata["usage_source"] != "upstream" {
		t.Fatalf("expected safe metadata to remain, got %#v", metadata)
	}

	metadata, gap = SanitizeMetadataWithGap(map[string]interface{}{
		"usage_source": " " + strings.Repeat("x", 300) + " ",
		"api_key":      "sk-secret",
		"mapped_model": map[string]interface{}{"nested": "dropped"},
	})
	if gap != "redacted" {
		t.Fatalf("expected redacted capture gap, got %q", gap)
	}
	if usageSource, ok := metadata["usage_source"].(string); !ok || len(usageSource) != 256 {
		t.Fatalf("expected truncated usage_source, got %#v", metadata["usage_source"])
	}
	if _, ok := metadata["api_key"]; ok {
		t.Fatalf("expected restricted metadata to be redacted, got %#v", metadata)
	}
	if _, ok := metadata["mapped_model"]; ok {
		t.Fatalf("expected unsupported metadata shape to be redacted, got %#v", metadata)
	}
}

func TestSanitizeMetadata_PayloadPolicyFields(t *testing.T) {
	metadata, gap := SanitizeMetadataWithGap(map[string]interface{}{
		"payload_capture_mode":  "metadata_only",
		"payload_policy_reason": "payload.metadata_only",
		"payload_ref_present":   false,
		"raw_payload_stored":    false,
	})
	if gap != "" {
		t.Fatalf("expected no capture gap for payload policy metadata, got %q", gap)
	}
	if metadata["payload_capture_mode"] != "metadata_only" || metadata["payload_policy_reason"] != "payload.metadata_only" {
		t.Fatalf("expected payload policy metadata to remain, got %#v", metadata)
	}
	if metadata["payload_ref_present"] != false || metadata["raw_payload_stored"] != false {
		t.Fatalf("expected payload policy booleans to remain, got %#v", metadata)
	}
}

func TestSanitizeMetadata_PromptCacheBillingFields(t *testing.T) {
	input := map[string]interface{}{
		"cache_scope":               "provider_prompt",
		"cache_usage_authoritative": true,
		"cache_hit":                 true,
		"uncached_prompt_tokens":    10,
		"cache_read_tokens":         20,
		"cache_creation_tokens":     5,
		"cache_usage_schema":        "anthropic.prompt_cache.v1",
		"cache_usage_status":        "cache_read_write_reported",
		"cache_read_ratio":          0.1,
		"cache_creation_ratio":      1.25,
		"cache_read_cost_cents":     0.002,
		"cache_creation_cost_cents": 0.03125,
		"pricing_version":           "pv2_fixture",
		"prepaid_topup_cents":       400,
	}
	metadata, gap := SanitizeMetadataWithGap(input)
	if gap != "" {
		t.Fatalf("expected safe prompt-cache evidence without a capture gap, got %q", gap)
	}
	if len(metadata) != len(input) || metadata["cache_usage_schema"] != "anthropic.prompt_cache.v1" || metadata["cache_read_tokens"] != 20 || metadata["prepaid_topup_cents"] != 400 {
		t.Fatalf("unexpected prompt-cache metadata: %#v", metadata)
	}
}

func TestSanitizeMetadata_RetryDecisionFields(t *testing.T) {
	metadata, gap := SanitizeMetadataWithGap(map[string]interface{}{
		"failure_kind":   "channel_failure",
		"retryable":      true,
		"retry_decision": "retry",
	})
	if gap != "" {
		t.Fatalf("expected no capture gap for retry decision metadata, got %q", gap)
	}
	if metadata["failure_kind"] != "channel_failure" || metadata["retryable"] != true || metadata["retry_decision"] != "retry" {
		t.Fatalf("expected retry decision metadata to remain, got %#v", metadata)
	}
}

func TestSanitizeMetadata_RoutingPolicyFields(t *testing.T) {
	metadata, gap := SanitizeMetadataWithGap(map[string]interface{}{
		"effective_config_version":          "effective-config.v1",
		"routing_config_version":            "routing-config.v1",
		"max_retry_count":                   2,
		"circuit_breaker_threshold":         7,
		"circuit_breaker_recover_seconds":   90,
		"circuit_breaker_half_open_percent": 25,
		"rate_limit_cooldown_seconds":       45,
		"auth_failure_cooldown_seconds":     1800,
		"provider_key_failure_threshold":    6,
	})
	if gap != "" {
		t.Fatalf("expected effective routing policy metadata to remain, got gap %q", gap)
	}
	if len(metadata) != 9 || metadata["effective_config_version"] != "effective-config.v1" || metadata["routing_config_version"] != "routing-config.v1" || metadata["max_retry_count"] != 2 {
		t.Fatalf("unexpected effective routing policy metadata: %#v", metadata)
	}
}

func TestSanitizeMetadata_RuntimeReleaseAndModelAuthorizationFields(t *testing.T) {
	input := map[string]interface{}{
		"runtime_release_ref":             "rcr_company_chat_openai_v2",
		"runtime_release_stage":           "canary",
		"runtime_assignment_id":           "runtime-assignment.v1:request-42",
		"model_authorization_contract":    "unirouter.model-call-authorization.v1",
		"model_authorization_result":      "allow",
		"model_authorization_reason":      "allow_intersection",
		"model_authorization_policy_refs": "mca_project_company_chat,mca_user_finance",
		"model_authorization_environment": "production",
	}
	metadata, gap := SanitizeMetadataWithGap(input)
	if gap != "" || len(metadata) != len(input) {
		t.Fatalf("runtime governance evidence was dropped: gap=%q metadata=%#v", gap, metadata)
	}
	for key, value := range input {
		if metadata[key] != value {
			t.Fatalf("metadata[%q] = %#v, want %#v", key, metadata[key], value)
		}
	}
}

func TestSanitizeMetadata_StreamPolicyFields(t *testing.T) {
	metadata, gap := SanitizeMetadataWithGap(map[string]interface{}{
		"stream_policy_version":     "stream-transport.v1",
		"stream_max_sse_line_bytes": 2 * 1024 * 1024,
	})
	if gap != "" {
		t.Fatalf("expected stream policy metadata to remain, got gap %q", gap)
	}
	if len(metadata) != 2 || metadata["stream_policy_version"] != "stream-transport.v1" || metadata["stream_max_sse_line_bytes"] != 2*1024*1024 {
		t.Fatalf("unexpected stream policy metadata: %#v", metadata)
	}
}

func TestSanitizeMetadata_ModelExecutionProfileEvidence(t *testing.T) {
	input := map[string]interface{}{
		"execution_profile_id":               "deepseek.v4-flash.api",
		"execution_profile_version":          "1.0.0",
		"reasoning_profile":                  "reasoning.deepseek-thinking-toggle.v1",
		"reasoning_mode":                     "toggle",
		"reasoning_request":                  "omitted",
		"reasoning_provider_default":         "enabled",
		"reasoning_default_authority":        "provider_owned",
		"reasoning_toggle_request":           "omitted",
		"reasoning_thinking_keep_request":    "omitted",
		"reasoning_thinking_keep_default":    "all",
		"reasoning_effort_request":           "omitted",
		"reasoning_effort_effective":         "omitted",
		"cache_usage_profile":                "deepseek.prompt_cache.v1",
		"processing_tier_policy":             "processing-tier.gemini-openai-compatibility.v1",
		"processing_tier_request":            "priority",
		"processing_tier_effective":          "priority",
		"processing_tier_timeout_seconds":    300,
		"processing_tier_explicit":           true,
		"processing_tier_observation":        "observed",
		"processing_tier_observation_source": "response_header:x-gemini-service-tier",
		"processing_tier_observed":           "standard",
		"processing_tier_disposition":        "provider_downgraded",
		"provider_request_normalizations":    "tool_choice=auto->omitted",
	}
	metadata, gap := SanitizeMetadataWithGap(input)
	if gap != "" || len(metadata) != len(input) {
		t.Fatalf("model execution profile evidence was dropped: gap=%q metadata=%#v", gap, metadata)
	}
	for key, value := range input {
		if metadata[key] != value {
			t.Fatalf("metadata[%q] = %#v, want %#v", key, metadata[key], value)
		}
	}
}

func TestSanitizeMetadata_GuardrailFieldsRemainBoundedScalars(t *testing.T) {
	input := map[string]interface{}{
		"guardrail_policy_set_version":   "a1b2c3d4",
		"guardrail_policy_ref":           "guardrail:7",
		"guardrail_policy_version":       int64(3),
		"guardrail_detector_ref":         "builtin:pii_secrets",
		"guardrail_detector_version":     "unirouter.local-pii-secrets.v1",
		"guardrail_mode":                 "enforce",
		"guardrail_action":               "redact",
		"guardrail_outcome":              "transform",
		"guardrail_failure_mode":         "degraded_allow",
		"guardrail_mandatory":            false,
		"guardrail_match_count":          1,
		"guardrail_categories":           "email=1",
		"guardrail_finding_fingerprints": "4c0c9d8baab960610550d32d",
		"guardrail_decision":             "allow",
		"guardrail_transformed":          true,
		"guardrail_degraded":             false,
	}
	metadata, gap := SanitizeMetadataWithGap(input)
	if gap != "" || len(metadata) != len(input) {
		t.Fatalf("guardrail evidence was dropped: gap=%q metadata=%#v", gap, metadata)
	}
	if _, exists := metadata["prompt"]; exists {
		t.Fatal("raw prompt unexpectedly entered guardrail metadata")
	}
}
