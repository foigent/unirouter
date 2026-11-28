package adapters

import (
	"encoding/json"
	"fmt"
)

// Qwen Omni reports visible output separately from reasoning. Its prompt
// subtype counters partition the same prompt total, while cached_tokens
// overlaps the modalities and must not be added to them.
func validateQwenOmniUsage(wire *openAIUsageWire, endpoint Endpoint, prompt, completion, total int) (*int, error) {
	if endpoint != EndpointChatCompletions || wire.TotalTokens == nil || total < prompt || total-prompt != completion {
		return nil, fmt.Errorf("Qwen Omni requires complete provider totals")
	}
	if wire.PromptDetails != nil {
		parts := []json.RawMessage{wire.PromptDetails.TextTokens, wire.PromptDetails.ImageTokens, wire.PromptDetails.AudioTokens, wire.PromptDetails.VideoTokens}
		left := prompt
		for _, part := range parts {
			value, _, err := parseOptionalUsageCounter(part, "Qwen Omni prompt modality")
			if err != nil || value > left {
				return nil, fmt.Errorf("invalid Qwen Omni prompt partition")
			}
			left -= value
		}
	}
	if len(wire.CompletionDetails) == 0 || isJSONNullUsage(wire.CompletionDetails) {
		return nil, nil
	}
	var details struct {
		ReasoningTokens json.RawMessage `json:"reasoning_tokens"`
		TextTokens      json.RawMessage `json:"text_tokens"`
	}
	if err := json.Unmarshal(wire.CompletionDetails, &details); err != nil {
		return nil, fmt.Errorf("invalid Qwen Omni completion details")
	}
	reasoning, reported, err := parseOptionalUsageCounter(details.ReasoningTokens, "completion_tokens_details.reasoning_tokens")
	text, textReported, textErr := parseOptionalUsageCounter(details.TextTokens, "completion_tokens_details.text_tokens")
	if err != nil || textErr != nil || reasoning > completion || text > completion-reasoning ||
		(reported && textReported && text != completion-reasoning) {
		return nil, fmt.Errorf("invalid Qwen Omni completion partition")
	}
	if !reported {
		return nil, nil
	}
	return &reasoning, nil
}
