package modelcapabilityqualification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const kimiProbeEnvelopePrefix = `{"id":"chatcmpl-kimi-probe","object":"chat.completion","created":1787932800,"model":"kimi-k2.6",`

func TestCompiledKimiBindingsRemainExactPerExecutionProfile(t *testing.T) {
	bindings := KimiK26CurrentBindings()
	require.Len(t, bindings, 6)
	wantCapabilities := []string{
		modelprofile.CapabilityStreaming,
		modelprofile.CapabilityReasoning,
		modelprofile.CapabilityTools,
		modelprofile.CapabilityFunctionTools,
		modelprofile.CapabilityToolChoiceAuto,
		modelprofile.CapabilityJSONObject,
	}
	for index, binding := range bindings {
		require.Equal(t, wantCapabilities[index], binding.CapabilityID)
		if binding.CapabilityID == modelprofile.CapabilityStreaming {
			require.Equal(t, KimiChatSSEProbeContractVersionV3, binding.ProbeContractVersion)
		} else if binding.ProbeContractID == KimiChatFunctionToolsProbeContractID {
			require.Equal(t, KimiChatFunctionToolsProbeContractVersionV4, binding.ProbeContractVersion)
		} else {
			require.Equal(t, KimiChatCoreProbeContractVersionV2, binding.ProbeContractVersion)
		}
		require.Equal(t, "kimi.k2.6.api", binding.ExecutionProfileID)
		require.Equal(t, "1.7.0", binding.ExecutionProfileVersion)
		require.Equal(t, "protocol.kimi-openai-chat.v5", binding.ProtocolProfileID)
		require.Equal(t, "1.0.0", binding.ProtocolProfileVersion)
		require.Equal(t, "kimi", binding.ExpectedProvider)
		require.Equal(t, "kimi-k2.6", binding.ExpectedUpstreamModel)
		resolved, err := Resolve(binding.OperationID, binding.CapabilityID,
			binding.ExecutionProfileID, binding.ExecutionProfileVersion,
			binding.ProtocolProfileID, binding.ProtocolProfileVersion)
		require.NoError(t, err)
		require.Equal(t, binding, resolved)
	}

	k27, err := ResolveAll(
		"openai.chat_completions.v1", "kimi.k2.7-code.api", "1.3.0",
		"protocol.kimi-openai-chat.v5", "1.0.0",
	)
	require.NoError(t, err)
	require.Len(t, k27, 6)
	for _, binding := range k27 {
		require.Equal(t, "kimi.k2.7-code.api", binding.ExecutionProfileID)
		require.Equal(t, "1.3.0", binding.ExecutionProfileVersion)
		require.Equal(t, "kimi-k2.7-code", binding.ExpectedUpstreamModel)
	}

	k3, err := ResolveAll(
		"openai.chat_completions.v1", "kimi.k3.api", "1.9.0",
		"protocol.kimi-openai-chat.k3.v5", "1.0.0",
	)
	require.NoError(t, err)
	require.Len(t, k3, 8)
	for _, binding := range k3 {
		require.Equal(t, "kimi.k3.api", binding.ExecutionProfileID)
		require.Equal(t, "1.9.0", binding.ExecutionProfileVersion)
		require.Equal(t, "kimi-k3", binding.ExpectedUpstreamModel)
		require.NotEqual(t, modelprofile.CapabilityDynamicTools, binding.CapabilityID)
		require.NotEqual(t, modelprofile.CapabilityJSONSchema, binding.CapabilityID)
	}

	_, err = ResolveAll(
		"openai.chat_completions.v1", "kimi.k3.api", "1.8.0",
		"protocol.kimi-openai-chat.k3.v5", "1.0.0",
	)
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestKimiK27RequiredThinkingProbeContractsReuseOnlyExactReviewedGrammar(t *testing.T) {
	usage := `{"prompt_tokens":24,"completion_tokens":12,"total_tokens":36,"cached_tokens":4,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":7}}`
	reasoningBody := `{"id":"chatcmpl-k27","object":"chat.completion","created":1787932800,"model":"kimi-k2.7-code","choices":[{"index":0,"message":{"role":"assistant","content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private reasoning"},"finish_reason":"stop","logprobs":null}],"usage":` + usage + `}`
	reasoning, err := ValidateKimiChatReasoning(
		KimiChatReasoningProbeContractID, KimiChatCoreProbeContractVersionV2,
		"kimi-k2.7-code", "application/json", strings.NewReader(reasoningBody),
	)
	require.NoError(t, err)
	require.True(t, reasoning.ReasoningObserved)

	toolsBody := `{"id":"chatcmpl-k27","object":"chat.completion","created":1787932800,"model":"kimi-k2.7-code","choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":"private reasoning","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"finish_reason":"tool_calls","logprobs":null}],"usage":` + usage + `}`
	tools, err := ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV4,
		"kimi-k2.7-code", "application/json", strings.NewReader(toolsBody),
	)
	require.NoError(t, err)
	require.True(t, tools.ToolCallObserved)

	jsonBody := `{"id":"chatcmpl-k27","object":"chat.completion","created":1787932800,"model":"kimi-k2.7-code","choices":[{"index":0,"message":{"role":"assistant","content":"{\"status\":\"UNIROUTER_JSON_PROBE_OK\"}","reasoning_content":"private reasoning"},"finish_reason":"stop","logprobs":null}],"usage":` + usage + `}`
	structured, err := ValidateKimiChatJSONObject(
		KimiChatJSONObjectProbeContractID, KimiChatCoreProbeContractVersionV2,
		"kimi-k2.7-code", "application/json", strings.NewReader(jsonBody),
	)
	require.NoError(t, err)
	require.True(t, structured.StructuredOutputObserved)

	for name, body := range map[string]string{"tools": toolsBody, "json": jsonBody} {
		t.Run(name+" rejects missing required reasoning", func(t *testing.T) {
			withoutReasoning := strings.Replace(body, `,"reasoning_content":"private reasoning"`, "", 1)
			if name == "tools" {
				_, err = ValidateKimiChatFunctionTools(
					KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV4,
					"kimi-k2.7-code", "application/json", strings.NewReader(withoutReasoning),
				)
			} else {
				_, err = ValidateKimiChatJSONObject(
					KimiChatJSONObjectProbeContractID, KimiChatCoreProbeContractVersionV2,
					"kimi-k2.7-code", "application/json", strings.NewReader(withoutReasoning),
				)
			}
			require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
			require.Equal(t, "reasoning_content", KimiChatErrorCode(err))
		})
	}
}

