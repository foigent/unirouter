package gateway

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const (
	miniMaxM27CacheProfile  = "cache-usage.minimax-openai.v1"
	miniMaxM27CacheProtocol = "protocol.minimax-m2-openai-chat.v6"
)

func TestMiniMaxM27CacheReadCandidateUsesOnePlanSnapshot(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m2.7.token-plan", "1.4.0")
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].UpstreamModel = "MiniMax-M2.7"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true,"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m27-cache",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityCacheReadUsage, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityPromptCache, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityStreaming, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Equal(t, miniMaxM27CacheProfile, batch.routes[0].compat.CacheUsageProfile)
	require.Equal(t, []string{"stream_options.include_usage=omitted->true"}, batch.profileEvidence.ProviderRequestNormalizations)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"MiniMax-M2.7","messages":[{"role":"user","content":"hello"}],"stream":true,"stream_options":{"include_usage":true},"max_tokens":128}`, string(actual))
}

func TestMiniMaxM27CacheReadCandidateRequiresTerminalUsageRequest(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m2.7.token-plan", "1.4.0")
	tests := []struct {
		name, body, normalization, err string
	}{
		{name: "omitted options", body: `{"model":"MiniMax-M2.7","messages":[],"stream":true}`, normalization: "stream_options.include_usage=omitted->true"},
		{name: "explicit true", body: `{"model":"MiniMax-M2.7","messages":[],"stream":true,"stream_options":{"include_usage":true}}`},
		{name: "explicit false", body: `{"model":"MiniMax-M2.7","messages":[],"stream":true,"stream_options":{"include_usage":false}}`, err: "include_usage=false is not supported"},
		{name: "unknown option", body: `{"model":"MiniMax-M2.7","messages":[],"stream":true,"stream_options":{"chunk_include_usage":true}}`, err: "has no bounded forwarding contract"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.err != "" {
				require.ErrorContains(t, err, test.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.normalization, strings.Join(evidence.ProviderRequestNormalizations, ","))
		})
	}
}

func TestMiniMaxM27NonStreamCacheReadUsageUsesExactOfficialWire(t *testing.T) {
	adapter := adapters.NewOpenAICompatible()
	for _, cached := range []int{80, 0} {
		body := []byte(`{"id":"minimax-cache","model":"MiniMax-M2.7","choices":[{"index":0,"message":{"role":"assistant","content":"ok","name":"MiniMax AI","audio_content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":` + strconv.Itoa(cached) + `}},"input_sensitive":false,"output_sensitive":false,"base_resp":{"status_code":0,"status_msg":"success"}}`)
		response, err := adapter.ParseResponse(context.Background(), adapters.ResponseInput{
			Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: body,
			Channel:        models.Channel{Provider: "minimax", AdapterType: adapters.TypeOpenAICompatible},
			RequestedModel: "public-minimax", UpstreamModel: "MiniMax-M2.7",
			CacheUsageProfile: miniMaxM27CacheProfile, ProtocolProfile: miniMaxM27CacheProtocol,
		})
		require.NoError(t, err)
		require.Equal(t, 100-cached, response.Usage.UncachedPromptTokens)
		require.Equal(t, cached, response.Usage.CacheReadTokens)
		require.Equal(t, adapters.CacheUsageSchemaOpenAIV1, response.Usage.CacheUsageSchema)
		require.Equal(t, adapters.CacheUsageStatusReadReported, response.Usage.CacheUsageStatus)
		require.Equal(t, metering.UsageAuthorityProviderReported, response.Usage.Authority)
		require.Equal(t, metering.UsageFinalityFinal, response.Usage.Finality)
	}
}

func TestMiniMaxM27CacheReadUsageRejectsMissingOrConflictingAuthority(t *testing.T) {
	adapter := adapters.NewOpenAICompatible()
	for _, body := range []string{
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":null}}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"cached_tokens":80}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":101}}}`,
	} {
		_, err := adapter.ParseResponse(context.Background(), adapters.ResponseInput{
			Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: []byte(body),
			Channel:           models.Channel{Provider: "minimax", AdapterType: adapters.TypeOpenAICompatible},
			CacheUsageProfile: miniMaxM27CacheProfile, ProtocolProfile: miniMaxM27CacheProtocol,
		})
		require.Error(t, err, body)
	}
}

