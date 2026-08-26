package modelprofile

import "slices"

const (
	AnthropicMessagesSonnet5CorePolicyV1     = "anthropic.messages.sonnet-5-core.v1"
	AnthropicMessagesSonnet5EvidencePolicyV2 = "anthropic.messages.sonnet-5-evidence.v2"
	AnthropicMessagesSonnet5ToolsPolicyV3    = "anthropic.messages.sonnet-5-tools.v3"
	AnthropicMessagesOpus5EvidencePolicyV1   = "anthropic.messages.opus-5-evidence.v1"
	AnthropicMessagesHaiku45EvidencePolicyV1 = "anthropic.messages.haiku-4.5-evidence.v1"
)

// AnthropicMessagesOperationContract owns model-specific Messages grammar
// which is neither a reasoning control nor a capability tag. The exact Model
// Execution Profile references this immutable policy; operation-facing code
// receives only a request-scoped projection of it.
type AnthropicMessagesOperationContract struct {
	ProfileID               string
	RequireFinalUserMessage bool
	AllowedResponseStops    []string
	RefusalStopDetails      bool
	ResponseInferenceGeo    bool
	ResponseServiceTier     bool
	FunctionTools           *AnthropicMessagesFunctionToolsContract
}

// AnthropicMessagesFunctionToolsContract is the provider/surface-specific
// client-tool grammar. It deliberately does not reuse ChatProtocolContract:
// Messages owns content-block history, object tool_choice and thinking replay.
type AnthropicMessagesFunctionToolsContract struct {
	MaxTools                    int
	ToolNamePattern             string
	ToolChoices                 []string
	ProviderDefaultChoice       string
	NamedChoice                 bool
	ParallelToolUse             bool
	ParallelProviderDefault     bool
	ExactHistory                bool
	ExactResponse               bool
	RequiresNonStream           bool
	ToolInputIdentityGuarded    bool
	ThinkingHistoryIdentityOnly bool
}

var anthropicMessagesOperationContracts = map[string]AnthropicMessagesOperationContract{
	AnthropicMessagesSonnet5CorePolicyV1: {
		ProfileID:               AnthropicMessagesSonnet5CorePolicyV1,
		RequireFinalUserMessage: true,
		AllowedResponseStops:    []string{"end_turn", "max_tokens", "refusal", "stop_sequence"},
		RefusalStopDetails:      true,
	},
	AnthropicMessagesSonnet5EvidencePolicyV2: {
		ProfileID:               AnthropicMessagesSonnet5EvidencePolicyV2,
		RequireFinalUserMessage: true,
		AllowedResponseStops:    []string{"end_turn", "max_tokens", "refusal", "stop_sequence"},
		RefusalStopDetails:      true,
		ResponseInferenceGeo:    true,
		ResponseServiceTier:     true,
	},
	AnthropicMessagesSonnet5ToolsPolicyV3: {
		ProfileID:               AnthropicMessagesSonnet5ToolsPolicyV3,
		RequireFinalUserMessage: true,
		AllowedResponseStops:    []string{"end_turn", "max_tokens", "refusal", "stop_sequence", "tool_use"},
		RefusalStopDetails:      true,
		ResponseInferenceGeo:    true,
		ResponseServiceTier:     true,
		FunctionTools: &AnthropicMessagesFunctionToolsContract{
			MaxTools: 64, ToolNamePattern: `^[a-zA-Z0-9_-]{1,64}$`,
			ToolChoices: []string{"any", "auto", "none", "tool"}, ProviderDefaultChoice: "auto",
			NamedChoice: true, ParallelToolUse: true, ParallelProviderDefault: true,
			ExactHistory: true, ExactResponse: true, RequiresNonStream: true,
			ToolInputIdentityGuarded: true, ThinkingHistoryIdentityOnly: true,
		},
	},
	AnthropicMessagesOpus5EvidencePolicyV1: {
		ProfileID:               AnthropicMessagesOpus5EvidencePolicyV1,
		RequireFinalUserMessage: true,
		AllowedResponseStops:    []string{"end_turn", "max_tokens", "refusal", "stop_sequence"},
		RefusalStopDetails:      true,
		ResponseInferenceGeo:    true,
		ResponseServiceTier:     true,
	},
	AnthropicMessagesHaiku45EvidencePolicyV1: {
		ProfileID:               AnthropicMessagesHaiku45EvidencePolicyV1,
		RequireFinalUserMessage: true,
		AllowedResponseStops:    []string{"end_turn", "max_tokens", "refusal", "stop_sequence"},
		RefusalStopDetails:      true,
		ResponseInferenceGeo:    true,
		ResponseServiceTier:     true,
	},
}

func ResolveAnthropicMessagesOperationContract(profileID string) (AnthropicMessagesOperationContract, bool) {
	contract, ok := anthropicMessagesOperationContracts[profileID]
	if !ok {
		return AnthropicMessagesOperationContract{}, false
	}
	contract.AllowedResponseStops = slices.Clone(contract.AllowedResponseStops)
	if contract.FunctionTools != nil {
		tools := *contract.FunctionTools
		tools.ToolChoices = slices.Clone(contract.FunctionTools.ToolChoices)
		contract.FunctionTools = &tools
	}
	return contract, true
}