func TestKimiK27SSEProbeV4RequiresReasoningAndPreservesOlderContracts(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl-k27","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.7-code","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"private reasoning"},"finish_reason":null}],"usage":null}`,
		`data: {"id":"chatcmpl-k27","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.7-code","choices":[{"index":0,"delta":{"content":"UNIROUTER_STREAM_PROBE_OK","reasoning_content":""},"finish_reason":null}],"usage":null}`,
		`data: {"id":"chatcmpl-k27","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.7-code","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":2,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":4}}}]}`,
		`data: {"id":"chatcmpl-k27","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.7-code","choices":[],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":2,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":4}}}`,
		`data: [DONE]`, "",
	}, "\n\n")

	observation, err := ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV4,
		"kimi-k2.7-code", "text/event-stream", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.DoneSeen)
	require.True(t, observation.ReasoningObserved)
	require.Equal(t, int64(2), observation.CachedTokens)

	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV3,
		"kimi-k2.7-code", "text/event-stream", strings.NewReader(body),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "reasoning_content", KimiChatErrorCode(err))

	withoutReasoning := strings.Replace(body, `"reasoning_content":"private reasoning"`, `"reasoning_content":""`, 1)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV4,
		"kimi-k2.7-code", "text/event-stream", strings.NewReader(withoutReasoning),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "reasoning_content", KimiChatErrorCode(err))

	duplicate := strings.Replace(body, `"content":"UNIROUTER_STREAM_PROBE_OK"`, `"content":"shadow","\u0063ontent":"UNIROUTER_STREAM_PROBE_OK"`, 1)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV4,
		"kimi-k2.7-code", "text/event-stream", strings.NewReader(duplicate),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "json", KimiChatErrorCode(err))
}

