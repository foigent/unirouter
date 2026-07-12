package traceutil

import "strings"

var metadataAllowlist = map[string]bool{
	"billing_status":                    true,
	"auth_failure_cooldown_seconds":     true,
	"circuit_breaker_half_open_percent": true,
	"circuit_breaker_recover_seconds":   true,
	"circuit_breaker_threshold":         true,
	"charged_cost_cents":                true,
	"completion_tokens":                 true,
	"cost_cents":                        true,
	"effective_config_version":          true,
	"error_code":                        true,
	"estimated_tokens":                  true,
	"failure_kind":                      true,
	"freeze_amount_cents":               true,
	"is_stream":                         true,
	"mapped_model":                      true,
	"max_retry_count":                   true,
	"payload_capture_mode":              true,
	"payload_policy_reason":             true,
	"payload_ref_present":               true,
	"prompt_tokens":                     true,
	"raw_payload_stored":                true,
	"rate_limit_cooldown_seconds":       true,
	"route_count":                       true,
	"route_index":                       true,
	"routing_config_version":            true,
	"routing_policy_version":            true,
	"routing_snapshot_version":          true,
	"routing_snapshot_source":           true,
	"routing_snapshot_degraded":         true,
	"routing_candidate_count":           true,
	"routing_excluded_count":            true,
	"routing_exclusion_reasons":         true,
	"routing_priority":                  true,
	"routing_weight":                    true,
	"routing_selection_reason":          true,
	"routing_requires_probe":            true,
	"retry_decision":                    true,
	"retryable":                         true,
	"settle_cancelled":                  true,
	"stream":                            true,
	"stream_max_sse_line_bytes":         true,
	"stream_policy_version":             true,
	"total_tokens":                      true,
	"ttft_ms":                           true,
	"usage_confidence":                  true,
	"usage_source":                      true,
	"provider_key_failure_threshold":    true,
	"limit_policy_set_version":          true,
	"limit_policy_result":               true,
	"limit_policy_reason":               true,
	"limit_policy_decision_count":       true,
	"limit_policy_enforce_count":        true,
	"limit_policy_shadow_count":         true,
	"limit_policy_chain":                true,
}

func SanitizeMetadata(input map[string]interface{}) map[string]interface{} {
	out, _ := SanitizeMetadataWithGap(input)
	return out
}

func SanitizeMetadataWithGap(input map[string]interface{}) (map[string]interface{}, string) {
	if len(input) == 0 {
		return map[string]interface{}{}, ""
	}

	out := make(map[string]interface{})
	captureGap := ""
	for key, value := range input {
		if !metadataAllowlist[key] {
			captureGap = "redacted"
			continue
		}
		switch v := value.(type) {
		case string:
			sanitized, truncated := truncateString(v)
			out[key] = sanitized
			if truncated {
				captureGap = "redacted"
			}
		case bool:
			out[key] = v
		case int:
			out[key] = v
		case int64:
			out[key] = v
		case int32:
			out[key] = v
		case float64:
			out[key] = v
		case float32:
			out[key] = v
		default:
			captureGap = "redacted"
			continue
		}
	}
	return out, captureGap
}

func truncateString(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) <= 256 {
		return value, false
	}
	return value[:256], true
}
