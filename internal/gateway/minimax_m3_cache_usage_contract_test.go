package gateway

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const (
	miniMaxM3CacheProfile  = "cache-usage.minimax-openai.v1"
	miniMaxM3CacheProtocol = "protocol.minimax-m3-openai-chat.v8"
)

func TestMiniMaxM3CacheReadCandidateUsesOnePlanSnapshotWithVision(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m3.token-plan", "1.6.0")
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].UpstreamModel = "MiniMax-M3"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))

	dataURL := testJPEGDataURL(t, 32, 32)
	body := testMiniMaxM3ImageBody(t, "user", []string{dataURL}, nil)
	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-cache-image",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityCacheReadUsage, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityPromptCache, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityVision, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Equal(t, miniMaxM3CacheProfile, batch.routes[0].compat.CacheUsageProfile)
	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.Equal(t, body, actual)
}

func TestMiniMaxM3CacheReadAndPriorityTierComposeOnlyInCostObservation(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m3.token-plan", "1.6.0")
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].UpstreamModel = "MiniMax-M3"
	inspector := &processingTierReadinessStub{result: billing.AdmissionReadiness{BillingMode: billing.BillingModeCostObservation}}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	relay.SetBillingAdmissionReadinessInspector(inspector)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"service_tier":"priority","max_completion_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-cache-priority",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, 1, inspector.calls)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityCacheReadUsage, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityPromptCache, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityServiceTier, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.Contains(t, string(actual), `"service_tier":"priority"`)
}

func TestMiniMaxM3CacheReadUsageUsesExactNonStreamAndStreamWire(t *testing.T) {
	adapter := adapters.NewOpenAICompatible()
	for _, cached := range []int{80, 0} {
		body := []byte(`{"id":"minimax-cache","model":"MiniMax-M3","choices":[{"index":0,"message":{"role":"assistant","content":"ok","name":"MiniMax AI","audio_content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":` + strconv.Itoa(cached) + `}},"input_sensitive":false,"output_sensitive":false,"base_resp":{"status_code":0,"status_msg":"success"}}`)
		response, err := adapter.ParseResponse(context.Background(), adapters.ResponseInput{
			Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: body,
			Channel:        models.Channel{Provider: "minimax", AdapterType: adapters.TypeOpenAICompatible},
			RequestedModel: "public-minimax", UpstreamModel: "MiniMax-M3",
			CacheUsageProfile: miniMaxM3CacheProfile, ProtocolProfile: miniMaxM3CacheProtocol,
		})
		require.NoError(t, err)
		require.Equal(t, 100-cached, response.Usage.UncachedPromptTokens)
		require.Equal(t, cached, response.Usage.CacheReadTokens)
		require.Equal(t, adapters.CacheUsageSchemaOpenAIV1, response.Usage.CacheUsageSchema)
		require.Equal(t, metering.UsageAuthorityProviderReported, response.Usage.Authority)
	}

	for _, invalid := range []string{
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":null}}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"cached_tokens":80}}`,
	} {
		_, err := adapter.ParseResponse(context.Background(), adapters.ResponseInput{
			Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: []byte(invalid),
			Channel:           models.Channel{Provider: "minimax", AdapterType: adapters.TypeOpenAICompatible},
			CacheUsageProfile: miniMaxM3CacheProfile, ProtocolProfile: miniMaxM3CacheProtocol,
		})
		require.Error(t, err, invalid)
	}

	plan := executionProfilePlan(t, "minimax.m3.token-plan", "1.6.0")
	evidence, err := validateChatExecutionProfileRequest([]byte(
		`{"model":"MiniMax-M3","messages":[],"thinking":{"type":"disabled"},"stream":true}`,
	), plan)
	require.NoError(t, err)
	require.Equal(t, []string{"stream_options.include_usage=omitted->true"}, evidence.ProviderRequestNormalizations)

	state := exactStreamStateForProtocol(miniMaxM3CacheProtocol, "")
	require.NotNil(t, state)
	raw := []byte(`{"id":"chunk","object":"chat.completion.chunk","model":"MiniMax-M3","choices":[{"index":0,"delta":{"content":"answer","name":"MiniMax AI","audio_content":null},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":0}},"input_sensitive":false,"output_sensitive":false,"base_resp":{"status_code":0,"status_msg":"success"}}`)
	normalized, err := adapter.NormalizeStreamFrame(context.Background(), adapters.StreamChunkInput{
		Data: raw, Channel: models.Channel{Provider: "minimax"}, CacheUsageProfile: miniMaxM3CacheProfile,
	})
	require.NoError(t, err)
	frame, err := parseGuardedStreamFrameForProtocol(normalized, miniMaxM3CacheProtocol)
	require.NoError(t, err)
	require.False(t, frame.unsupported, string(normalized))
	usage, present, err := adapters.ParseOpenAICompatibleUsageWithCacheProfile(
		normalized, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, miniMaxM3CacheProfile,
	)
	require.NoError(t, err)
	require.True(t, present)
	require.NoError(t, state.accept(frame, usage.HasAuthoritativeCacheUsage()))
	require.NoError(t, state.validateCleanEOF(true))

	missing := exactStreamStateForProtocol(miniMaxM3CacheProtocol, "")
	finish, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}],"usage":null}`),
		miniMaxM3CacheProtocol,
	)
	require.NoError(t, err)
	require.NoError(t, missing.accept(finish, false))
	require.ErrorContains(t, missing.validateCleanEOF(true), "terminal usage")

	_, err = validateChatExecutionProfileRequest([]byte(
		`{"model":"MiniMax-M3","messages":[],"stream":true}`,
	), plan)
	require.ErrorContains(t, err, "streaming is not supported while reasoning is active")
	require.NotContains(t, strings.Join(evidence.ProviderRequestNormalizations, ","), "service_tier")
}