func TestMiniMaxM27CacheReadStreamFreezesNormalizedFramesAndCleanEOF(t *testing.T) {
	adapter := adapters.NewOpenAICompatible()
	state := exactStreamStateForProtocol(miniMaxM27CacheProtocol, "")
	require.NotNil(t, state)
	rawFrames := []string{
		`{"id":"chunk-1","object":"chat.completion.chunk","model":"MiniMax-M2.7","choices":[{"index":0,"delta":{"role":"assistant","content":"<think>work</think>","name":"MiniMax AI","audio_content":""},"finish_reason":null}],"usage":null,"input_sensitive":false,"output_sensitive":false,"base_resp":{"status_code":0,"status_msg":"success"}}`,
		`{"id":"chunk-1","object":"chat.completion.chunk","model":"MiniMax-M2.7","choices":[{"index":0,"delta":{"role":"assistant","content":"answer","name":"MiniMax AI","audio_content":""},"finish_reason":"stop"}],"usage":null,"input_sensitive":false,"output_sensitive":false}`,
		`{"id":"chunk-1","object":"chat.completion.chunk","model":"MiniMax-M2.7","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80}}}`,
	}
	for _, raw := range rawFrames {
		normalized, err := adapter.NormalizeStreamFrame(context.Background(), adapters.StreamChunkInput{
			Data: []byte(raw), Channel: models.Channel{Provider: "minimax"}, CacheUsageProfile: miniMaxM27CacheProfile,
		})
		require.NoError(t, err)
		frame, err := parseGuardedStreamFrameForProtocol(normalized, miniMaxM27CacheProtocol)
		require.NoError(t, err)
		require.False(t, frame.unsupported, string(normalized))
		usage, present, err := adapters.ParseOpenAICompatibleUsageWithCacheProfile(
			normalized, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, miniMaxM27CacheProfile,
		)
		require.NoError(t, err)
		require.NoError(t, state.accept(frame, present && usage.HasAuthoritativeCacheUsage()))
	}
	require.NoError(t, state.validateCleanEOF(true), "clean EOF must retain terminal cache authority")

	combinedState := exactStreamStateForProtocol(miniMaxM27CacheProtocol, "")
	combinedRaw := []byte(`{"id":"chunk-2","object":"chat.completion.chunk","model":"MiniMax-M2.7","choices":[{"index":0,"delta":{"role":"assistant","content":"answer","name":"MiniMax AI","audio_content":null},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":0}},"input_sensitive":false,"output_sensitive":false,"base_resp":{"status_code":0,"status_msg":"success"}}`)
	combined, err := adapter.NormalizeStreamFrame(context.Background(), adapters.StreamChunkInput{
		Data: combinedRaw, Channel: models.Channel{Provider: "minimax"}, CacheUsageProfile: miniMaxM27CacheProfile,
	})
	require.NoError(t, err)
	combinedFrame, err := parseGuardedStreamFrameForProtocol(combined, miniMaxM27CacheProtocol)
	require.NoError(t, err)
	require.False(t, combinedFrame.unsupported, string(combined))
	require.True(t, combinedFrame.terminalUsage)
	combinedUsage, present, err := adapters.ParseOpenAICompatibleUsageWithCacheProfile(
		combined, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, miniMaxM27CacheProfile,
	)
	require.NoError(t, err)
	require.True(t, present)
	require.True(t, combinedUsage.HasAuthoritativeCacheUsage())
	require.NoError(t, combinedState.accept(combinedFrame, true))
	require.NoError(t, combinedState.validateCleanEOF(true))

	missing := exactStreamStateForProtocol(miniMaxM27CacheProtocol, "")
	finish, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}],"usage":null}`),
		miniMaxM27CacheProtocol,
	)
	require.NoError(t, err)
	require.NoError(t, missing.accept(finish, false))
	require.ErrorContains(t, missing.validateCleanEOF(true), "terminal usage")

	for _, raw := range []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"work"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"content_filter"}]}`,
		`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"prompt_tokens_details":{"cached_tokens":0}}}`,
	} {
		frame, err := parseGuardedStreamFrameForProtocol([]byte(raw), miniMaxM27CacheProtocol)
		require.NoError(t, err)
		require.True(t, frame.unsupported, raw)
	}
}
