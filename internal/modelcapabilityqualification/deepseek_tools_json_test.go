package modelcapabilityqualification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

func TestDeepSeekChatFunctionToolsProbeAcceptsOnlyExactToolCall(t *testing.T) {
	body := `{"model":"deepseek-v4-flash","choices":[{"finish_reason":"tool_calls","message":{"content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29}}`
	observation, err := ValidateDeepSeekChatFunctionTools(
		DeepSeekChatFunctionToolsProbeContractID, DeepSeekChatCoreProbeContractVersion,
		"application/json", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.ToolCallObserved)
	require.Equal(t, "tool_calls", observation.FinishReason)
	require.Equal(t, int64(29), observation.TotalTokens)

	_, err = ValidateDeepSeekChatFunctionTools(
		DeepSeekChatFunctionToolsProbeContractID, DeepSeekChatCoreProbeContractVersion,
		"application/json", strings.NewReader(strings.Replace(body, "unirouter_probe_echo", "other", 1)),
	)
	require.ErrorIs(t, err, ErrInvalidDeepSeekCoreCapability)
	require.Equal(t, "tool_call_identity", DeepSeekCoreErrorCode(err))
}

func TestDeepSeekChatFunctionToolsProbeV2MatchesExactDeliveryShape(t *testing.T) {
	body := `{"id":"chatcmpl-probe","object":"chat.completion","created":1787504000,"model":"deepseek-v4-flash","system_fingerprint":"fp","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"logprobs":null}],"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29}}`
	observation, err := ValidateDeepSeekChatFunctionTools(
		DeepSeekChatFunctionToolsProbeContractID, DeepSeekChatFunctionToolsProbeContractV2,
		"application/json", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.ToolCallObserved)

	for _, test := range []struct{ name, mutated string }{
		{name: "string index", mutated: strings.Replace(body, `"index":0,"id":"call_1"`, `"index":"0","id":"call_1"`, 1)},
		{name: "null index", mutated: strings.Replace(body, `"index":0,"id":"call_1"`, `"index":null,"id":"call_1"`, 1)},
		{name: "negative index", mutated: strings.Replace(body, `"index":0,"id":"call_1"`, `"index":-1,"id":"call_1"`, 1)},
		{name: "decimal index", mutated: strings.Replace(body, `"index":0,"id":"call_1"`, `"index":0.5,"id":"call_1"`, 1)},
		{name: "exponent index", mutated: strings.Replace(body, `"index":0,"id":"call_1"`, `"index":1e2,"id":"call_1"`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NotEqual(t, body, test.mutated)
			_, validationErr := ValidateDeepSeekChatFunctionTools(
				DeepSeekChatFunctionToolsProbeContractID, DeepSeekChatFunctionToolsProbeContractV2,
				"application/json", strings.NewReader(test.mutated),
			)
			require.ErrorIs(t, validationErr, ErrInvalidDeepSeekCoreCapability)
			require.Equal(t, "delivery_shape", DeepSeekCoreErrorCode(validationErr))
		})
	}
}