func TestKimiChatReasoningProbeProvesProviderDefaultThinking(t *testing.T) {
	body := kimiProbeEnvelopePrefix + `"choices":[{"index":0,"message":{"role":"assistant","content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private reasoning"},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"cached_tokens":3}}`
	observation, err := ValidateKimiChatReasoning(
		KimiChatReasoningProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "application/json", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.ReasoningObserved)
	require.Equal(t, int64(3), observation.CachedTokens)

	_, err = ValidateKimiChatReasoning(
		KimiChatReasoningProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "application/json", strings.NewReader(strings.Replace(body, `,"reasoning_content":"private reasoning"`, "", 1)),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "reasoning_content", KimiChatErrorCode(err))
}

func TestKimiChatReasoningEffortUsesExistingReasoningDeliveryShape(t *testing.T) {
	body := `{"id":"chatcmpl-k3","object":"chat.completion","created":1787932800,"model":"kimi-k3","choices":[{"index":0,"message":{"role":"assistant","content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private"},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"cached_tokens":0,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":5}}}`
	observation, err := ValidateKimiChatReasoning(
		KimiChatReasoningEffortProbeContractID, KimiChatReasoningEffortProbeContractVersion,
		"kimi-k3", "application/json", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.ReasoningObserved)
	require.Equal(t, "kimi-k3", observation.ReportedModel)
	require.Equal(t, int64(0), observation.CachedTokens)

	_, err = ValidateKimiChatReasoning(
		KimiChatReasoningEffortProbeContractID, KimiChatReasoningEffortProbeContractVersion,
		"kimi-k3", "application/json", strings.NewReader(strings.Replace(body, `"reasoning_tokens":5`, `"reasoning_tokens":5,"unmodeled":1`, 1)),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "json", KimiChatErrorCode(err))

	_, err = ValidateKimiChatReasoning(
		KimiChatReasoningEffortProbeContractID, KimiChatReasoningEffortProbeContractVersion,
		"kimi-k3", "application/json", strings.NewReader(strings.Replace(body, `"reasoning_tokens":5`, `"reasoning_tokens":5,"reasoning_tokens":5`, 1)),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "json", KimiChatErrorCode(err))

	_, err = ValidateKimiChatReasoning(
		KimiChatReasoningEffortProbeContractID, KimiChatCoreProbeContractVersionV2,
		"kimi-k3", "application/json", strings.NewReader(body),
	)
	require.Error(t, err, "the effort identity must not inherit later reasoning contract versions")
}

