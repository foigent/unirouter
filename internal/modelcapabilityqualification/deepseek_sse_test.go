package modelcapabilityqualification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekChatSSEProbeContractV1PreservesHistoricalUsageOnlyChunk(t *testing.T) {
	lines := []string{
		`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"content":"UNIROUTER_STREAM_PROBE_OK"},"finish_reason":"stop"}]}`,
		"",
		`data: {"model":"deepseek-v4-flash","choices":[],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24}}`,
		"",
		`data: [DONE]`,
		"",
	}
	for name, separator := range map[string]string{"lf": "\n", "crlf": "\r\n", "cr": "\r"} {
		t.Run(name, func(t *testing.T) {
			valid := strings.Join(lines, separator)
			observation, err := ValidateDeepSeekChatSSE(
				DeepSeekChatSSEProbeContractID, DeepSeekChatSSEProbeContractVersionV1,
				"text/event-stream; charset=utf-8", strings.NewReader(valid),
			)
			require.NoError(t, err)
			require.Equal(t, "deepseek-v4-flash", observation.ReportedModel)
			require.Equal(t, "stop", observation.FinishReason)
			require.Equal(t, int64(24), observation.TotalTokens)
			require.True(t, observation.UsageSeen)
			require.True(t, observation.DoneSeen)
			require.Equal(t, len(valid), observation.ResponseBytes)
		})
	}
}

func TestDeepSeekChatSSEProbeContractV2AcceptsDocumentedFinalChoiceUsage(t *testing.T) {
	lines := []string{
		`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"role":"assistant"},"finish_reason":null}],"usage":null}`,
		"",
		`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"content":"UNIROUTER_STREAM_PROBE_OK"},"finish_reason":null}],"usage":null}`,
		"",
		`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24}}`,
		"",
		`data: [DONE]`,
		"",
	}
	for name, separator := range map[string]string{"lf": "\n", "crlf": "\r\n", "cr": "\r"} {
		t.Run(name, func(t *testing.T) {
			valid := strings.Join(lines, separator)
			observation, err := ValidateDeepSeekChatSSE(
				DeepSeekChatSSEProbeContractID, DeepSeekChatSSEProbeContractCurrentVersion,
				"text/event-stream; charset=utf-8", strings.NewReader(valid),
			)
			require.NoError(t, err)
			require.Equal(t, "deepseek-v4-flash", observation.ReportedModel)
			require.Equal(t, "stop", observation.FinishReason)
			require.Equal(t, int64(24), observation.TotalTokens)
			require.True(t, observation.UsageSeen)
			require.True(t, observation.DoneSeen)
			require.Equal(t, len(valid), observation.ResponseBytes)
		})
	}
}

func TestDeepSeekChatSSEProbeUsesBindingOwnedProModelIdentity(t *testing.T) {
	body := strings.Join([]string{
		`data: {"model":"deepseek-v4-pro","choices":[{"delta":{"role":"assistant"},"finish_reason":null}],"usage":null}`,
		"",
		`data: {"model":"deepseek-v4-pro","choices":[{"delta":{"content":"UNIROUTER_STREAM_PROBE_OK"},"finish_reason":null}],"usage":null}`,
		"",
		`data: {"model":"deepseek-v4-pro","choices":[{"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	observation, err := ValidateDeepSeekChatSSEForModel(
		DeepSeekChatSSEProbeContractID, DeepSeekChatSSEProbeContractCurrentVersion,
		"deepseek-v4-pro", "text/event-stream", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.Equal(t, "deepseek-v4-pro", observation.ReportedModel)

	_, err = ValidateDeepSeekChatSSEForModel(
		DeepSeekChatSSEProbeContractID, DeepSeekChatSSEProbeContractCurrentVersion,
		"deepseek-v4-flash", "text/event-stream", strings.NewReader(body),
	)
	require.ErrorIs(t, err, ErrInvalidDeepSeekSSE)
	require.Equal(t, "reported_model", DeepSeekSSEErrorCode(err))
}

func TestDeepSeekChatSSEProbeContractVersionsDoNotReinterpretEachOther(t *testing.T) {
	historical := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		"",
		`data: {"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	_, err := ValidateDeepSeekChatSSE(
		DeepSeekChatSSEProbeContractID, DeepSeekChatSSEProbeContractCurrentVersion,
		"text/event-stream", strings.NewReader(historical),
	)
	require.ErrorIs(t, err, ErrInvalidDeepSeekSSE)
	require.Equal(t, "final_usage", DeepSeekSSEErrorCode(err))

	current := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}],"usage":null}`,
		"",
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	_, err = ValidateDeepSeekChatSSE(
		DeepSeekChatSSEProbeContractID, DeepSeekChatSSEProbeContractVersionV1,
		"text/event-stream", strings.NewReader(current),
	)
	require.ErrorIs(t, err, ErrInvalidDeepSeekSSE)
	require.Equal(t, "usage_mixed_with_choices", DeepSeekSSEErrorCode(err))
}

func TestDeepSeekChatSSEProbeContractV2RejectsNonTerminalUsage(t *testing.T) {
	for name, body := range map[string]string{
		"usage before finish": strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			"", `data: [DONE]`, "",
		}, "\n"),
		"content with final usage": strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			"", `data: [DONE]`, "",
		}, "\n"),
		"data after final usage": strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}],"usage":null}`,
			"",
			`data: {"choices":[{"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			"",
			`data: {"choices":[{"delta":{"content":"late"},"finish_reason":null}],"usage":null}`,
			"", `data: [DONE]`, "",
		}, "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateDeepSeekChatSSE(
				DeepSeekChatSSEProbeContractID, DeepSeekChatSSEProbeContractCurrentVersion,
				"text/event-stream", strings.NewReader(body),
			)
			require.ErrorIs(t, err, ErrInvalidDeepSeekSSE)
		})
	}
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
			_, err := ValidateDeepSeekChatSSE(
				DeepSeekChatSSEProbeContractID, DeepSeekChatSSEProbeContractCurrentVersion,
				"text/event-stream", strings.NewReader(test.body),
			)
			require.ErrorIs(t, err, ErrInvalidDeepSeekSSE)
		})
	}
}

func TestDeepSeekChatSSEProbePreservesBoundedFailureEvidence(t *testing.T) {
	body := strings.Join([]string{
		`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"content":"partial"},"finish_reason":null}]}`,
		"",
		"event: unsupported-by-the-deepseek-probe-contract",
		"",
	}, "\r\n")

	observation, err := ValidateDeepSeekChatSSE(
		DeepSeekChatSSEProbeContractID, DeepSeekChatSSEProbeContractCurrentVersion,
		"text/event-stream", strings.NewReader(body),
	)
	require.ErrorIs(t, err, ErrInvalidDeepSeekSSE)
	require.Equal(t, "unsupported_field", DeepSeekSSEErrorCode(err))
	require.Equal(t, "deepseek-v4-flash", observation.ReportedModel)
	require.Equal(t, 1, observation.DataEventCount)
	require.Equal(t, 1, observation.ContentDeltaCount)
	require.Equal(t, len(body), observation.ResponseBytes)
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
