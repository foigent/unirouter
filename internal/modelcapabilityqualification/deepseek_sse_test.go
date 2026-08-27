package modelcapabilityqualification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekChatSSEProbeContract(t *testing.T) {
	valid := strings.Join([]string{
		`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"content":"UNIROUTER_STREAM_PROBE_OK"},"finish_reason":"stop"}]}`,
		"",
		`data: {"model":"deepseek-v4-flash","choices":[],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	observation, err := ValidateDeepSeekChatSSE("text/event-stream; charset=utf-8", strings.NewReader(valid))
	require.NoError(t, err)
	require.Equal(t, "deepseek-v4-flash", observation.ReportedModel)
	require.Equal(t, "stop", observation.FinishReason)
	require.Equal(t, int64(24), observation.TotalTokens)
	require.True(t, observation.UsageSeen)
	require.True(t, observation.DoneSeen)
}

func TestDeepSeekChatSSEProbeRejectsIncompleteOrMisorderedEvidence(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing usage", body: "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"},
		{name: "missing done", body: "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"},
		{name: "usage before finish", body: "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n"},
		{name: "data after done", body: "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\ndata: {}\n\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateDeepSeekChatSSE("text/event-stream", strings.NewReader(test.body))
			require.ErrorIs(t, err, ErrInvalidDeepSeekSSE)
		})
	}
}

func TestCompiledDeepSeekStreamingBindingIsExact(t *testing.T) {
	for _, binding := range []Binding{
		DeepSeekFlashStreamingBinding(),
		DeepSeekFlashLegacyStreamingBinding(),
	} {
		resolved, err := Resolve(binding.OperationID, binding.CapabilityID,
			binding.ExecutionProfileID, binding.ExecutionProfileVersion,
			binding.ProtocolProfileID, binding.ProtocolProfileVersion)
		require.NoError(t, err)
		require.Equal(t, binding, resolved)
	}
	binding := DeepSeekFlashStreamingBinding()
	_, err := Resolve(binding.OperationID, binding.CapabilityID,
		binding.ExecutionProfileID, binding.ExecutionProfileVersion,
		"openai.chat_completions", "9.9.9")
	require.ErrorIs(t, err, ErrUnsupported)
	_, err = Resolve(binding.OperationID, binding.CapabilityID,
		binding.ExecutionProfileID, "1.3.0", binding.ProtocolProfileID, binding.ProtocolProfileVersion)
	require.ErrorIs(t, err, ErrUnsupported)
}
