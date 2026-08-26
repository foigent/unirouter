package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

const doubaoEvolvingCacheUsageProfile = "cache-usage.doubao-ark-chat.v1"

func TestDoubaoSeedEvolvingImplicitCacheProfileEvidenceIsAppendOnly(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.8.0")
	evidence, err := validateChatExecutionProfileRequest(
		[]byte(`{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"hello"}]}`),
		plan,
	)
	require.NoError(t, err)
	require.Equal(t, doubaoEvolvingCacheUsageProfile, evidence.CacheUsageProfile)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v6", evidence.ModelProtocolProfile)

	historical := executionProfilePlan(t, "doubao.seed-evolving.api", "1.7.0")
	historicalEvidence, err := validateChatExecutionProfileRequest(
		[]byte(`{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"hello"}]}`),
		historical,
	)
	require.NoError(t, err)
	require.Empty(t, historicalEvidence.CacheUsageProfile)
}

func TestDoubaoSeedEvolvingFinalStreamUsageUsesMandatoryCacheReadCounter(t *testing.T) {
	var settled billing.SettleRequest
	relay := newTestRelay(p6f11RelayDB(), &mockBilling{settleFn: func(_ context.Context, request billing.SettleRequest) (*billing.SettleResult, error) {
		settled = request
		return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
	}})
	route := &RouteResult{
		Channel:              models.Channel{ID: 1, Name: "Doubao Seed Evolving", Provider: "doubao"},
		CacheUsageProfile:    doubaoEvolvingCacheUsageProfile,
		ModelProtocolProfile: "protocol.doubao-seed-evolving-openai-chat.v6",
	}
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":13,\"completion_tokens\":4,\"total_tokens\":17,\"prompt_tokens_details\":{\"cached_tokens\":8}}}\n\n" +
		"data: [DONE]\n\n"
	recorder := httptest.NewRecorder()
	relay.relayStreamRequest(
		p6f11StreamContext(nil, "req-doubao-cache-stream"), recorder,
		&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}, route,
		"req-doubao-cache-stream", 1, 2, "doubao-seed-evolving", "doubao-seed-evolving", "127.0.0.1", nil,
		logger.New(logger.Fields{}), time.Now(), 1_000, nil, "",
	)
	require.NotContains(t, recorder.Body.String(), "stream_error.invalid_frame")
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
	require.Equal(t, 13, settled.PromptTokens)
	require.Equal(t, 5, settled.UncachedPromptTokens)
	require.Equal(t, 8, settled.CacheReadTokens)
	require.Equal(t, adapters.CacheUsageSchemaOpenAIV1, settled.CacheUsageSchema)
	require.Equal(t, adapters.CacheUsageStatusReadReported, settled.CacheUsageStatus)
	require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)

	missing := strings.Replace(stream, `,"prompt_tokens_details":{"cached_tokens":8}`, "", 1)
	missingRecorder := httptest.NewRecorder()
	relay.relayStreamRequest(
		p6f11StreamContext(nil, "req-doubao-cache-stream-missing"), missingRecorder,
		&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(missing))}, route,
		"req-doubao-cache-stream-missing", 1, 2, "doubao-seed-evolving", "doubao-seed-evolving", "127.0.0.1", nil,
		logger.New(logger.Fields{}), time.Now(), 1_000, nil, "",
	)
	require.Contains(t, missingRecorder.Body.String(), "stream_error.invalid_frame")
	require.NotContains(t, missingRecorder.Body.String(), "data: [DONE]")
}

func TestDoubaoSeedEvolvingNonStreamMissingCacheCounterStopsBeforeCustomerDelivery(t *testing.T) {
	tests := []struct {
		name  string
		usage string
	}{
		{name: "missing cache counter", usage: `,"usage":{"prompt_tokens":13,"completion_tokens":4,"total_tokens":17}`},
		{name: "missing usage", usage: ""},
		{name: "null usage", usage: `,"usage":null`},
		{name: "missing prompt core", usage: `,"usage":{"completion_tokens":4,"total_tokens":4,"prompt_tokens_details":{"cached_tokens":0}}`},
		{name: "missing completion core", usage: `,"usage":{"prompt_tokens":13,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":0}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var prepares, settlements atomic.Int64
			relay := newTestRelay(p6f11RelayDB(), &mockBilling{
				prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
					prepares.Add(1)
					return &billing.PrepareDeliveryResult{Funded: true}, nil
				},
				settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
					settlements.Add(1)
					return &billing.SettleResult{Success: true}, nil
				},
			})
			route := &RouteResult{
				Channel:              models.Channel{ID: 1, Name: "Doubao Seed Evolving", Provider: "doubao"},
				CacheUsageProfile:    doubaoEvolvingCacheUsageProfile,
				ModelProtocolProfile: "protocol.doubao-seed-evolving-openai-chat.v6",
			}
			body := `{"id":"chatcmpl-cache-missing","object":"chat.completion","model":"doubao-seed-evolving","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"answer"}}]` + test.usage + `}`
			requestID := "req-doubao-cache-" + strings.ReplaceAll(test.name, " ", "-")
			ctx := contextWithRouteAttempt(contextWithAttemptID(context.Background(), "att-"+requestID), 1, 1)
			recorder := httptest.NewRecorder()
			relay.relayNonStreamRequest(
				ctx, recorder,
				&http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))},
				route, requestID, 1, 2, "doubao-seed-evolving", "doubao-seed-evolving", "127.0.0.1",
				logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 13, 4, "upstream-cache-missing",
			)
			require.Equal(t, http.StatusInternalServerError, recorder.Code)
			require.Contains(t, recorder.Body.String(), "server_error.internal_error")
			require.NotContains(t, recorder.Body.String(), "answer")
			require.Zero(t, prepares.Load())
			require.Zero(t, settlements.Load())
		})
	}
}

func TestDoubaoSeedEvolvingAutomaticCacheCapabilitiesAreProfileRequired(t *testing.T) {
	profile, ok := modelprofile.Lookup("doubao.seed-evolving.api", "1.8.0")
	require.True(t, ok)
	require.ElementsMatch(t, []string{
		modelprofile.CapabilityPromptCache,
		modelprofile.CapabilityCacheReadUsage,
	}, profile.AlwaysRequiredCapabilities)
}