func TestKimiChatCoreProbeV2AcceptsOnlyBoundedUsageDetails(t *testing.T) {
	body := kimiProbeEnvelopePrefix + `"choices":[{"index":0,"message":{"role":"assistant","content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private reasoning"},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"cached_tokens":3,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":5}}}`

	_, err := ValidateKimiChatReasoning(
		KimiChatReasoningProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "application/json", strings.NewReader(body),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "json", KimiChatErrorCode(err), "historical v1 must not reinterpret new wire members")

	observation, err := ValidateKimiChatReasoning(
		KimiChatReasoningProbeContractID, KimiChatCoreProbeContractVersionV2,
		"kimi-k2.6", "application/json", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.ReasoningObserved)
	require.Equal(t, int64(3), observation.CachedTokens)

	tests := []struct {
		name, body, code string
	}{
		{"unknown usage sibling", strings.Replace(body, `"reasoning_tokens":5`, `"reasoning_tokens":5,"other":1`, 1), "json"},
		{"non numeric reasoning", strings.Replace(body, `"reasoning_tokens":5`, `"reasoning_tokens":"5"`, 1), "json"},
		{"reasoning exceeds completion", strings.Replace(body, `"reasoning_tokens":5`, `"reasoning_tokens":8`, 1), "reasoning_tokens"},
		{"cache mirror conflict", strings.Replace(body, `"prompt_tokens_details":{"cached_tokens":3}`, `"prompt_tokens_details":{"cached_tokens":2}`, 1), "cached_tokens_conflict"},
		{"cache mirror without authority", strings.Replace(body, `,"cached_tokens":3`, ``, 1), "cached_tokens"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateKimiChatReasoning(
				KimiChatReasoningProbeContractID, KimiChatCoreProbeContractVersionV2,
				"kimi-k2.6", "application/json", strings.NewReader(test.body),
			)
			require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
			require.Equal(t, test.code, KimiChatErrorCode(err))
		})
	}
}

func TestKimiChatFunctionToolsProbeUsesExactAutoCompatibleShape(t *testing.T) {
	body := kimiProbeEnvelopePrefix + `"choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"finish_reason":"tool_calls","logprobs":null}],"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29,"cached_tokens":0}}`
	observation, err := ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "application/json", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.ToolCallObserved)

	for _, mutation := range []string{
		strings.Replace(body, "unirouter_probe_echo", "other", 1),
		strings.Replace(body, `"id":"call_1"`, `"index":0,"id":"call_1"`, 1),
		strings.Replace(body, `"total_tokens":29`, `"total_tokens":28`, 1),
	} {
		_, err := ValidateKimiChatFunctionTools(
			KimiChatFunctionToolsProbeContractID, KimiChatCoreProbeContractVersion,
			"kimi-k2.6", "application/json", strings.NewReader(mutation),
		)
		require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	}
}

func TestKimiChatFunctionToolsProbeV3AlignsWithRuntimeUsageShapeWithoutPromotingUnknownCounters(t *testing.T) {
	body := kimiProbeEnvelopePrefix + `"service_tier":"standard","metadata":{"trace":true},"choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":"","refusal":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"finish_reason":"tool_calls","logprobs":null}],"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29,"cached_tokens":0,"prompt_tokens_details":{"cached_tokens":0,"provider_input_tokens":20},"completion_tokens_details":{"reasoning_tokens":0,"provider_tool_tokens":4},"provider_total_details":{"tool_tokens":4}}}`

	_, err := ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatCoreProbeContractVersionV2,
		"kimi-k2.6", "application/json", strings.NewReader(body),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe, "historical v2 must keep its immutable acceptance set")
	for _, historicalNull := range []string{
		strings.Replace(body, `"service_tier":"standard"`, `"service_tier":null`, 1),
		strings.Replace(body, `"metadata":{"trace":true}`, `"metadata":null`, 1),
		strings.Replace(body, `"reasoning_content":"","refusal":null,`, `"reasoning_content":"","reasoning":null,"refusal":null,`, 1),
	} {
		_, err := ValidateKimiChatFunctionTools(
			KimiChatFunctionToolsProbeContractID, KimiChatCoreProbeContractVersionV2,
			"kimi-k2.6", "application/json", strings.NewReader(historicalNull),
		)
		require.ErrorIs(t, err, ErrInvalidKimiChatProbe, "historical v2 must reject newly recognized fields even when null")
	}

	observation, err := ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV3,
		"kimi-k2.6", "application/json", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.ToolCallObserved)
	require.Equal(t, int64(20), observation.PromptTokens)
	require.Equal(t, int64(9), observation.CompletionTokens)
	require.Equal(t, int64(29), observation.TotalTokens)
	require.Zero(t, observation.CachedTokens)

	for _, mutation := range []string{
		strings.Replace(body, `"provider_tool_tokens":4`, `"provider_tool_tokens":"4"`, 1),
		strings.Replace(body, `"tool_tokens":4`, `"tool_tokens":4.5`, 1),
		strings.Replace(body, `\"value\":\"UNIROUTER_TOOL_PROBE_OK\"`, `\"value\":\"WRONG\",\"value\":\"UNIROUTER_TOOL_PROBE_OK\"`, 1),
		strings.Replace(body, `\"value\":\"UNIROUTER_TOOL_PROBE_OK\"`, `\"value\":\"WRONG\",\"\\u0076alue\":\"UNIROUTER_TOOL_PROBE_OK\"`, 1),
		strings.Replace(body, `"id":"call_1"`, `"index":0,"id":"call_1"`, 1),
		strings.Replace(body, `"metadata":{"trace":true}`, `"metadata":{"trace":true},"unknown":"x"`, 1),
	} {
		_, err := ValidateKimiChatFunctionTools(
			KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV3,
			"kimi-k2.6", "application/json", strings.NewReader(mutation),
		)
		require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	}
}

