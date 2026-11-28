package adapters

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestQwenOmniProviderUsagePartitions(t *testing.T) {
	// Use the counters from the provider's published audio SSE final frame.
	const final = `{"model":"qwen3.8-omni-flash","choices":[],"usage":{"prompt_tokens":171,"total_tokens":640,"completion_tokens":469,"prompt_tokens_details":{"cached_tokens":0,"audio_tokens":114,"text_tokens":57},"completion_tokens_details":{"reasoning_tokens":416,"text_tokens":53}}}`
	read := func(input, profile string, stream bool) (Usage, bool, error) {
		if stream {
			return ParseOpenAICompatibleStreamUsageWithCacheProfile([]byte(input), EndpointChatCompletions, metering.UsageSourceUpstream, profile)
		}
		return ParseOpenAICompatibleUsageWithCacheProfile([]byte(input), EndpointChatCompletions, metering.UsageSourceUpstream, profile)
	}
	for _, stream := range []bool{false, true} {
		u, observed, err := read(final, modelprofile.QwenOmniUsageProfile, stream)
		require.NoError(t, err)
		require.True(t, observed)
		require.Equal(t, 171, u.PromptTokens)
		require.Equal(t, 171, u.UncachedPromptTokens)
		require.Equal(t, 469, u.CompletionTokens)
		require.Equal(t, 640, u.TotalTokens)
		require.NotNil(t, u.ProviderReasoningTokens)
		require.Equal(t, 416, *u.ProviderReasoningTokens)
		require.True(t, u.FitsProviderCompletionSplit(53, 416))
	}

	legacy, observed, err := read(final, "cache-usage.qwen-context-cache.v1", true)
	require.NoError(t, err)
	require.True(t, observed)
	require.Nil(t, legacy.ProviderReasoningTokens)
	_, _, err = read(final, modelprofile.QwenHostedSplitUsageProfile, true)
	require.ErrorContains(t, err, "invalid hosted Qwen completion partition")

	for _, tc := range []struct{ from, to, errorText string }{
		{`"audio_tokens":114`, `"audio_tokens":115`, "invalid Qwen Omni prompt partition"},
		{`"text_tokens":53`, `"text_tokens":469`, "invalid Qwen Omni completion partition"},
		{`"reasoning_tokens":416`, `"reasoning_tokens":417`, "invalid Qwen Omni completion partition"},
		{`"total_tokens":640`, `"total_tokens":641`, "Qwen Omni requires complete provider totals"},
		{`"cached_tokens":0`, `"cached_tokens":0,"cached_tokens":1`, "duplicate"},
		{`"cached_tokens":0`, `"cached_tokens":0,"cache_creation_input_tokens":1`, "without another cache schema"},
	} {
		_, _, err := read(strings.Replace(final, tc.from, tc.to, 1), modelprofile.QwenOmniUsageProfile, true)
		require.ErrorContains(t, err, tc.errorText)
	}
	u, _, err := read(strings.Replace(final, `"reasoning_tokens":416,`, "", 1), modelprofile.QwenOmniUsageProfile, true)
	require.NoError(t, err)
	require.Nil(t, u.ProviderReasoningTokens, "missing reasoning is not inferred")
}
