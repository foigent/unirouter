package modelcapabilityqualification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const kimiProbeEnvelopePrefix = `{"id":"chatcmpl-kimi-probe","object":"chat.completion","created":1787932800,"model":"kimi-k2.6",`

func TestCompiledKimiK26BindingsAreExactAndDoNotAuthorizeSiblingModels(t *testing.T) {
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
			require.Equal(t, KimiChatSSEProbeContractVersionV2, binding.ProbeContractVersion)
		} else {
			require.Equal(t, KimiChatCoreProbeContractVersion, binding.ProbeContractVersion)
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

	_, err := ResolveAll(
		"openai.chat_completions.v1", "kimi.k2.7-code.api", "1.3.0",
		"protocol.kimi-openai-chat.v5", "1.0.0",
	)
	require.ErrorIs(t, err, ErrUnsupported)
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

	duplicateBeforeNormalization := strings.Replace(
		emptyReasoning,
		`"content":"UNIROUTER_STREAM_PROBE_OK"`,
		`"content":"sensitive","\u0063ontent":"UNIROUTER_STREAM_PROBE_OK"`,
		1,
	)
	_, err = ValidateKimiChatSSE(
		KimiChatSSEProbeContractID, KimiChatSSEProbeContractVersionV2,
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
