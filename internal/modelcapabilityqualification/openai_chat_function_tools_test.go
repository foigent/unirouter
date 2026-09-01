package modelcapabilityqualification

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func representativeFunctionToolsBinding(contractID string) Binding {
	identity := openAIChatFunctionToolsBindingIdentities[contractID][0]
	return Binding{
		OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
		ExecutionProfileID: identity.profileID, ExecutionProfileVersion: identity.profileVersion,
		ProtocolProfileID: identity.protocolID, ProtocolProfileVersion: identity.protocolVersion,
		ProbeContractID: contractID, ProbeContractVersion: OpenAIChatFunctionToolsProbeContractVersion,
		ExpectedProvider: identity.provider, ExpectedUpstreamModel: identity.upstreamModel, PublicPath: identity.path,
	}
}

func exactFunctionToolsBinding(contractID string, identity openAIChatFunctionToolsBindingIdentity) Binding {
	return Binding{
		OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
		ExecutionProfileID: identity.profileID, ExecutionProfileVersion: identity.profileVersion,
		ProtocolProfileID: identity.protocolID, ProtocolProfileVersion: identity.protocolVersion,
		ProbeContractID: contractID, ProbeContractVersion: OpenAIChatFunctionToolsProbeContractVersion,
		ExpectedProvider: identity.provider, ExpectedUpstreamModel: identity.upstreamModel, PublicPath: identity.path,
	}
}

