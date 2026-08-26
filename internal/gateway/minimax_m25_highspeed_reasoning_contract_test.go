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

func TestMiniMaxM25HighspeedRequiredReasoningCandidateUsesReviewedM2Contract(t *testing.T) {
	profilePlan := executionProfilePlan(t, "minimax.m2.5-highspeed.token-plan", "1.3.0")
	body := []byte(`{"model":"MiniMax-M2.5-highspeed","messages":[{"role":"user","content":"hello"}],"reasoning_split":false,"max_tokens":128}`)
	evidence, err := validateChatExecutionProfileRequest(body, profilePlan)
	require.NoError(t, err)
	require.Equal(t, modelprofile.ReasoningModeRequired, evidence.ReasoningMode)
	require.Equal(t, "required", evidence.ReasoningRequest)
	require.Equal(t, "false", evidence.ReasoningSplitRequest)

	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m2.5-highspeed.token-plan", "1.3.0")
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].UpstreamModel = "MiniMax-M2.5-highspeed"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m25-highspeed-reasoning",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{{
		CapabilityID: modelprofile.CapabilityReasoning,
		Origins:      []string{modelrouting.CapabilityOriginProfileAlwaysRequired},
	}}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	historical := executionProfilePlan(t, "minimax.m2.5-highspeed.token-plan", "1.2.0")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"MiniMax-M2.5-highspeed","messages":[],"reasoning_split":false}`), historical)
	require.ErrorContains(t, err, "reasoning control is not supported")
}