func TestKimiChatFunctionToolsProbeV4NormalizesOnlyNonStreamToolCallIndex(t *testing.T) {
	body := kimiProbeEnvelopePrefix + `"service_tier":"standard","choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":"","tool_calls":[{"index":999999999999999999999999999999999999999,"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"finish_reason":"tool_calls","logprobs":null}],"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29,"cached_tokens":0,"completion_tokens_details":{"reasoning_tokens":0,"provider_tool_tokens":4}}}`

	_, err := ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV3,
		"kimi-k2.6", "application/json", strings.NewReader(body),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe, "historical v3 must retain its immutable acceptance set")
	require.Equal(t, "json", KimiChatErrorCode(err))

	observation, err := ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV4,
		"kimi-k2.6", "application/json", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.ToolCallObserved)
	require.Equal(t, int64(29), observation.TotalTokens)

	k3Body := strings.Replace(body, `"model":"kimi-k2.6"`, `"model":"kimi-k3"`, 1)
	k3Observation, err := ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV4,
		"kimi-k3", "application/json", strings.NewReader(k3Body),
	)
	require.NoError(t, err)
	require.True(t, k3Observation.ToolCallObserved)
	require.Equal(t, int64(29), k3Observation.TotalTokens)

	for _, invalid := range []string{`null`, `"0"`, `-1`, `0.5`, `1e2`, `01`} {
		t.Run("invalid_index_"+strings.NewReplacer("-", "negative_", ".", "decimal_", `"`, "quote_").Replace(invalid), func(t *testing.T) {
			mutated := strings.Replace(body, "999999999999999999999999999999999999999", invalid, 1)
			_, err := ValidateKimiChatFunctionTools(
				KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV4,
				"kimi-k2.6", "application/json", strings.NewReader(mutated),
			)
			require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
		})
	}

	unknownSibling := strings.Replace(body, `"id":"call_1"`, `"unexpected":true,"id":"call_1"`, 1)
	_, err = ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV4,
		"kimi-k2.6", "application/json", strings.NewReader(unknownSibling),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "json", KimiChatErrorCode(err))

	for _, duplicate := range []string{
		strings.Replace(body, `"id":"call_1"`, `"id":"shadow","id":"call_1"`, 1),
		strings.Replace(body, `"id":"call_1"`, `"id":"shadow","\u0069d":"call_1"`, 1),
	} {
		_, err = ValidateKimiChatFunctionTools(
			KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV4,
			"kimi-k2.6", "application/json", strings.NewReader(duplicate),
		)
		require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
		require.Equal(t, "json", KimiChatErrorCode(err))
		require.Equal(t, "json_duplicate_field", KimiChatDiagnosticCode(err))
	}
}

