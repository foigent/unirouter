package adapters

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestKimiChatReadWriteUsageContract(t *testing.T) {
	// Values from the official K3.1 streaming usage example. Both transports
	// consume the same inclusive prompt and completion totals.
	const totals = `{"usage":{"prompt_tokens":309,"completion_tokens":101,"total_tokens":410`
	for _, tc := range []struct {
		name, cache, status   string
		uncached, read, write int
	}{
		{"write only", `,"prompt_tokens_details":{"cache_write_tokens":256}`, CacheUsageStatusWriteReported, 53, 0, 256},
		{"zero write", `,"prompt_tokens_details":{"cache_write_tokens":0}`, CacheUsageStatusWriteReported, 309, 0, 0},
		{"read and write", `,"prompt_tokens_details":{"cached_tokens":8,"cache_write_tokens":256}`, CacheUsageStatusReadWriteReported, 45, 8, 256},
		{"read and write with mirror", `,"cached_tokens":8,"prompt_tokens_details":{"cached_tokens":8,"cache_write_tokens":256}`, CacheUsageStatusReadWriteReported, 45, 8, 256},
		{"read only", `,"prompt_tokens_details":{"cached_tokens":8}`, CacheUsageStatusReadReported, 301, 8, 0},
		{"no cache counters", "", CacheUsageStatusNotReported, 309, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, parse := range []func([]byte, Endpoint, string, string) (Usage, bool, error){
				ParseOpenAICompatibleUsageWithCacheProfile,
				ParseOpenAICompatibleStreamUsageWithCacheProfile,
			} {
				usage, present, err := parse([]byte(totals+tc.cache+`}}`), EndpointChatCompletions, metering.UsageSourceUpstream, modelprofile.KimiChatReadWriteCacheUsageProfile)
				require.NoError(t, err)
				require.True(t, present)
				require.Equal(t, tc.status, usage.CacheUsageStatus)
				require.Equal(t, 309, usage.PromptTokens)
				require.Equal(t, tc.uncached, usage.UncachedPromptTokens)
				require.Equal(t, tc.read, usage.CacheReadTokens)
				require.Equal(t, tc.write, usage.CacheCreationTokens)
				require.Equal(t, 101, usage.CompletionTokens)
				require.Equal(t, 410, usage.TotalTokens)
			}
		})
	}
	for name, cache := range map[string]string{
		"wrong mirror":        `,"cached_tokens":7,"prompt_tokens_details":{"cached_tokens":8,"cache_write_tokens":256}`,
		"mirror without read": `,"cached_tokens":8,"prompt_tokens_details":{"cache_write_tokens":256}`,
		"negative mirror":     `,"cached_tokens":-1,"prompt_tokens_details":{"cached_tokens":0}`,
		"negative read":       `,"prompt_tokens_details":{"cached_tokens":-1}`,
		"negative write":      `,"prompt_tokens_details":{"cache_write_tokens":-1}`,
		"fractional write":    `,"prompt_tokens_details":{"cache_write_tokens":1.5}`,
		"overlapping classes": `,"prompt_tokens_details":{"cached_tokens":54,"cache_write_tokens":256}`,
		"other write schema":  `,"prompt_tokens_details":{"cache_creation_input_tokens":10}`,
		"other hit schema":    `,"prompt_cache_hit_tokens":10,"prompt_cache_miss_tokens":299`,
		"duplicate counter":   `,"prompt_tokens_details":{"cache_write_tokens":0,"cache_write_tokens":256}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := ParseOpenAICompatibleStreamUsageWithCacheProfile([]byte(totals+cache+`}}`), EndpointChatCompletions, metering.UsageSourceUpstream, modelprofile.KimiChatReadWriteCacheUsageProfile)
			require.Error(t, err)
		})
	}
	_, _, err := ParseOpenAICompatibleStreamUsageWithCacheProfile(
		[]byte(totals+`,"prompt_tokens_details":{"cache_write_tokens":256}}}`),
		EndpointChatCompletions, metering.UsageSourceUpstream, kimiChatCacheUsageProfile,
	)
	require.Error(t, err, "the old immutable cache profile must still reject writes")
}
