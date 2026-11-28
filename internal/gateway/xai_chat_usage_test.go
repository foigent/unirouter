package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
)

func TestGrok47NormalChatSettlesVisibleAndReasoningExactlyOnce(t *testing.T) {
	// Numeric partition from the xAI Chat reference's same-shape response.
	// The assistant text and request identity are synthetic.
	const body = `{"id":"fixture","object":"chat.completion","model":"grok-4.7","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Ready."}}],"usage":{"prompt_tokens":26,"completion_tokens":168,"total_tokens":498,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":304}}}`
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"documented partition", body, true},
		{"contradictory total", strings.Replace(body, `"total_tokens":498`, `"total_tokens":194`, 1), false},
		{"double-counted reasoning", strings.Replace(body, `"completion_tokens":168`, `"completion_tokens":472`, 1), false},
		{"duplicate counter", strings.Replace(body, `"reasoning_tokens":304`, `"reasoning_tokens":0,"reasoning_tokens":304`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var delivered []billing.SettleRequest
			relay := newTestRelay(p6f11RelayDB(), &mockBilling{
				prepareDeliveryFn: func(_ context.Context, request billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
					delivered = append(delivered, request)
					return &billing.PrepareDeliveryResult{Funded: true}, nil
				},
			})
			route := &RouteResult{
				Channel: models.Channel{ID: 1, Provider: "xai"}, MappedModel: "grok-4.7",
				ModelOperationID: modelprofile.OperationOpenAIChat, ModelProtocolProfile: modelprofile.PlainTextChatMaxTokensProtocol,
				CacheUsageProfile: modelprofile.XAIChatAdditiveReasoningCacheUsageProfile,
			}
			recorder := httptest.NewRecorder()
			ctx := contextWithRouteAttempt(contextWithAttemptID(t.Context(), "att-xai-chat-fixture"), 1, 1)
			relay.relayNonStreamRequest(ctx, recorder, &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(tc.body)),
			}, route, "xai-chat-fixture", 1, 2, "public-model", "public-model", "127.0.0.1",
				logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 26, 1024, "")
			if !tc.valid {
				require.NotEqual(t, http.StatusOK, recorder.Code)
				require.Empty(t, delivered)
				require.NotContains(t, recorder.Body.String(), "Ready.")
				return
			}
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Len(t, delivered, 1)
			require.Equal(t, metering.UsageSourceUpstream, delivered[0].UsageSource)
			require.Equal(t, 26, delivered[0].PromptTokens)
			require.Equal(t, 22, delivered[0].UncachedPromptTokens)
			require.Equal(t, 4, delivered[0].CacheReadTokens)
			require.Equal(t, 472, delivered[0].CompletionTokens)
			require.Contains(t, recorder.Body.String(), `"reasoning_tokens":304`)
		})
	}
	oldUsage, present, err := adapters.ParseOpenAICompatibleUsageWithCacheProfile([]byte(body), adapters.EndpointChatCompletions,
		metering.UsageSourceUpstream, "cache-usage.openai-compatible-read.v1")
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 168, oldUsage.CompletionTokens, "legacy profiles keep their original completion accounting")
}
