package adapters

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/models"
)

const miniMaxCacheThresholdProfile = "cache-usage.minimax-openai.v2"

func TestMiniMaxCacheThresholdRequiresReadCounterOnlyForEligiblePrompts(t *testing.T) {
	tests := []struct {
		name          string
		promptTokens  int
		cacheMember   string
		wantErr       string
		wantCacheRead bool
	}{
		{name: "511 absent is ineligible", promptTokens: 511},
		{name: "512 absent is rejected", promptTokens: 512, wantErr: "requires prompt_tokens_details.cached_tokens"},
		{name: "511 explicit zero is authoritative", promptTokens: 511, cacheMember: `,"prompt_tokens_details":{"cached_tokens":0}`, wantCacheRead: true},
		{name: "512 explicit zero is authoritative", promptTokens: 512, cacheMember: `,"prompt_tokens_details":{"cached_tokens":0}`, wantCacheRead: true},
		{name: "511 explicit null is rejected", promptTokens: 511, cacheMember: `,"prompt_tokens_details":{"cached_tokens":null}`, wantErr: "rejects null"},
		{name: "512 explicit null is rejected", promptTokens: 512, cacheMember: `,"prompt_tokens_details":{"cached_tokens":null}`, wantErr: "rejects null"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"usage":{"prompt_tokens":%d,"completion_tokens":7,"total_tokens":%d%s}}`,
				test.promptTokens, test.promptTokens+7, test.cacheMember))
			usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
				body, EndpointChatCompletions, metering.UsageSourceUpstream, miniMaxCacheThresholdProfile,
			)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				require.True(t, present)
				return
			}
			require.NoError(t, err)
			require.True(t, present)
			require.Equal(t, test.wantCacheRead, usage.HasAuthoritativeCacheUsage())
			if !test.wantCacheRead {
				require.Empty(t, usage.CacheUsageSchema)
				require.Equal(t, CacheUsageStatusNotReported, usage.CacheUsageStatus)
			}
		})
	}
}

func TestMiniMaxCacheThresholdStillRequiresTheFinalUsageObject(t *testing.T) {
	adapter := NewOpenAICompatible()
	_, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: 200,
		Body:           []byte(`{"id":"minimax-no-usage","model":"MiniMax-M2.7","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`),
		Channel:        models.Channel{Provider: "minimax", AdapterType: TypeOpenAICompatible},
		RequestedModel: "minimax-m2.7", UpstreamModel: "MiniMax-M2.7",
		CacheUsageProfile: miniMaxCacheThresholdProfile,
		ProtocolProfile:   "protocol.minimax-m2-openai-chat.v7",
	})
	require.ErrorContains(t, err, "requires final response usage")
}

func TestMiniMaxCacheThresholdStreamingTerminalUsageDoesNotInventCacheAuthority(t *testing.T) {
	belowThreshold := []byte(`{"id":"chunk","object":"chat.completion.chunk","model":"MiniMax-M2.7","choices":[],"usage":{"prompt_tokens":511,"completion_tokens":7,"total_tokens":518}}`)
	usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		belowThreshold, EndpointChatCompletions, metering.UsageSourceUpstream, miniMaxCacheThresholdProfile,
	)
	require.NoError(t, err)
	require.True(t, present, "the terminal core usage object remains authoritative")
	require.False(t, usage.HasAuthoritativeCacheUsage(), "cache authority is absent below the documented eligibility threshold")

	eligibleMissing := []byte(`{"id":"chunk","object":"chat.completion.chunk","model":"MiniMax-M2.7","choices":[],"usage":{"prompt_tokens":512,"completion_tokens":7,"total_tokens":519}}`)
	_, present, err = ParseOpenAICompatibleUsageWithCacheProfile(
		eligibleMissing, EndpointChatCompletions, metering.UsageSourceUpstream, miniMaxCacheThresholdProfile,
	)
	require.ErrorContains(t, err, "requires prompt_tokens_details.cached_tokens")
	require.True(t, present)
}
