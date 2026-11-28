package modelprofile

import (
	"slices"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

const (
	AnthropicMessages55ToolsPolicy = "anthropic.messages.claude-5.5-tools.v1"
	openAI62ResponsesReasoning     = "reasoning.openai-gpt-6.2-responses.v1"
	claudeOpus55Reasoning          = "reasoning.anthropic-opus-5.5.v1"
	claudeSonnet55Reasoning        = "reasoning.anthropic-sonnet-5.5.v1"
	claudeHaiku55Reasoning         = "reasoning.anthropic-haiku-5.5.v1"
)

// NativeClientToolsProfile selects a supported wire contract. The exact model
// identities enter through immutable V2 Definitions and Probe bindings.
func NativeClientToolsProfile(p Profile) bool {
	if !slices.Equal(p.Capabilities, []string{CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto, CapabilityReasoning}) ||
		p.CacheUsageProfile != "" || len(p.UpstreamModelIDs) != 1 || p.ProtocolProfileVersion != "1.0.0" || p.Region != "global" {
		return false
	}
	switch p.APISurface {
	case APISurfaceOpenAIResponses:
		return p.Provider == "openai" && slices.Equal(p.Operations, []string{OperationOpenAIResponses}) &&
			slices.Equal(p.EndpointClasses, []string{EndpointClassOpenAIOfficialOrigin}) &&
			p.ProtocolProfile == endpointprotocol.ProfileOpenAIResponsesToolsV3 && p.OperationPolicyProfile == "" &&
			p.ReasoningProfile == openAI62ResponsesReasoning
	case APISurfaceAnthropicMessage:
		return p.Provider == "anthropic" && slices.Equal(p.Operations, []string{OperationAnthropicMessage}) &&
			slices.Equal(p.EndpointClasses, []string{EndpointClassAnthropicOfficialOrigin}) &&
			p.ProtocolProfile == endpointprotocol.ProfileAnthropicMessagesTextV3 && p.OperationPolicyProfile == AnthropicMessages55ToolsPolicy &&
			slices.Contains([]string{claudeOpus55Reasoning, claudeSonnet55Reasoning, claudeHaiku55Reasoning}, p.ReasoningProfile)
	default:
		return false
	}
}

// NativeInlineJPEGProfile composes a bounded image with the existing exact
// text, tool, and reasoning grammar on the same model name. An invocation
// selects either image or tools; their combination has no qualified wire.
func NativeInlineJPEGProfile(p Profile) bool {
	if len(p.Capabilities) != 5 || p.Capabilities[4] != CapabilityVision {
		return false
	}
	switch p.ProtocolProfile {
	case endpointprotocol.ProfileOpenAIResponsesInlineJPEG:
		p.ProtocolProfile = endpointprotocol.ProfileOpenAIResponsesToolsV3
	case endpointprotocol.ProfileAnthropicMessagesInlineJPEG:
		p.ProtocolProfile = endpointprotocol.ProfileAnthropicMessagesTextV3
	default:
		return false
	}
	p.Capabilities = p.Capabilities[:4]
	return NativeClientToolsProfile(p)
}

func claude55Reasoning(profileID, defaultEffort string, modes, disabled []string) ReasoningContract {
	return ReasoningContract{
		ProfileID: profileID, Mode: ReasoningModeToggle, ToggleObjectField: "thinking",
		ToggleControl: ReasoningControlContract{
			Field: "type", WireType: ReasoningControlWireString, AllowedValues: modes, DisabledValues: disabled, ProviderDefault: "adaptive",
		},
		EffortObjectField: "output_config",
		EffortControl: ReasoningControlContract{
			Field: "effort", WireType: ReasoningControlWireString, AllowedValues: []string{"low", "medium", "high", "xhigh", "max"}, ProviderDefault: defaultEffort,
		},
		ResponseThinkingTextMode: ReasoningResponseThinkingOmitted, DefaultAuthority: ReasoningDefaultProvider,
		UnsupportedRequestFieldsWhileActive: []string{"temperature", "top_p", "top_k"},
	}
}

// NativeClientToolsRequestContract retains the default reasoning wire so signed
// or encrypted replay can be validated. Optional reasoning controls have no
// tool Probe approval and are not enabled by the runtime projection.
func NativeClientToolsRequestContract(p Profile) endpointprotocol.PublicRequestContract {
	contract := endpointprotocol.PublicRequestContract{ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version}
	if p.APISurface == APISurfaceOpenAIResponses {
		contract.Reasoning = &endpointprotocol.PublicReasoningRequestContract{
			RequestObjectField: "reasoning", EffortField: "effort",
			AllowedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
		}
		return contract
	}
	r, _ := ResolveReasoningContract(p.ReasoningProfile)
	contract.AnthropicMessages = &endpointprotocol.PublicAnthropicMessagesContract{
		ForbiddenRootFields: []string{"temperature", "top_k", "top_p"}, RequireFinalUserMessage: true,
		AllowedResponseStops: []string{"end_turn", "max_tokens", "refusal", "stop_sequence", "tool_use"},
		RefusalStopDetails:   true, ResponseInferenceGeo: true, ResponseServiceTier: true,
		Reasoning: &endpointprotocol.PublicAnthropicReasoningRequestContract{
			ThinkingObjectField: "thinking", ThinkingTypeField: "type", AllowedThinkingTypes: []string{"adaptive"},
			ThinkingProviderDefault: "adaptive", OutputConfigObjectField: "output_config", EffortField: "effort",
			AllowedEfforts: r.EffortControl.AllowedValues, EffortProviderDefault: r.EffortControl.ProviderDefault,
			ResponseThinkingTextMode: r.ResponseThinkingTextMode,
		},
		FunctionTools: &endpointprotocol.PublicAnthropicFunctionToolsContract{
			MaxTools: 64, ToolNamePattern: `^[a-zA-Z0-9_-]{1,64}$`,
			ToolChoices: []string{"auto", "none"}, ProviderDefaultChoice: "auto",
			ExactHistory: true, ExactResponse: true, RequiresNonStream: true, ForceSerial: true,
			ToolInputIdentityGuarded: true, ThinkingHistoryIdentityOnly: true,
		},
	}
	return contract
}
