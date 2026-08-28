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

const kimiK26ToolsProtocol = "protocol.kimi-openai-chat.v5"

func TestKimiK26ExactToolsCandidateUsesModelSpecificChoices(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.6.api", "1.7.0")
	tool := `{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}`
	tests := []struct {
		name, body, wantRequest, wantEffective, wantOrigin, wantErr string
	}{
		{
			name:        "omitted choice uses provider auto",
			body:        `{"model":"kimi-k2.6","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
			wantRequest: "omitted", wantEffective: "auto", wantOrigin: modelrouting.CapabilityOriginProviderDefault,
		},
		{
			name:        "explicit auto",
			body:        `{"model":"kimi-k2.6","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"tool_choice":"auto"}`,
			wantRequest: "auto", wantEffective: "auto", wantOrigin: modelrouting.CapabilityOriginExplicitRequest,
		},
		{
			name:        "none without tools",
			body:        `{"model":"kimi-k2.6","messages":[{"role":"user","content":"hello"}],"tool_choice":"none"}`,
			wantRequest: "none", wantEffective: "none", wantOrigin: modelrouting.CapabilityOriginExplicitRequest,
		},
		{name: "required is not a K2.6 choice", body: `{"model":"kimi-k2.6","messages":[],"tools":[` + tool + `],"tool_choice":"required"}`, wantErr: "tool_choice must be one of none, auto"},
		{name: "named is not a K2.6 choice", body: `{"model":"kimi-k2.6","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`, wantErr: "named tool_choice is not supported"},
		{name: "parallel is not modelled", body: `{"model":"kimi-k2.6","messages":[],"tools":[` + tool + `],"parallel_tool_calls":true}`, wantErr: "parallel_tool_calls is not supported"},
		{name: "null parallel cannot bypass", body: `{"model":"kimi-k2.6","messages":[],"tools":[` + tool + `],"parallel_tool_calls":null}`, wantErr: "parallel_tool_calls is not supported"},
		{name: "streaming tools require a separate exact delta contract", body: `{"model":"kimi-k2.6","messages":[],"stream":true,"tools":[` + tool + `]}`, wantErr: "function tools require stream=false"},
		{name: "plain streaming remains supported", body: `{"model":"kimi-k2.6","messages":[{"role":"user","content":"hello"}],"stream":true}`, wantRequest: "omitted", wantEffective: "none", wantOrigin: modelrouting.CapabilityOriginProviderDefault},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.True(t, evidence.FunctionToolInvocationModeled)
			require.Equal(t, test.wantRequest, evidence.FunctionToolChoiceRequest)
			require.Equal(t, test.wantEffective, evidence.FunctionToolChoiceEffective)
			require.Equal(t, test.wantOrigin, evidence.FunctionToolChoiceOrigin)
			if evidence.FunctionToolsRequested {
				require.Equal(t, []string{"lookup"}, evidence.DeclaredFunctionNames)
			}
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestKimiK26ExactToolsCandidateRequiresCompleteToolHistory(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.6.api", "1.7.0")
	tests := []struct {
		name, messages, wantErr string
	}{
		{
			name:     "complete thinking tool turn",
			messages: `[{"role":"user","content":"weather"},{"role":"assistant","content":null,"reasoning_content":"need weather","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"},{"role":"user","content":"continue"}]`,
		},
		{
			name:     "tool result missing",
			messages: `[{"role":"assistant","content":null,"reasoning_content":"need weather","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}]`,
			wantErr:  "assistant tool_calls must be followed by matching tool messages",
		},
		{
			name:     "thinking tool call missing reasoning replay",
			messages: `[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}]`,
			wantErr:  "must preserve reasoning_content",
		},
		{
			name:     "tool result references unknown call",
			messages: `[{"role":"tool","tool_call_id":"call_other","content":"sunny"}]`,
			wantErr:  "does not reference prior assistant tool_calls",
		},
		{
			name:     "non-stream history rejects streaming delta index",
			messages: `[{"role":"assistant","content":null,"reasoning_content":"need weather","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}]`,
			wantErr:  `assistant tool_call field "index" has no bounded forwarding contract`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"model":"kimi-k2.6","messages":` + test.messages + `}`)
			_, err := validateChatExecutionProfileRequest(body, plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			_, err = estimateChatAdmissionForPlan(body, plan)
			require.NoError(t, err)
		})
	}
}

func TestKimiK26ExactToolsCandidateBindsOnePlanSnapshotAndProviderPayload(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.moonshot.cn", encKey, nil)
	setExecutionTestProfile(t, &plan, "kimi.k2.6.api", "1.7.0")
	plan.Routes[0].Provider = "moonshot"
	plan.Routes[0].UpstreamModel = "kimi-k2.6"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}],"max_completion_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-kimi-k26-tools",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Contains(t, batch.plan.CapabilityRequirements, modelrouting.CapabilityRequirement{
		CapabilityID:       modelprofile.CapabilityToolChoiceAuto,
		Origins:            []string{modelrouting.CapabilityOriginProviderDefault},
		FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, ""),
	})
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	planner.mu.Lock()
	require.Equal(t, batch.plan.CapabilityRequirements, planner.requests[0].InvocationCapabilities)
	planner.mu.Unlock()

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actual, &payload))
	require.Contains(t, string(payload["tools"]), `"name":"lookup"`)
	_, choiceInjected := payload["tool_choice"]
	require.False(t, choiceInjected, "provider default is evidence, not a wire rewrite")
}

func TestKimiK26ExactToolsCandidateRejectsUndeclaredResponseFunction(t *testing.T) {
	route := &RouteResult{
		Channel:     models.Channel{ID: 1, Name: "kimi", Provider: "moonshot"},
		ChannelKey:  models.ChannelKey{ID: 2},
		MappedModel: "kimi-k2.6", ModelProtocolProfile: kimiK26ToolsProtocol,
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID:       modelprofile.CapabilityToolChoiceAuto,
			Origins:            []string{modelrouting.CapabilityOriginProviderDefault},
			FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, ""),
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"lookup"}), route).complete())
	require.False(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"other"}), route).complete())
}
