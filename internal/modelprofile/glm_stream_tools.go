package modelprofile

// The successor changes only the precise Chat delivery contract. Historical
// endpoint, default, reasoning, cache and media identities are not reinterpreted.
func glmStreamFunctionToolsSuccessorProfiles() []Profile {
	var result []Profile
	for _, profile := range currentProviderProfiles() {
		switch profile.ID + "@" + profile.Version {
		case "glm.5.3.api@1.8.0", "glm.5.2.api@1.8.0":
			profile.Version = "1.9.0"
		case "glm.5.1.api@1.0.0", "glm.5-turbo.api@1.0.0", "glm.5.api@1.0.0":
			profile.Version = "1.1.0"
		case "glm.5v-turbo.api@1.11.0":
			profile.Version = "1.12.0"
		default:
			continue
		}
		profile.ProtocolProfile = "protocol.glm-openai-chat.v7"
		if profile.ID == "glm.5v-turbo.api" {
			profile.ProtocolProfile = "protocol.glm-openai-chat.glm5v.v6"
		}
		profile.Label += " / Stream Function Tools"
		result = append(result, profile)
	}
	return result
}

func glmStreamFunctionToolsContract(contract ChatProtocolContract) ChatProtocolContract {
	contract = glmExactFunctionToolsContract(contract)
	contract.StreamContract = ChatStreamContract{
		RequestOptionsModeled: true, AllowedRequestOptions: []string{"include_usage"}, RequireIncludeUsage: true,
		ResponseModeled: true, RequireDone: true, RequireTerminalUsage: true, AllowTerminalUsageChoices: true,
		DeltaTextFields: []string{"content", "reasoning_content"}, FinishReasons: []string{"stop", "length", "tool_calls"},
		FunctionToolDeltas: true,
	}
	return contract
}
