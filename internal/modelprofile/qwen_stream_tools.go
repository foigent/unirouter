package modelprofile

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
