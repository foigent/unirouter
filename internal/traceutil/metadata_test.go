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
