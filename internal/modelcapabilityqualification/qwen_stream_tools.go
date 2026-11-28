package modelcapabilityqualification

import (
	"encoding/json"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

const QwenProviderCompletionProbeContractID = "unirouter.capability-probe.qwen-chat-stream-tools.v2"

const QwenHybridStreamToolsProbeVersion = "2.0.0"
const QwenHostedStreamToolsProbeVersion = "3.0.0"
const qwenHostedProbeAnswerLimit = 1024

const QwenStreamFunctionToolsProbeContractID = "unirouter.capability-probe.qwen-chat-stream-tools.v1"

func validQwenStreamComposition(b Binding, p modelprofile.Profile) bool {
	if b.ProbeContractID == QwenStreamFunctionToolsProbeContractID && b.ProbeContractVersion == QwenHostedStreamToolsProbeVersion {
		return modelprofile.QwenHostedSplitProfile(p) &&
			(streamToolCapability(b.CapabilityID) || b.CapabilityID == modelprofile.CapabilityReasoningBudget)
	}
	if b.ProbeContractID == QwenStreamFunctionToolsProbeContractID && b.ProbeContractVersion == QwenHybridStreamToolsProbeVersion {
		return p.Provider == "dashscope" && p.Region == "cn-beijing" && len(p.UpstreamModelIDs) == 1 &&
			slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassDashScopeBeijingSharedPayGo, modelprofile.EndpointClassDashScopeBeijingWorkspacePayGo}) &&
			slices.Contains([]string{modelprofile.QwenHybridStreamToolsProtocol, modelprofile.QwenHybridInlineJPEGProtocol, modelprofile.QwenHybridInlineVideoProtocol}, p.ProtocolProfile) &&
			slices.Contains([]string{"reasoning.qwen4-max-hybrid.v1", "reasoning.qwen3.8-flash-hybrid.v1"}, p.ReasoningProfile) &&
			p.CacheUsageProfile == "cache-usage.qwen-context-cache.v1" && streamToolCapability(b.CapabilityID)
	}
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
	scenario, err := streamToolProbeScenarioForBinding(b)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"model": b.ExpectedUpstreamModel, "stream": true, "stream_options": map[string]bool{"include_usage": true},
		"tool_choice": "auto", "max_completion_tokens": limit,
		"tools":    scenario.tools(),
		"messages": []map[string]string{{"role": "user", "content": scenario.prompt}},
	}
	// These newer models supply their own thinking, history and effort defaults.
	// Combining the explicit legacy budget with effort is rejected by the API.
	if b.ProbeContractVersion == QwenHostedStreamToolsProbeVersion {
		delete(payload, "max_completion_tokens")
		payload["max_tokens"] = qwenHostedProbeAnswerLimit - modelprofile.QwenHostedAnswerTolerance
		payload["thinking_budget"] = modelprofile.QwenHostedProbeThinking
	} else if b.ProbeContractVersion != QwenHybridStreamToolsProbeVersion {
		payload["enable_thinking"], payload["thinking_budget"] = true, 1024
	}
	return json.Marshal(payload)
}
