package modelprofile

const QwenHybridStreamToolsProtocol = "protocol.qwen-openai-chat.hybrid.v1"

func qwenHybridStreamToolsContract(id string) ChatProtocolContract {
	contract := qwenFlashStreamFunctionToolsContract(id)
	contract.OutputLimitField = "max_completion_tokens"
	contract.OutputLimitAcceptedFields = []string{"max_completion_tokens"}
	contract.OutputLimitFieldMaximums = nil
	contract.OutputLimitMax = 262_144
	contract.AssistantReasoningHistoryModeled = true
	contract.AssistantReasoningHistoryFields = []string{"reasoning_content"}
	contract.AssistantReasoningResponseModeled = true
	contract.AssistantReasoningResponseFields = []string{"reasoning_content"}
	return contract
}

func qwenNewHybridReasoningContract(id, defaultEffort string, defaultBudget int64, levels []string, aliases map[string]string) ReasoningContract {
	return ReasoningContract{
		ProfileID: id, Mode: ReasoningModeToggle, DefaultAuthority: ReasoningDefaultProvider,
		ToggleControl: ReasoningControlContract{
			Field: "enable_thinking", WireType: ReasoningControlWireBoolean,
			AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues: levels, ProviderDefault: defaultEffort,
			CompatibilityValues: aliases, DisabledValues: []string{"none"},
		},
		BudgetControl: ReasoningBudgetContract{
			Field: "thinking_budget", Minimum: 1, Maximum: 262_144, ProviderDefault: defaultBudget,
			RequiresActiveReasoning: true, MutuallyExclusiveWithEffort: true,
		},
		PreserveThinkingField: "preserve_thinking", PreserveThinkingProviderDefault: true,
	}
}

// Qwen Flash's bounded streamed Agent subset is a new implementation identity.
// Historical v6 keeps its original independently modeled capabilities.
func qwenFlashStreamFunctionToolsContract(id string) ChatProtocolContract {
	c := qwenChatToolsContract(id)
	c.FunctionToolChoices = []string{"auto"}
	c.FunctionToolProviderDefaultChoice = "auto"
	c.FunctionToolChoicesRequiringTools = []string{"auto"}
	c.NamedFunctionToolChoice = false
	c.ParallelFunctionToolCalls = false
	c.ToolStream = false
	c.ToolStreamField = ""
	c.FunctionToolParametersRequired = true
	c.FunctionToolParametersObjectWhenPresent = true
	c.FunctionToolInvocationCapabilitiesModeled = true
	c.ExactFunctionToolHistoryModeled = true
	c.ExactFunctionToolResponseModeled = true
	c.StreamContract = reasoningFunctionToolStreamContract()
	return c
}
