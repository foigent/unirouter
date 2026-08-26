package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestDeepSeekV4ProCandidateUsesCurrentReasoningDifferential(t *testing.T) {
	plan := executionProfilePlan(t, "deepseek.v4-pro.api", "1.3.0")
	tool := `{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}`
	tests := []struct {
		name          string
		body          string
		wantEffective string
		wantErr       string
	}{
		{name: "low remains low", body: `{"model":"deepseek-v4-pro","messages":[],"tools":[` + tool + `],"reasoning_effort":"low"}`, wantEffective: "low"},
		{name: "medium maps to high", body: `{"model":"deepseek-v4-pro","messages":[],"tools":[` + tool + `],"reasoning_effort":"medium"}`, wantEffective: "high"},
		{name: "xhigh maps to high", body: `{"model":"deepseek-v4-pro","messages":[],"tools":[` + tool + `],"reasoning_effort":"xhigh"}`, wantEffective: "high"},
		{name: "max remains max", body: `{"model":"deepseek-v4-pro","messages":[],"tools":[` + tool + `],"reasoning_effort":"max"}`, wantEffective: "max"},
		{name: "required is accepted while thinking", body: `{"model":"deepseek-v4-pro","messages":[],"tools":[` + tool + `],"tool_choice":"required"}`, wantEffective: "omitted"},
		{name: "parallel null remains fail closed", body: `{"model":"deepseek-v4-pro","messages":[],"tools":[` + tool + `],"parallel_tool_calls":null}`, wantErr: "parallel_tool_calls is not supported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantEffective, evidence.ReasoningEffortEffective)
			require.True(t, evidence.FunctionToolInvocationModeled)
			require.Equal(t, []string{"lookup"}, evidence.DeclaredFunctionNames)
		})
	}
}

func TestDeepSeekV4ProRequiredChoiceReusesOnePlanAndExactResponseContract(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", encKey, nil)
	setExecutionTestProfile(t, &plan, "deepseek.v4-pro.api", "1.3.0")
	plan.Routes[0].Provider = "deepseek"
	plan.Routes[0].UpstreamModel = "deepseek-v4-pro"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"required","max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-deepseek-pro-required",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	want := modelrouting.CapabilityRequirement{
		CapabilityID:       modelprofile.CapabilityToolChoiceReq,
		Origins:            []string{modelrouting.CapabilityOriginExplicitRequest},
		FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, ""),
	}
	require.Contains(t, batch.plan.CapabilityRequirements, want)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	payloadBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payloadBody, &payload))
	require.JSONEq(t, `"required"`, string(payload["tool_choice"]))
	require.Equal(t, `"deepseek-v4-pro"`, string(payload["model"]))

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "deepseek", Provider: "deepseek"}, ChannelKey: models.ChannelKey{ID: 2},
		MappedModel: "deepseek-v4-pro", ModelProtocolProfile: deepSeekV4FlashToolsProtocol,
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{want},
	}
	require.True(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"lookup"}), route).complete())
	require.False(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse(nil), route).complete())
	require.False(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"other"}), route).complete())
}
