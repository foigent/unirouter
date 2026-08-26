package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestDeepSeekV4ProToolHistoryReusesReviewedReplayContract(t *testing.T) {
	plan := executionProfilePlan(t, "deepseek.v4-pro.api", "1.4.0")
	tool := `{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}`
	missing := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"assistant","content":"hi"},{"role":"user","content":"weather"}],"tools":[` + tool + `]}`)
	_, err := validateChatExecutionProfileRequest(missing, plan)
	require.ErrorContains(t, err, "assistant history message 0 must preserve reasoning_content when function tools are requested")

	preserved := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"assistant","content":"hi","reasoning_content":"greeting"},{"role":"user","content":"weather"}],"tools":[` + tool + `]}`)
	evidence, err := validateChatExecutionProfileRequest(preserved, plan)
	require.NoError(t, err)
	require.Equal(t, "thinking=enabled;reasoning_effort=high", evidence.ReasoningProviderDefault)
	require.NoError(t, func() error { _, err := estimateChatAdmissionForPlan(preserved, plan); return err }())

	disabled := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"assistant","content":"hi"},{"role":"user","content":"weather"}],"tools":[` + tool + `],"thinking":{"type":"disabled"}}`)
	_, err = validateChatExecutionProfileRequest(disabled, plan)
	require.NoError(t, err)
}

func TestDeepSeekV4ProToolHistoryPreservesProviderPayloadAndPlanSnapshot(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", encKey, nil)
	setExecutionTestProfile(t, &plan, "deepseek.v4-pro.api", "1.4.0")
	plan.Routes[0].Provider = "deepseek"
	plan.Routes[0].UpstreamModel = "deepseek-v4-pro"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"assistant","content":"hi","reasoning_content":"greeting"},{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-deepseek-pro-history",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	payloadBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payloadBody, &payload))
	require.Contains(t, string(payload["messages"]), `"reasoning_content":"greeting"`)
}
