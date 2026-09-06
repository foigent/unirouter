package modelprofile

// These are implementation revisions, not catalog entries. Immutable data
// definitions select them explicitly; historical Profile addresses stay intact.
func deepSeekStreamFunctionToolsContract(id string) ChatProtocolContract {
	c := deepSeekV4ExactFunctionToolHistoryContract(id)
	c.FunctionToolsRequireNonStream = false
	c.StreamContract = reasoningFunctionToolStreamContract()
	return c
}

func kimiStreamFunctionToolsContract(id string, k3 bool) ChatProtocolContract {
	c := kimiK2xExactFunctionToolsContract(id)
	if k3 {
		c = kimiK3ExactDynamicFunctionToolsContract(id)
	}
	c.FunctionToolsRequireNonStream = false
	c.StreamContract = reasoningFunctionToolStreamContract()
	return c
}

func reasoningFunctionToolStreamContract() ChatStreamContract {
	return ChatStreamContract{
		RequestOptionsModeled: true, AllowedRequestOptions: []string{"include_usage"}, RequireIncludeUsage: true,
		ResponseModeled: true, RequireDone: true, RequireTerminalUsage: true, AllowTerminalUsageChoices: true,
		DeltaTextFields: []string{"content", "reasoning_content"}, FinishReasons: []string{"stop", "length", "tool_calls"},
		FunctionToolDeltas: true,
	}
}

func miniMaxM3StreamFunctionToolsContract(id string) ChatProtocolContract {
	c := miniMaxM3FunctionToolsContract(id)
	c.FunctionToolsRequireNonStream = false
	c.FunctionToolChoices = []string{"auto"}
	c.FunctionToolChoicesRequiringTools = []string{"auto"}
	c.StreamContract.FunctionToolDeltas = true
	c.StreamContract.FinishReasons = []string{"stop", "length", "tool_calls"}
	return c
}

func miniMaxM3StreamReasoningContract() ReasoningContract {
	return ReasoningContract{
		ProfileID: "reasoning.minimax-m3-adaptive-embedded.v2", Mode: ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", WireType: ReasoningControlWireString,
			AllowedValues: []string{"adaptive", "disabled"}, ProviderDefault: "adaptive", DisabledValues: []string{"disabled"},
		},
		ResponseThinkingTextMode: ReasoningResponseThinkingEmbedded, DefaultAuthority: ReasoningDefaultProvider,
		SplitOutputField: "reasoning_split", AssistantToolHistoryReplayField: "content",
	}
}

func miniMaxM3ToolContinuationReasoningContract() ReasoningContract {
	c := miniMaxM3StreamReasoningContract()
	c.ProfileID = "reasoning.minimax-m3-adaptive-embedded.v3"
	c.AssistantToolHistoryReplayField = ""
	return c
}

// The explicit implementation revision is required by the data Profile so an
// older fleet cannot advertise support for the new composition accidentally.
// It reuses M2.7's established embedded-thinking continuation semantics.
func miniMaxRequiredToolContinuationReasoningContract(id string) ReasoningContract {
	return ReasoningContract{
		ProfileID: id, Mode: ReasoningModeRequired,
		ResponseThinkingTextMode: ReasoningResponseThinkingEmbedded,
		DefaultAuthority:         ReasoningDefaultProvider, SplitOutputField: "reasoning_split",
		SplitOutputProviderDefault: false, SplitOutputEnabledSupported: false,
	}
}
