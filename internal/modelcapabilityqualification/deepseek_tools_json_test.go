package modelcapabilityqualification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
