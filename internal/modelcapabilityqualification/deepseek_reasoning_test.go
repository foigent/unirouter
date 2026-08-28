package modelcapabilityqualification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekChatReasoningProbeContractsAcceptBoundedEvidence(t *testing.T) {
	body := `{"model":"deepseek-v4-flash","choices":[{"finish_reason":"stop","message":{"content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private reasoning"}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`
	for _, contractID := range []string{
		DeepSeekChatReasoningProbeContractID,
		DeepSeekChatReasoningEffortProbeContractID,
	} {
		observation, err := ValidateDeepSeekChatReasoning(
			contractID, DeepSeekChatReasoningProbeContractVersion,
			"application/json; charset=utf-8", strings.NewReader(body),
		)
		require.NoError(t, err)
		require.Equal(t, "deepseek-v4-flash", observation.ReportedModel)
		require.Equal(t, "stop", observation.FinishReason)
		require.Equal(t, int64(19), observation.TotalTokens)
		require.True(t, observation.ReasoningSeen)
	}
}

func TestDeepSeekChatReasoningProbeUsesBindingOwnedProModelIdentity(t *testing.T) {
	body := `{"model":"deepseek-v4-pro","choices":[{"finish_reason":"stop","message":{"content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private reasoning"}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`
	observation, err := ValidateDeepSeekChatReasoningForModel(
		DeepSeekChatReasoningProbeContractID, DeepSeekChatReasoningProbeContractVersion,
		"deepseek-v4-pro", "application/json", strings.NewReader(body),
	)
	require.NoError(t, err)
	require.Equal(t, "deepseek-v4-pro", observation.ReportedModel)
	require.True(t, observation.ReasoningSeen)

	_, err = ValidateDeepSeekChatReasoningForModel(
		DeepSeekChatReasoningProbeContractID, DeepSeekChatReasoningProbeContractVersion,
		"deepseek-v4-flash", "application/json", strings.NewReader(body),
	)
	require.ErrorIs(t, err, ErrInvalidDeepSeekReasoning)
	require.Equal(t, "reported_model", DeepSeekReasoningErrorCode(err))
}

func TestDeepSeekChatReasoningProbeRejectsMissingOrInvalidEvidence(t *testing.T) {
	valid := `{"model":"deepseek-v4-flash","choices":[{"finish_reason":"stop","message":{"content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"work"}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "wrong model", body: strings.Replace(valid, "deepseek-v4-flash", "deepseek-v4-pro", 1), code: "reported_model"},
		{name: "missing reasoning", body: strings.Replace(valid, `"reasoning_content":"work"`, `"reasoning_content":null`, 1), code: "reasoning_content"},
		{name: "wrong answer", body: strings.Replace(valid, "UNIROUTER_REASONING_PROBE_OK", "almost", 1), code: "content"},
		{name: "truncated", body: strings.Replace(valid, `"finish_reason":"stop"`, `"finish_reason":"length"`, 1), code: "finish_reason"},
		{name: "usage conflict", body: strings.Replace(valid, `"total_tokens":19`, `"total_tokens":18`, 1), code: "usage_total"},
		{name: "duplicate field", body: strings.Replace(valid, `"model":"deepseek-v4-flash"`, `"model":"deepseek-v4-flash","model":"deepseek-v4-flash"`, 1), code: "json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateDeepSeekChatReasoning(
				DeepSeekChatReasoningProbeContractID, DeepSeekChatReasoningProbeContractVersion,
				"application/json", strings.NewReader(test.body),
			)
			require.ErrorIs(t, err, ErrInvalidDeepSeekReasoning)
			require.Equal(t, test.code, DeepSeekReasoningErrorCode(err))
		})
	}
}

func TestCompiledDeepSeekReasoningBindingsAreExact(t *testing.T) {
	for _, binding := range []Binding{
		DeepSeekFlashReasoningBinding(), DeepSeekFlashLegacyReasoningBinding(),
		DeepSeekFlashReasoningEffortBinding(), DeepSeekFlashLegacyReasoningEffortBinding(),
	} {
		resolved, err := Resolve(binding.OperationID, binding.CapabilityID,
			binding.ExecutionProfileID, binding.ExecutionProfileVersion,
			binding.ProtocolProfileID, binding.ProtocolProfileVersion)
		require.NoError(t, err)
		require.Equal(t, binding, resolved)
	}
}
