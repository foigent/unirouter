package modelprofile

import "slices"

const (
	QwenHostedSplitProtocol     = "protocol.qwen-openai-chat.hosted-split.v1"
	QwenHostedVisionProtocol    = "protocol.qwen-openai-chat.hosted-split.v2"
	QwenHosted27BReasoning      = "reasoning.qwen3.8-hosted-27b.v1"
	QwenHostedA95BReasoning     = "reasoning.qwen3.8-hosted-a95b.v1"
	QwenHostedSplitUsageProfile = "cache-usage.qwen-hosted-split.v1"
	QwenHostedBudgetScope       = "separate_reasoning_and_answer"
	QwenHostedProbeThinking     = 512
	QwenHostedAnswerTolerance   = 10
)

func qwenHostedSplitContract(id string) ChatProtocolContract {
	contract := qwenHybridStreamToolsContract(id)
	contract.OutputLimitField = "max_tokens"
	contract.OutputLimitAcceptedFields = []string{"max_tokens"}
	contract.OutputLimitMin = QwenHostedAnswerTolerance + 1
	contract.OutputLimitMax = 32_768
	contract.OutputEnforcement = OutputEnforcementProviderCompletionV1
	contract.ExactTextOperationProbe = true
	return contract
}

func qwenHostedReasoning(id string, optional bool) ReasoningContract {
	contract := ReasoningContract{
		ProfileID: id, Mode: ReasoningModeRequired, DefaultAuthority: ReasoningDefaultProvider,
		BudgetControl: ReasoningBudgetContract{
			Field: "thinking_budget", Minimum: 1, Maximum: 4096, RequiresActiveReasoning: true,
		},
	}
	if optional {
		contract.Mode = ReasoningModeToggle
		contract.ToggleControl = ReasoningControlContract{
			Field: "enable_thinking", WireType: ReasoningControlWireBoolean,
			AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		}
	}
	return contract
}

func IsQwenHostedSplitProtocol(id string) bool {
	return id == QwenHostedSplitProtocol || id == QwenHostedVisionProtocol
}

func QwenHostedSplitProfile(p Profile) bool {
	if p.Provider != "dashscope" || p.Region != "cn-beijing" || !IsQwenHostedSplitProtocol(p.ProtocolProfile) ||
		p.CacheUsageProfile != QwenHostedSplitUsageProfile ||
		!slices.Equal(p.EndpointClasses, []string{EndpointClassDashScopeBeijingSharedPayGo, EndpointClassDashScopeBeijingWorkspacePayGo}) {
		return false
	}
	switch p.ReasoningProfile {
	case QwenHosted27BReasoning:
		return slices.Equal(p.UpstreamModelIDs, []string{"qwen3.8-27b"})
	case QwenHostedA95BReasoning:
		return p.ProtocolProfile == QwenHostedSplitProtocol && slices.Equal(p.UpstreamModelIDs, []string{"qwen3.8-2.4t-a95b"})
	default:
		return false
	}
}