func TestDeepSeekChatJSONObjectProbeAcceptsOnlyExactJSONObject(t *testing.T) {
	body := `{"model":"deepseek-v4-flash","choices":[{"finish_reason":"stop","message":{"content":"{\"status\":\"UNIROUTER_JSON_PROBE_OK\"}"}}],"usage":{"prompt_tokens":15,"completion_tokens":5,"total_tokens":20}}`
	observation, err := ValidateDeepSeekChatJSONObject(
		DeepSeekChatJSONObjectProbeContractID, DeepSeekChatCoreProbeContractVersion,
		"application/json; charset=utf-8", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.True(t, observation.StructuredOutputObserved)
	require.Equal(t, int64(20), observation.TotalTokens)

	_, err = ValidateDeepSeekChatJSONObject(
		DeepSeekChatJSONObjectProbeContractID, DeepSeekChatCoreProbeContractVersion,
		"application/json", strings.NewReader(strings.Replace(body, `\"status\":`, `\"status\":\"wrong\",\"status\":`, 1)),
	)
	require.ErrorIs(t, err, ErrInvalidDeepSeekCoreCapability)
}

func TestCompiledDeepSeekCoreBindingsAreExact(t *testing.T) {
	bindings := DeepSeekFlashCoreBindings()
	require.Len(t, bindings, 12)
	for _, binding := range bindings {
		resolved, err := Resolve(binding.OperationID, binding.CapabilityID,
			binding.ExecutionProfileID, binding.ExecutionProfileVersion,
			binding.ProtocolProfileID, binding.ProtocolProfileVersion)
		require.NoError(t, err)
		require.Equal(t, binding, resolved)
	}
}

func TestCompiledDeepSeekCurrentProfileBindingsAreExactAndComplete(t *testing.T) {
	bindings := DeepSeekFlashCurrentBindings()
	require.Len(t, bindings, 18)
	for _, binding := range bindings {
		require.Equal(t, "1.5.0", binding.ExecutionProfileVersion)
		if binding.ProbeContractID == DeepSeekChatFunctionToolsProbeContractID {
			require.Equal(t, DeepSeekChatFunctionToolsProbeContractV2, binding.ProbeContractVersion)
		} else if binding.ProbeContractID == DeepSeekChatSSEProbeContractID {
			require.Equal(t, DeepSeekChatSSEProbeContractCurrentVersion, binding.ProbeContractVersion)
		} else {
			require.Equal(t, DeepSeekChatCoreProbeContractVersion, binding.ProbeContractVersion)
		}
		resolved, err := Resolve(binding.OperationID, binding.CapabilityID,
			binding.ExecutionProfileID, binding.ExecutionProfileVersion,
			binding.ProtocolProfileID, binding.ProtocolProfileVersion)
		require.NoError(t, err)
		require.Equal(t, binding, resolved)
	}

	for _, protocolProfileID := range []string{"protocol.deepseek-openai-chat.v4", "openai.chat_completions"} {
		resolved, err := ResolveAll(
			"openai.chat_completions.v1", "deepseek.v4-flash.api", "1.5.0",
			protocolProfileID, "1.0.0",
		)
		require.NoError(t, err)
		require.Len(t, resolved, 9)
	}
}

func TestResolvePreferredOperationProtocolUsesExactProfileIdentity(t *testing.T) {
	tests := []struct {
		profileVersion  string
		protocolProfile string
	}{
		{profileVersion: "1.4.0", protocolProfile: "protocol.deepseek-openai-chat.v4"},
		{profileVersion: "1.5.0", protocolProfile: "protocol.deepseek-openai-chat.v4"},
	}
	for _, test := range tests {
		t.Run(test.profileVersion, func(t *testing.T) {
			preferred, err := ResolvePreferredOperationProtocol(
				string(modeloperation.IDChatCompletions), "deepseek.v4-flash.api", test.profileVersion,
			)
			require.NoError(t, err)
			require.Equal(t, test.protocolProfile, preferred.ProtocolProfileID)
			require.Equal(t, "1.0.0", preferred.ProtocolProfileVersion)
			_, err = ResolveAll(
				preferred.OperationID,
				preferred.ExecutionProfileID, preferred.ExecutionProfileVersion,
				preferred.ProtocolProfileID, preferred.ProtocolProfileVersion,
			)
			require.NoError(t, err)
		})
	}

	_, err := ResolvePreferredOperationProtocol(
		string(modeloperation.IDChatCompletions), "deepseek.v4-flash.api", "1.6.0",
	)
	require.ErrorIs(t, err, ErrUnsupported)

	_, err = ResolvePreferredOperationProtocol(
		string(modeloperation.IDChatCompletions), "deepseek.v4-flash.api", "9.9.9",
	)
	require.ErrorIs(t, err, ErrUnsupported)
}
