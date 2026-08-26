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
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const doubaoSeed21TurboCacheUsageProfile = "cache-usage.doubao-ark-chat.v1"

func TestDoubaoSeed21TurboImplicitCacheReadCandidateUsesOnePlanSnapshot(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.5.0")
	body := []byte(`{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"user","content":"hello"}]}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.Equal(t, doubaoSeed21TurboCacheUsageProfile, evidence.CacheUsageProfile)
	require.Equal(t, doubaoSeed21TurboStrictJSONSchemaProtocol, evidence.ModelProtocolProfile)

	historical := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.4.0")
	historicalEvidence, err := validateChatExecutionProfileRequest(body, historical)
	require.NoError(t, err)
	require.Empty(t, historicalEvidence.CacheUsageProfile)

	encKey := []byte("0123456789abcdef0123456789abcdef")
	executionPlan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &executionPlan, "doubao.seed-2.1-turbo.api", "1.5.0")
	executionPlan.Routes[0].Provider = "doubao"
	executionPlan.Routes[0].UpstreamModel = "doubao-seed-2-1-turbo-260628"
	planner := &executionPlannerStub{plan: executionPlan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-seed21-turbo-cache",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityCacheReadUsage, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityPromptCache, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Equal(t, doubaoSeed21TurboCacheUsageProfile, batch.routes[0].compat.CacheUsageProfile)
}

func TestDoubaoSeed21TurboNonStreamCacheReadUsageUsesExactOfficialWire(t *testing.T) {
	adapter := adapters.NewOpenAICompatible()
	for _, test := range []struct {
		name      string
		cacheRead int
	}{
		{name: "cache hit", cacheRead: 80},
		{name: "authoritative zero"},
	} {
		t.Run(test.name, func(t *testing.T) {
			counter := "0"
			if test.cacheRead == 80 {
				counter = "80"
			}
			body := []byte(`{"id":"doubao-turbo-cache","model":"doubao-seed-2-1-turbo-260628","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":` + counter + `}}}`)
			response, err := adapter.ParseResponse(context.Background(), adapters.ResponseInput{
				Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: body,
				Channel:           models.Channel{Provider: "doubao", AdapterType: adapters.TypeOpenAICompatible},
				RequestedModel:    "public-doubao-turbo",
				UpstreamModel:     "doubao-seed-2-1-turbo-260628",
				CacheUsageProfile: doubaoSeed21TurboCacheUsageProfile,
				ProtocolProfile:   doubaoSeed21TurboStrictJSONSchemaProtocol,
			})
			require.NoError(t, err)
			require.Equal(t, 100, response.Usage.PromptTokens)
			require.Equal(t, 100-test.cacheRead, response.Usage.UncachedPromptTokens)
			require.Equal(t, test.cacheRead, response.Usage.CacheReadTokens)
			require.Equal(t, adapters.CacheUsageSchemaOpenAIV1, response.Usage.CacheUsageSchema)
			require.Equal(t, adapters.CacheUsageStatusReadReported, response.Usage.CacheUsageStatus)
			require.Equal(t, metering.UsageAuthorityProviderReported, response.Usage.Authority)
			require.Equal(t, metering.UsageFinalityFinal, response.Usage.Finality)
		})
	}
}

func TestDoubaoSeed21TurboCacheReadCounterIsMandatory(t *testing.T) {
	adapter := adapters.NewOpenAICompatible()
	body := []byte(`{"id":"doubao-turbo-cache","model":"doubao-seed-2-1-turbo-260628","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107}}`)
	_, err := adapter.ParseResponse(context.Background(), adapters.ResponseInput{
		Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: body,
		Channel:           models.Channel{Provider: "doubao", AdapterType: adapters.TypeOpenAICompatible},
		RequestedModel:    "public-doubao-turbo",
		UpstreamModel:     "doubao-seed-2-1-turbo-260628",
		CacheUsageProfile: doubaoSeed21TurboCacheUsageProfile,
		ProtocolProfile:   doubaoSeed21TurboStrictJSONSchemaProtocol,
	})
	require.ErrorContains(t, err, "including an authoritative zero")
}

func TestDoubaoSeed21TurboFinalStreamUsageRequiresCacheReadCounter(t *testing.T) {
	var settled billing.SettleRequest
	relay := newTestRelay(p6f11RelayDB(), &mockBilling{settleFn: func(_ context.Context, request billing.SettleRequest) (*billing.SettleResult, error) {
		settled = request
		return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
	}})
	route := &RouteResult{
		Channel:              models.Channel{ID: 1, Name: "Doubao Seed 2.1 Turbo", Provider: "doubao"},
		CacheUsageProfile:    doubaoSeed21TurboCacheUsageProfile,
		ModelProtocolProfile: doubaoSeed21TurboStrictJSONSchemaProtocol,
	}
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":13,\"completion_tokens\":4,\"total_tokens\":17,\"prompt_tokens_details\":{\"cached_tokens\":8}}}\n\n" +
		"data: [DONE]\n\n"
	recorder := httptest.NewRecorder()
	relay.relayStreamRequest(
		p6f11StreamContext(nil, "req-doubao-turbo-cache-stream"), recorder,
		&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}, route,
		"req-doubao-turbo-cache-stream", 1, 2, "doubao-seed-2-1-turbo-260628", "doubao-seed-2-1-turbo-260628", "127.0.0.1", nil,
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
		p6f11StreamContext(nil, "req-doubao-turbo-cache-stream-missing"), missingRecorder,
		&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(missing))}, route,
		"req-doubao-turbo-cache-stream-missing", 1, 2, "doubao-seed-2-1-turbo-260628", "doubao-seed-2-1-turbo-260628", "127.0.0.1", nil,
		logger.New(logger.Fields{}), time.Now(), 1_000, nil, "",
	)
	require.Contains(t, missingRecorder.Body.String(), "stream_error.invalid_frame")
	require.NotContains(t, missingRecorder.Body.String(), "data: [DONE]")
}
