package modelcapabilityqualification

import (
	"encoding/json"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

const QwenProviderCompletionProbeContractID = "unirouter.capability-probe.qwen-chat-stream-tools.v2"

const QwenStreamFunctionToolsProbeContractID = "unirouter.capability-probe.qwen-chat-stream-tools.v1"

func validQwenStreamComposition(b Binding, p modelprofile.Profile) bool {
	protocol := "protocol.qwen-openai-chat.flash.v7"
	if b.ProbeContractID == QwenProviderCompletionProbeContractID {
		protocol = "protocol.qwen-openai-chat.flash.v8"
	}
	return p.Provider == "dashscope" && p.ProtocolProfile == protocol &&
		p.ReasoningProfile == "reasoning.qwen3.7-hybrid.v1" && p.CacheUsageProfile == "cache-usage.qwen-context-cache.v1" &&
		(streamToolCapability(b.CapabilityID) || b.CapabilityID == modelprofile.CapabilityReasoningBudget)
}
func BuildQwenStreamFunctionToolsProbeRequest(b Binding) ([]byte, error) {
	if (b.ProbeContractID != QwenStreamFunctionToolsProbeContractID && b.ProbeContractID != QwenProviderCompletionProbeContractID) || b.validateDataDefinition() != nil {
		return nil, ErrUnsupported
	}
	// max_completion_tokens bounds thinking plus answer; thinking_budget reserves
	// only part of it. Experiment reservations additionally allow the documented
	// ten-token generation tolerance. A length finish does not qualify tools.
	limit := 2048
	if b.ProbeContractID == QwenProviderCompletionProbeContractID {
		limit -= 10
	}
	return json.Marshal(map[string]interface{}{
		"model": b.ExpectedUpstreamModel, "stream": true, "stream_options": map[string]bool{"include_usage": true},
		"tool_choice": "auto", "max_completion_tokens": limit, "enable_thinking": true, "thinking_budget": 1024,
		"tools":    openAIChatProbeTools(),
		"messages": []map[string]string{{"role": "user", "content": "Think briefly, then call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}},
	})
}
