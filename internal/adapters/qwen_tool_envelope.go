package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/uniroute/unirouter/internal/strictjson"
)

// Qwen v7 projects only explicitly modeled no-fact fields. It neither creates
// tool identities nor changes counters. The shared state owns assembly/order.
func prepareQwenToolEnvelope(data []byte, expectedModel string) ([]byte, error) {
	if err := strictjson.Validate(data); err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || root == nil {
		return nil, fmt.Errorf("invalid Qwen stream envelope")
	}
	var model string
	if json.Unmarshal(root["model"], &model) != nil || model != expectedModel {
		return nil, fmt.Errorf("Qwen stream model mismatch")
	}
	// Once strict JSON and exact model identity are known, delivery errors
	// preserve the original bytes for independent supplier usage validation.
	// They still prohibit forwarding any part of this frame.
	removeNull := func(object map[string]json.RawMessage, fields ...string) error {
		for _, field := range fields {
			if raw, present := object[field]; present {
				if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					return fmt.Errorf("unmodeled non-null Qwen %s", field)
				}
				delete(object, field)
			}
		}
		return nil
	}
	if err := removeNull(root, "system_fingerprint", "service_tier"); err != nil {
		return data, err
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(root["choices"], &choices) != nil || choices == nil || len(choices) > 1 {
		return data, fmt.Errorf("invalid Qwen choices")
	}
	for _, choice := range choices {
		if err := removeNull(choice, "logprobs"); err != nil {
			return data, err
		}
		var delta map[string]json.RawMessage
		if json.Unmarshal(choice["delta"], &delta) != nil || delta == nil {
			return data, fmt.Errorf("invalid Qwen delta")
		}
		if err := removeNull(delta, "function_call", "refusal"); err != nil {
			return data, err
		}
		for _, field := range []string{"role", "tool_calls"} {
			if bytes.Equal(bytes.TrimSpace(delta[field]), []byte("null")) {
				delete(delta, field)
			}
		}
		if raw, present := delta["tool_calls"]; present {
			var tools []map[string]json.RawMessage
			if json.Unmarshal(raw, &tools) != nil || tools == nil {
				return data, fmt.Errorf("invalid Qwen tool deltas")
			}
			for _, tool := range tools {
				// An empty ID is a continuation marker, not an instruction to reset the
				// existing identity. Missing first identity still fails in shared state.
				if bytes.Equal(bytes.TrimSpace(tool["id"]), []byte(`""`)) {
					delete(tool, "id")
				}
			}
			delta["tool_calls"], _ = json.Marshal(tools)
		}
		choice["delta"], _ = json.Marshal(delta)
	}
	root["choices"], _ = json.Marshal(choices)
	return json.Marshal(root)
}
