package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestMiniMaxM25HighspeedCacheReadCandidateUsesReviewedM2ContractAndOnePlanSnapshot(t *testing.T) {
	historical := executionProfilePlan(t, "minimax.m2.5-highspeed.token-plan", "1.3.0")
	historicalEvidence, err := validateChatExecutionProfileRequest(
		[]byte(`{"model":"MiniMax-M2.5-highspeed","messages":[]}`), historical,
	)
	require.NoError(t, err)
	require.Equal(t, "cache-usage.minimax-m2.v1", historicalEvidence.CacheUsageProfile)

	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m2.5-highspeed.token-plan", "1.4.0")
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].UpstreamModel = "MiniMax-M2.5-highspeed"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m25-cache",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityCacheReadUsage, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityPromptCache, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Equal(t, miniMaxM27CacheProfile, batch.routes[0].compat.CacheUsageProfile)
	require.Equal(t, miniMaxM27CacheProtocol, batch.routes[0].compat.ModelProtocolProfile)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"MiniMax-M2.5-highspeed","messages":[{"role":"user","content":"hello"}],"max_tokens":128}`, string(actual))
}