func TestKimiChatDiagnosticCodeNeverProjectsProviderValues(t *testing.T) {
	body := kimiProbeEnvelopePrefix + `"provider_extension":"secret-value","choices":[],"usage":{}}`
	_, err := ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV3,
		"kimi-k2.6", "application/json", strings.NewReader(body),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "json", KimiChatErrorCode(err))
	diagnostic := DescribeKimiChatDiagnostic(err)
	require.Equal(t, "json_unknown_field", diagnostic.Code)
	require.Equal(t, "provider_extension", diagnostic.Member)
	require.NotContains(t, diagnostic.Member, "secret-value")

	unsafeName := strings.Replace(body, "provider_extension", "private value", 1)
	_, err = ValidateKimiChatFunctionTools(
		KimiChatFunctionToolsProbeContractID, KimiChatFunctionToolsProbeContractVersionV3,
		"kimi-k2.6", "application/json", strings.NewReader(unsafeName),
	)
	diagnostic = DescribeKimiChatDiagnostic(err)
	require.Equal(t, "json_unknown_field", diagnostic.Code)
	require.Empty(t, diagnostic.Member)
}

func TestCanonicalizeKimiChatDiagnosticProjectsHistoricalDynamicCodeWithoutMutation(t *testing.T) {
	diagnostic := CanonicalizeKimiChatDiagnostic("json_unknown_field_provider_extension", "")
	require.Equal(t, "json_unknown_field", diagnostic.Code)
	require.Equal(t, "provider_extension", diagnostic.Member)

	diagnostic = CanonicalizeKimiChatDiagnostic("json_unknown_field_private value", "")
	require.Equal(t, "json_unknown_field", diagnostic.Code)
	require.Empty(t, diagnostic.Member)

	diagnostic = CanonicalizeKimiChatDiagnostic("json_type_mismatch", "untrusted_member")
	require.Equal(t, "json_type_mismatch", diagnostic.Code)
	require.Empty(t, diagnostic.Member)
}

func TestKimiChatJSONObjectProbeRejectsUnknownEnvelopeFields(t *testing.T) {
	body := kimiProbeEnvelopePrefix + `"choices":[{"index":0,"message":{"role":"assistant","content":"{\"status\":\"UNIROUTER_JSON_PROBE_OK\"}"},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":15,"completion_tokens":5,"total_tokens":20}}`
	observation, err := ValidateKimiChatJSONObject(
		KimiChatJSONObjectProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "application/json; charset=utf-8", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.StructuredOutputObserved)

	unknown := strings.Replace(body, `"model":"kimi-k2.6",`, `"model":"kimi-k2.6","provider_extension":"x",`, 1)
	_, err = ValidateKimiChatJSONObject(
		KimiChatJSONObjectProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "application/json", strings.NewReader(unknown),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "json", KimiChatErrorCode(err))
}

