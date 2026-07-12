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
