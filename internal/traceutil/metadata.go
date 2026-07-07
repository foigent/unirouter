package traceutil

import "strings"

var metadataAllowlist = map[string]bool{
	"billing_status":      true,
	"charged_cost_cents":  true,
	"completion_tokens":   true,
	"cost_cents":          true,
	"error_code":          true,
	"estimated_tokens":    true,
	"failure_kind":        true,
	"freeze_amount_cents": true,
	"is_stream":           true,
	"mapped_model":        true,
	"prompt_tokens":       true,
	"route_count":         true,
	"route_index":         true,
	"settle_cancelled":    true,
	"stream":              true,
	"total_tokens":        true,
	"ttft_ms":             true,
	"usage_confidence":    true,
	"usage_source":        true,
}

func SanitizeMetadata(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return map[string]interface{}{}
	}

	out := make(map[string]interface{})
	for key, value := range input {
		if !metadataAllowlist[key] {
			continue
		}
		switch v := value.(type) {
		case string:
			out[key] = truncateString(v)
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
			continue
		}
	}
	return out
}

func truncateString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 256 {
		return value
	}
	return value[:256]
}
