package gateway

import (
	"context"
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

const doubaoSeed21ProCacheUsageProfile = "cache-usage.doubao-ark-chat.v1"

func TestDoubaoSeed21ProImplicitCacheReadUsesOnePlanSnapshot(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-pro.api", "1.5.0")
	body := []byte(`{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"hello"}]}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.Equal(t, doubaoSeed21ProCacheUsageProfile, evidence.CacheUsageProfile)
	require.Equal(t, doubaoSeed21ProStrictJSONSchemaProtocol, evidence.ModelProtocolProfile)

	historical := executionProfilePlan(t, "doubao.seed-2.1-pro.api", "1.4.0")
	historicalEvidence, err := validateChatExecutionProfileRequest(body, historical)
	require.NoError(t, err)
	require.Empty(t, historicalEvidence.CacheUsageProfile)

	encKey := []byte("0123456789abcdef0123456789abcdef")
	executionPlan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &executionPlan, "doubao.seed-2.1-pro.api", "1.5.0")
	executionPlan.Routes[0].Provider = "doubao"
	executionPlan.Routes[0].UpstreamModel = "doubao-seed-2-1-pro-260628"
	planner := &executionPlannerStub{plan: executionPlan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-seed21-pro-cache",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityCacheReadUsage, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityPromptCache, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Equal(t, doubaoSeed21ProCacheUsageProfile, batch.routes[0].compat.CacheUsageProfile)
}

func TestDoubaoSeed21ProNonStreamCacheReadUsageUsesExactOfficialWire(t *testing.T) {
	adapter := adapters.NewOpenAICompatible()
	tests := []struct {
		name      string
		cacheRead int
	}{
		{name: "cache hit", cacheRead: 80},
		{name: "authoritative zero"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			counter := "0"
			if test.cacheRead == 80 {
				counter = "80"
			}
			body := []byte(`{"id":"doubao-pro-cache","model":"doubao-seed-2-1-pro-260628","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":` + counter + `}}}`)
			response, err := adapter.ParseResponse(context.Background(), adapters.ResponseInput{
				Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: body,
				Channel:        models.Channel{Provider: "doubao", AdapterType: adapters.TypeOpenAICompatible},
				RequestedModel: "public-doubao-pro", UpstreamModel: "doubao-seed-2-1-pro-260628",
				CacheUsageProfile: doubaoSeed21ProCacheUsageProfile,
				ProtocolProfile:   doubaoSeed21ProStrictJSONSchemaProtocol,
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

func TestDoubaoSeed21ProCacheReadCounterIsMandatory(t *testing.T) {
	adapter := adapters.NewOpenAICompatible()
	body := []byte(`{"id":"doubao-pro-cache","model":"doubao-seed-2-1-pro-260628","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107}}`)
	_, err := adapter.ParseResponse(context.Background(), adapters.ResponseInput{
		Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: body,
		Channel:        models.Channel{Provider: "doubao", AdapterType: adapters.TypeOpenAICompatible},
		RequestedModel: "public-doubao-pro", UpstreamModel: "doubao-seed-2-1-pro-260628",
		CacheUsageProfile: doubaoSeed21ProCacheUsageProfile,
		ProtocolProfile:   doubaoSeed21ProStrictJSONSchemaProtocol,
	})
	require.ErrorContains(t, err, "including an authoritative zero")
}
