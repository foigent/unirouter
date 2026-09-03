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

func exactRuntimeFunctionToolsBinding(t *testing.T, contractID, capabilityID, upstreamModel string) Binding {
	t.Helper()
	for _, identity := range openAIChatFunctionToolsBindingIdentities[contractID] {
		if identity.upstreamModel != upstreamModel {
			continue
		}
		if identity.provider == "minimax" && identity.profileVersion != "1.2.0" {
			continue
		}
		binding := exactFunctionToolsBinding(contractID, identity)
		binding.CapabilityID = capabilityID
		binding.ProbeContractVersion = OpenAIChatRuntimeFunctionToolsProbeContractV2
		return binding
	}
	t.Fatalf("runtime binding not found for %s %s %s", contractID, capabilityID, upstreamModel)
	return Binding{}
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

func TestGLMToggleFunctionToolsContractEnumeratesFiveExactBindings(t *testing.T) {
	identities := openAIChatFunctionToolsBindingIdentities[GLMToggleChatFunctionToolsProbeContractID]
	require.Len(t, identities, 5)
	for _, identity := range identities {
		t.Run(identity.upstreamModel, func(t *testing.T) {
			binding := exactFunctionToolsBinding(GLMToggleChatFunctionToolsProbeContractID, identity)
			body, err := BuildOpenAIChatFunctionToolsProbeRequest(binding)
			require.NoError(t, err)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(body, &payload))
			require.Equal(t, identity.upstreamModel, payload["model"])
			require.Equal(t, "auto", payload["tool_choice"])
			require.NotContains(t, payload, "thinking", "toggle-family probe uses the immutable provider default")

			response := `{"id":"chatcmpl-probe","object":"chat.completion","model":"` + identity.upstreamModel + `","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"reasoning_content":"exact reasoning trace","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`
			observation, err := ValidateNormalizedOpenAIChatFunctionToolsForBinding(binding, "application/json", strings.NewReader(response))
			require.NoError(t, err)
			require.Equal(t, identity.upstreamModel, observation.ReportedModel)
		})
	}

	forged := exactFunctionToolsBinding(GLMToggleChatFunctionToolsProbeContractID, identities[0])
	forged.ExpectedUpstreamModel = identities[1].upstreamModel
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

func TestRuntimeFunctionToolsProbeAcceptsOnlyExactCapabilitySets(t *testing.T) {
	for _, capability := range []string{
		modelprofile.CapabilityCacheReadUsage,
		modelprofile.CapabilityFunctionTools,
		modelprofile.CapabilityPromptCache,
		modelprofile.CapabilityReasoning,
		modelprofile.CapabilityToolChoiceAuto,
		modelprofile.CapabilityTools,
	} {
		binding := exactRuntimeFunctionToolsBinding(t, MiniMaxPAYGM3ChatFunctionToolsProbeContractID, capability, "MiniMax-M3")
		body, err := BuildOpenAIChatFunctionToolsProbeRequest(binding)
		require.NoError(t, err)
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Len(t, payload.Messages, 1)
		require.Greater(t, len(payload.Messages[0].Content), 10_000)
	}

	for _, capability := range []string{
		modelprofile.CapabilityFunctionTools,
		modelprofile.CapabilityReasoning,
		modelprofile.CapabilityToolChoiceAuto,
		modelprofile.CapabilityTools,
	} {
		binding := exactRuntimeFunctionToolsBinding(t, GLMChatFunctionToolsProbeContractID, capability, "glm-5.3")
		_, err := BuildOpenAIChatFunctionToolsProbeRequest(binding)
		require.NoError(t, err)
	}

	unsupported := exactRuntimeFunctionToolsBinding(t, GLMChatFunctionToolsProbeContractID, modelprofile.CapabilityFunctionTools, "glm-5.3")
	unsupported.CapabilityID = modelprofile.CapabilityPromptCache
	_, err := BuildOpenAIChatFunctionToolsProbeRequest(unsupported)
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestMiniMaxRuntimeFunctionToolsProbeSeparatesCacheReportingFromCacheHit(t *testing.T) {
	response := func(cached string) string {
		return `{"id":"chatcmpl-probe","object":"chat.completion","model":"MiniMax-M3","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"<think>exact reasoning</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":600,"completion_tokens":7,"total_tokens":607,"prompt_tokens_details":{"cached_tokens":` + cached + `}}}`
	}

	readUsage := exactRuntimeFunctionToolsBinding(t, MiniMaxPAYGM3ChatFunctionToolsProbeContractID, modelprofile.CapabilityCacheReadUsage, "MiniMax-M3")
	observation, err := ValidateNormalizedOpenAIChatFunctionToolsForBinding(readUsage, "application/json", strings.NewReader(response("0")))
	require.NoError(t, err)
	require.True(t, observation.CacheReadCounterObserved)
	require.Zero(t, observation.CachedTokens)
	require.True(t, observation.ReasoningObserved)

	promptCache := readUsage
	promptCache.CapabilityID = modelprofile.CapabilityPromptCache
	_, err = ValidateNormalizedOpenAIChatFunctionToolsForBinding(promptCache, "application/json", strings.NewReader(response("0")))
	require.Equal(t, "cache_miss", OpenAIChatFunctionToolsErrorCode(err))
	observation, err = ValidateNormalizedOpenAIChatFunctionToolsForBinding(promptCache, "application/json", strings.NewReader(response("128")))
	require.NoError(t, err)
	require.Equal(t, int64(128), observation.CachedTokens)

	invalid := []struct {
		name, body, code string
	}{
		{name: "missing counter", body: strings.Replace(response("0"), `,"prompt_tokens_details":{"cached_tokens":0}`, "", 1), code: "cache_usage"},
		{name: "below eligibility threshold", body: strings.Replace(strings.Replace(response("0"), `"prompt_tokens":600`, `"prompt_tokens":511`, 1), `"total_tokens":607`, `"total_tokens":518`, 1), code: "cache_usage"},
		{name: "explicit null", body: strings.Replace(response("0"), `"cached_tokens":0`, `"cached_tokens":null`, 1), code: "cache_usage"},
		{name: "negative", body: strings.Replace(response("0"), `"cached_tokens":0`, `"cached_tokens":-1`, 1), code: "usage"},
		{name: "fraction", body: strings.Replace(response("0"), `"cached_tokens":0`, `"cached_tokens":1.5`, 1), code: "usage"},
		{name: "exponent", body: strings.Replace(response("0"), `"cached_tokens":0`, `"cached_tokens":1e2`, 1), code: "usage"},
		{name: "string", body: strings.Replace(response("0"), `"cached_tokens":0`, `"cached_tokens":"1"`, 1), code: "usage"},
		{name: "above prompt", body: strings.Replace(response("0"), `"cached_tokens":0`, `"cached_tokens":601`, 1), code: "usage"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			_, validationErr := ValidateNormalizedOpenAIChatFunctionToolsForBinding(readUsage, "application/json", strings.NewReader(test.body))
			require.Equal(t, test.code, OpenAIChatFunctionToolsErrorCode(validationErr))
		})
	}
}

func TestRuntimeFunctionToolsProbeRequiresProviderSpecificReasoningEvidence(t *testing.T) {
	miniMax := exactRuntimeFunctionToolsBinding(t, MiniMaxPAYGM2ChatFunctionToolsProbeContractID, modelprofile.CapabilityReasoning, "MiniMax-M2.7")
	miniMaxBody := `{"model":"MiniMax-M2.7","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"<think>work</think>answer","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":600,"completion_tokens":7,"total_tokens":607,"prompt_tokens_details":{"cached_tokens":0}}}`
	observation, err := ValidateNormalizedOpenAIChatFunctionToolsForBinding(miniMax, "application/json", strings.NewReader(miniMaxBody))
	require.NoError(t, err)
	require.True(t, observation.ReasoningObserved)
	for _, invalidContent := range []string{"plain answer", "<think></think>", "<think>unterminated", "</think><think>work</think>", "<think>one</think><think>two</think>"} {
		invalid := strings.Replace(miniMaxBody, "<think>work</think>answer", invalidContent, 1)
		_, err = ValidateNormalizedOpenAIChatFunctionToolsForBinding(miniMax, "application/json", strings.NewReader(invalid))
		require.Equal(t, "reasoning", OpenAIChatFunctionToolsErrorCode(err), invalidContent)
	}

	glm := exactRuntimeFunctionToolsBinding(t, GLMToggleChatFunctionToolsProbeContractID, modelprofile.CapabilityReasoning, "glm-5.2")
	glmBody := `{"model":"glm-5.2","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"reasoning_content":"work","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`
	observation, err = ValidateNormalizedOpenAIChatFunctionToolsForBinding(glm, "application/json", strings.NewReader(glmBody))
	require.NoError(t, err)
	require.True(t, observation.ReasoningObserved)
	for _, reasoning := range []string{"", "   "} {
		invalid := strings.Replace(glmBody, `"reasoning_content":"work"`, `"reasoning_content":"`+reasoning+`"`, 1)
		_, err = ValidateNormalizedOpenAIChatFunctionToolsForBinding(glm, "application/json", strings.NewReader(invalid))
		require.Equal(t, "reasoning", OpenAIChatFunctionToolsErrorCode(err))
	}
}
