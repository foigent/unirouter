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

func TestKimiK27CodeExactToolsCandidateRetainsRequiredThinkingDifferential(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.7-code.api", "1.3.0")
	tool := `{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}`
	tests := []struct {
		name, body, wantRequest, wantEffective, wantOrigin, wantReasoning, wantErr string
	}{
		{
			name:        "omitted thinking and choice use provider defaults",
			body:        `{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
			wantRequest: "omitted", wantEffective: "auto", wantOrigin: modelrouting.CapabilityOriginProviderDefault, wantReasoning: "omitted",
		},
		{
			name:        "explicit enabled keep all and auto",
			body:        `{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"weather"}],"thinking":{"type":"enabled","keep":"all"},"tools":[` + tool + `],"tool_choice":"auto"}`,
			wantRequest: "auto", wantEffective: "auto", wantOrigin: modelrouting.CapabilityOriginExplicitRequest, wantReasoning: "enabled",
		},
		{name: "thinking cannot be disabled", body: `{"model":"kimi-k2.7-code","messages":[],"thinking":{"type":"disabled"}}`, wantErr: "thinking must be one of enabled"},
		{name: "required remains unsupported", body: `{"model":"kimi-k2.7-code","messages":[],"tools":[` + tool + `],"tool_choice":"required"}`, wantErr: "tool_choice must be one of none, auto"},
		{name: "named remains unsupported", body: `{"model":"kimi-k2.7-code","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`, wantErr: "named tool_choice is not supported"},
		{name: "stream tools remain fail closed", body: `{"model":"kimi-k2.7-code","messages":[],"stream":true,"tools":[` + tool + `]}`, wantErr: "function tools require stream=false"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantRequest, evidence.FunctionToolChoiceRequest)
			require.Equal(t, test.wantEffective, evidence.FunctionToolChoiceEffective)
			require.Equal(t, test.wantOrigin, evidence.FunctionToolChoiceOrigin)
			require.Equal(t, test.wantReasoning, evidence.ReasoningRequest)
			require.Equal(t, "all", evidence.ReasoningThinkingKeepDefault)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestKimiK27CodeExactToolsCandidateRequiresReasoningReplay(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.7-code.api", "1.3.0")
	valid := []byte(`{"model":"kimi-k2.7-code","messages":[{"role":"assistant","content":null,"reasoning_content":"need weather","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}]}`)
	_, err := validateChatExecutionProfileRequest(valid, plan)
	require.NoError(t, err)

	invalid := []byte(`{"model":"kimi-k2.7-code","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}]}`)
	_, err = validateChatExecutionProfileRequest(invalid, plan)
	require.ErrorContains(t, err, "must preserve reasoning_content")
}

func TestKimiK27CodeExactToolsCandidateUsesOnePlanAndExactResponse(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.moonshot.cn", encKey, nil)
	setExecutionTestProfile(t, &plan, "kimi.k2.7-code.api", "1.3.0")
	plan.Routes[0].Provider = "moonshot"
	plan.Routes[0].UpstreamModel = "kimi-k2.7-code"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}],"max_completion_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-kimi-k27-tools",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Contains(t, batch.plan.CapabilityRequirements, modelrouting.CapabilityRequirement{
		CapabilityID:       modelprofile.CapabilityToolChoiceAuto,
		Origins:            []string{modelrouting.CapabilityOriginProviderDefault},
		FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, ""),
	})
	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actual, &payload))
	require.Contains(t, string(payload["tools"]), `"name":"lookup"`)

	route := &RouteResult{
		Channel:    models.Channel{ID: 1, Name: "kimi", Provider: "moonshot"},
		ChannelKey: models.ChannelKey{ID: 2}, MappedModel: "kimi-k2.7-code",
		ModelProtocolProfile: "protocol.kimi-openai-chat.v5",
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID:       modelprofile.CapabilityToolChoiceAuto,
			Origins:            []string{modelrouting.CapabilityOriginProviderDefault},
			FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, ""),
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"lookup"}), route).complete())
	require.False(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"other"}), route).complete())
}