func TestMiniMaxPAYGFunctionToolsContractEnumeratesExactM3AndM2Bindings(t *testing.T) {
	for contractID, identities := range map[string][]openAIChatFunctionToolsBindingIdentity{
		MiniMaxPAYGM3ChatFunctionToolsProbeContractID: openAIChatFunctionToolsBindingIdentities[MiniMaxPAYGM3ChatFunctionToolsProbeContractID],
		MiniMaxPAYGM2ChatFunctionToolsProbeContractID: openAIChatFunctionToolsBindingIdentities[MiniMaxPAYGM2ChatFunctionToolsProbeContractID],
	} {
		for _, identity := range identities {
			binding := exactFunctionToolsBinding(contractID, identity)
			body, err := BuildOpenAIChatFunctionToolsProbeRequest(binding)
			require.NoError(t, err)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(body, &payload))
			require.Equal(t, identity.upstreamModel, payload["model"])

			response := `{"id":"chatcmpl-probe","object":"chat.completion","model":"` + identity.upstreamModel + `","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"<think>probe</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`
			observation, err := ValidateNormalizedOpenAIChatFunctionToolsForBinding(binding, "application/json", strings.NewReader(response))
			require.NoError(t, err)
			require.Equal(t, identity.upstreamModel, observation.ReportedModel)
		}
	}

	forged := exactFunctionToolsBinding(MiniMaxPAYGM2ChatFunctionToolsProbeContractID,
		openAIChatFunctionToolsBindingIdentities[MiniMaxPAYGM2ChatFunctionToolsProbeContractID][0])
	forged.ExpectedUpstreamModel = "MiniMax-M2.7-highspeed"
	_, err := BuildOpenAIChatFunctionToolsProbeRequest(forged)
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestRepresentativeFunctionToolsProbeRequestsShareMechanicsAndKeepExactContracts(t *testing.T) {
	miniMaxBody, err := BuildOpenAIChatFunctionToolsProbeRequest(
		representativeFunctionToolsBinding(MiniMaxChatFunctionToolsProbeContractID),
	)
	require.NoError(t, err)
	glmBody, err := BuildOpenAIChatFunctionToolsProbeRequest(
		representativeFunctionToolsBinding(GLMChatFunctionToolsProbeContractID),
	)
	require.NoError(t, err)

	var miniMax, glm map[string]interface{}
	require.NoError(t, json.Unmarshal(miniMaxBody, &miniMax))
	require.NoError(t, json.Unmarshal(glmBody, &glm))
	for _, payload := range []map[string]interface{}{miniMax, glm} {
		require.Equal(t, false, payload["stream"])
		require.Len(t, payload["tools"], 1)
		require.Equal(t, "Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly.",
			payload["messages"].([]interface{})[0].(map[string]interface{})["content"])
	}
	require.Equal(t, "MiniMax-M3", miniMax["model"])
	require.Equal(t, float64(256), miniMax["max_completion_tokens"])
	require.NotContains(t, miniMax, "max_tokens")
	require.NotContains(t, miniMax, "tool_choice", "MiniMax uses the immutable provider-default auto contract")
	require.NotContains(t, miniMax, "thinking", "MiniMax uses the immutable adaptive provider default")
	require.Equal(t, "glm-5.3", glm["model"])
	require.Equal(t, float64(256), glm["max_tokens"])
	require.NotContains(t, glm, "max_completion_tokens")
	require.Equal(t, "auto", glm["tool_choice"])
	require.Equal(t, map[string]interface{}{"type": "enabled"}, glm["thinking"])

	forged := representativeFunctionToolsBinding(GLMChatFunctionToolsProbeContractID)
	forged.ProbeContractID = MiniMaxChatFunctionToolsProbeContractID
	_, err = BuildOpenAIChatFunctionToolsProbeRequest(forged)
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestRepresentativeFunctionToolsValidatorUsesOneStrictNormalizedGrammar(t *testing.T) {
	for _, test := range []struct {
		name, contractID, model, content, extension string
	}{
		{name: "minimax", contractID: MiniMaxChatFunctionToolsProbeContractID, model: "MiniMax-M3", content: `"<think>probe</think>"`, extension: `,"prompt_tokens_details":{"cached_tokens":3}`},
		{name: "glm", contractID: GLMChatFunctionToolsProbeContractID, model: "glm-5.3", content: `null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `{"id":"chatcmpl-probe","object":"chat.completion","model":"` + test.model + `","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":` + test.content + `,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19` + test.extension + `}}`
			observation, err := ValidateNormalizedOpenAIChatFunctionToolsForBinding(
				representativeFunctionToolsBinding(test.contractID), "application/json; charset=utf-8", strings.NewReader(body),
			)
			require.NoError(t, err)
			require.True(t, observation.ToolCallObserved)
			require.Equal(t, test.model, observation.ReportedModel)
			require.Equal(t, "tool_calls", observation.FinishReason)
			require.Equal(t, int64(19), observation.TotalTokens)

			wrongModel := strings.Replace(body, `"model":"`+test.model+`"`, `"model":"wrong"`, 1)
			_, err = ValidateNormalizedOpenAIChatFunctionToolsForBinding(
				representativeFunctionToolsBinding(test.contractID), "application/json", strings.NewReader(wrongModel),
			)
			require.Equal(t, "reported_model", OpenAIChatFunctionToolsErrorCode(err))

			ambiguousArguments := strings.Replace(body,
				`{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}`,
				`{\"value\":\"UNIROUTER_TOOL_PROBE_OK\",\"value\":\"wrong\"}`, 1)
			_, err = ValidateNormalizedOpenAIChatFunctionToolsForBinding(
				representativeFunctionToolsBinding(test.contractID), "application/json", strings.NewReader(ambiguousArguments),
			)
			require.Equal(t, "tool_arguments", OpenAIChatFunctionToolsErrorCode(err))

			if test.contractID == MiniMaxChatFunctionToolsProbeContractID {
				missingReplay := strings.Replace(body, `"content":"<think>probe</think>"`, `"content":null`, 1)
				_, err = ValidateNormalizedOpenAIChatFunctionToolsForBinding(
					representativeFunctionToolsBinding(test.contractID), "application/json", strings.NewReader(missingReplay),
				)
				require.Equal(t, "tool_calls", OpenAIChatFunctionToolsErrorCode(err))
			}
		})
	}
}