func TestKimiChatSSEProbeRequiresTerminalUsageAndDone(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl-kimi-probe","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}],"usage":null}`,
		`data: {"id":"chatcmpl-kimi-probe","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{"content":"UNIROUTER_STREAM_PROBE_OK"},"finish_reason":null}],"usage":null}`,
		`data: {"id":"chatcmpl-kimi-probe","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":2}}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
	observation, err := ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "text/event-stream", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.DoneSeen)
	require.Equal(t, int64(2), observation.CachedTokens)

	emptyReasoning := strings.Replace(
		body,
		`"content":"UNIROUTER_STREAM_PROBE_OK"`,
		`"content":"UNIROUTER_STREAM_PROBE_OK","reasoning_content":""`,
		1,
	)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "text/event-stream", strings.NewReader(emptyReasoning),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "reasoning_content", KimiChatErrorCode(err))

	observation, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV2,
		"kimi-k2.6", "text/event-stream", strings.NewReader(emptyReasoning),
	)
	require.NoError(t, err)
	require.True(t, observation.DoneSeen)
	require.Equal(t, int64(2), observation.CachedTokens)

	choiceUsage := strings.Join([]string{
		`data: {"id":"chatcmpl-kimi-probe","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}],"usage":null}`,
		`data: {"id":"chatcmpl-kimi-probe","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{"content":"UNIROUTER_STREAM_PROBE_OK","reasoning_content":""},"finish_reason":null}],"usage":null}`,
		`data: {"id":"chatcmpl-kimi-probe","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":2,"completion_tokens_details":{"reasoning_tokens":1}}}]}`,
		`data: {"id":"chatcmpl-kimi-probe","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":2,"completion_tokens_details":{"reasoning_tokens":1}}}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV2,
		"kimi-k2.6", "text/event-stream", strings.NewReader(choiceUsage),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	legacyNullableChoiceUsage := strings.Replace(
		choiceUsage,
		`"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":2,"completion_tokens_details":{"reasoning_tokens":1}}`,
		`"usage":null`,
		1,
	)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV2,
		"kimi-k2.6", "text/event-stream", strings.NewReader(legacyNullableChoiceUsage),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)

	observation, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV3,
		"kimi-k2.6", "text/event-stream", strings.NewReader(choiceUsage),
	)
	require.NoError(t, err)
	require.True(t, observation.DoneSeen)
	require.Equal(t, int64(2), observation.CachedTokens)

	choiceUsageWithoutTopLevel := strings.Replace(
		choiceUsage,
		`data: {"id":"chatcmpl-kimi-probe","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":2,"completion_tokens_details":{"reasoning_tokens":1}}}`+"\n\n",
		"",
		1,
	)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV3,
		"kimi-k2.6", "text/event-stream", strings.NewReader(choiceUsageWithoutTopLevel),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "terminal_order", KimiChatErrorCode(err))

	conflictingUsage := strings.Replace(
		choiceUsage,
		`"choices":[],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24`,
		`"choices":[],"usage":{"prompt_tokens":17,"completion_tokens":8,"total_tokens":25`,
		1,
	)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV3,
		"kimi-k2.6", "text/event-stream", strings.NewReader(conflictingUsage),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "usage_conflict", KimiChatErrorCode(err))

	nestedOnlyCache := strings.Replace(
		choiceUsage,
		`,"cached_tokens":2,"completion_tokens_details"`,
		`,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details"`,
		2,
	)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV3,
		"kimi-k2.6", "text/event-stream", strings.NewReader(nestedOnlyCache),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "cached_tokens", KimiChatErrorCode(err))

	conflictingCachedTokens := strings.Replace(
		choiceUsage,
		`,"cached_tokens":2,"completion_tokens_details"`,
		`,"cached_tokens":2,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details"`,
		1,
	)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV3,
		"kimi-k2.6", "text/event-stream", strings.NewReader(conflictingCachedTokens),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "cached_tokens_conflict", KimiChatErrorCode(err))

	reasoningExceedsCompletion := strings.Replace(choiceUsage, `"reasoning_tokens":1`, `"reasoning_tokens":9`, 1)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV3,
		"kimi-k2.6", "text/event-stream", strings.NewReader(reasoningExceedsCompletion),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "reasoning_tokens", KimiChatErrorCode(err))

	duplicateBeforeNormalization := strings.Replace(
		emptyReasoning,
		`"content":"UNIROUTER_STREAM_PROBE_OK"`,
		`"content":"sensitive","\u0063ontent":"UNIROUTER_STREAM_PROBE_OK"`,
		1,
	)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV3,
		"kimi-k2.6", "text/event-stream", strings.NewReader(duplicateBeforeNormalization),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
	require.Equal(t, "delivery_shape", KimiChatErrorCode(err))

	usageBeforeFinish := strings.Replace(body, `"finish_reason":"stop"`, `"finish_reason":null`, 1)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "text/event-stream", strings.NewReader(usageBeforeFinish),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)

	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatCoreProbeContractVersion,
		"kimi-k2.6", "text/event-stream", strings.NewReader(strings.Replace(body, "\n\ndata: [DONE]", "", 1)),
	)
	require.ErrorIs(t, err, ErrInvalidKimiChatProbe)
}
