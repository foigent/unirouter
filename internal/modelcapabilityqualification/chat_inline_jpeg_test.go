package modelcapabilityqualification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekInlineJPEGRejectsResponseFromDifferentImage(t *testing.T) {
	const model, cache = "deepseek-flash", "cache-usage.deepseek-v4.v1"
	wire := chatInlineJPEGWire{outputField: "max_tokens", outputLimit: chatInlineJPEGProbeOutputLimit, thinking: "disabled"}
	red, err := buildChatInlineJPEGChallenge(model, cache, [6]byte{}, wire)
	require.NoError(t, err)
	green, err := buildChatInlineJPEGChallenge(model, cache, [6]byte{1, 1, 1, 1, 1, 1}, wire)
	require.NoError(t, err)
	require.NotEqual(t, red.Body, green.Body)
	const redResponse = `{"model":"deepseek-flash","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"111111"}}],"usage":{"prompt_tokens":100,"completion_tokens":4,"total_tokens":104,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":100}}`
	_, _, err = red.Observe("application/json", strings.NewReader(redResponse))
	require.NoError(t, err)
	_, _, err = green.Observe("application/json", strings.NewReader(redResponse))
	require.ErrorContains(t, err, "image challenge")
}
