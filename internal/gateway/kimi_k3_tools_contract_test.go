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

func TestKimiK3ExactTopLevelToolsCandidateModelsRequiredChoice(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k3.api", "1.7.0")
	tool := `{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}`
	tests := []struct {
		name, body, wantRequest, wantEffective, wantOrigin, wantErr string
	}{
		{name: "omitted uses provider auto", body: `{"model":"kimi-k3","messages":[],"tools":[` + tool + `]}`, wantRequest: "omitted", wantEffective: "auto", wantOrigin: modelrouting.CapabilityOriginProviderDefault},
		{name: "required", body: `{"model":"kimi-k3","messages":[],"tools":[` + tool + `],"tool_choice":"required"}`, wantRequest: "required", wantEffective: "required", wantOrigin: modelrouting.CapabilityOriginExplicitRequest},
		{name: "none without tools", body: `{"model":"kimi-k3","messages":[],"tool_choice":"none"}`, wantRequest: "none", wantEffective: "none", wantOrigin: modelrouting.CapabilityOriginExplicitRequest},
		{name: "named remains unmodelled", body: `{"model":"kimi-k3","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`, wantErr: "named tool_choice is not supported"},
		{name: "required needs top-level tools", body: `{"model":"kimi-k3","messages":[],"tool_choice":"required"}`, wantErr: "requires at least one function tool"},
		{name: "tools stream requires exact delta milestone", body: `{"model":"kimi-k3","messages":[],"stream":true,"tools":[` + tool + `]}`, wantErr: "function tools require stream=false"},
		{name: "dynamic tools are a separate candidate", body: `{"model":"kimi-k3","messages":[{"role":"system","tools":[` + tool + `]}]}`, wantErr: "dynamic system tools are not supported"},
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
			require.Equal(t, "omitted", evidence.ReasoningEffortEffective)
			require.Equal(t, "max", evidence.ReasoningProviderDefault)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestKimiK3ExactTopLevelToolsCandidatePreservesHistoryReasoning(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k3.api", "1.7.0")
	valid := []byte(`{"model":"kimi-k3","messages":[{"role":"assistant","content":null,"reasoning_content":"need weather","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}]}`)
	_, err := validateChatExecutionProfileRequest(valid, plan)
	require.NoError(t, err)

	invalid := []byte(`{"model":"kimi-k3","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}]}`)
	_, err = validateChatExecutionProfileRequest(invalid, plan)
	require.ErrorContains(t, err, "must preserve reasoning_content")
}

func TestKimiK3ExactTopLevelToolsCandidateUsesOnePlanAndRequiredResponse(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.moonshot.cn", encKey, nil)
	setExecutionTestProfile(t, &plan, "kimi.k3.api", "1.7.0")
	plan.Routes[0].Provider = "moonshot"
	plan.Routes[0].UpstreamModel = "kimi-k3"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}],"tool_choice":"required","max_completion_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-kimi-k3-tools",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Contains(t, batch.plan.CapabilityRequirements, modelrouting.CapabilityRequirement{
		CapabilityID:       modelprofile.CapabilityToolChoiceReq,
		Origins:            []string{modelrouting.CapabilityOriginExplicitRequest},
		FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, ""),
	})
	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actual, &payload))
	require.JSONEq(t, `"required"`, string(payload["tool_choice"]))

	route := &RouteResult{
		Channel: models.Channel{ID: 1, Name: "kimi", Provider: "moonshot"}, ChannelKey: models.ChannelKey{ID: 2},
		MappedModel: "kimi-k3", ModelProtocolProfile: "protocol.kimi-openai-chat.k3.v4",
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID:       modelprofile.CapabilityToolChoiceReq,
			Origins:            []string{modelrouting.CapabilityOriginExplicitRequest},
			FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, ""),
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"lookup"}), route).complete())
	require.False(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse(nil), route).complete())
	require.False(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"other"}), route).complete())
}

func TestKimiK3ExactDynamicToolsCandidateFinalizesOneVisibleFunctionSet(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k3.api", "1.8.0")
	tool := func(name string) string {
		return `{"type":"function","function":{"name":"` + name + `","description":"Call ` + name + `","parameters":{"type":"object"}}}`
	}
	tests := []struct {
		name        string
		body        string
		wantChoice  string
		wantOrigin  string
		wantNames   []string
		wantDynamic bool
		wantErr     string
	}{
		{
			name:       "dynamic only omitted choice uses provider auto",
			body:       `{"model":"kimi-k3","messages":[{"role":"system","tools":[` + tool("lookup") + `]},{"role":"user","content":"weather"}]}`,
			wantChoice: "auto", wantOrigin: modelrouting.CapabilityOriginProviderDefault,
			wantNames: []string{"lookup"}, wantDynamic: true,
		},
		{
			name:       "top level and multiple dynamic declarations share one union",
			body:       `{"model":"kimi-k3","tools":[` + tool("global_search") + `],"tool_choice":"required","messages":[{"role":"system","tools":[` + tool("lookup") + `]},{"role":"user","content":"weather"},{"role":"system","tools":[` + tool("calculator") + `]}]}`,
			wantChoice: "required", wantOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantNames: []string{"calculator", "global_search", "lookup"}, wantDynamic: true,
		},
		{
			name:       "explicit none remains an exact no-call constraint",
			body:       `{"model":"kimi-k3","tool_choice":"none","messages":[{"role":"system","tools":[` + tool("lookup") + `]}]}`,
			wantChoice: "none", wantOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantNames: []string{"lookup"}, wantDynamic: true,
		},
		{
			name:    "required still needs a visible function",
			body:    `{"model":"kimi-k3","tool_choice":"required","messages":[{"role":"user","content":"weather"}]}`,
			wantErr: "requires at least one function tool",
		},
		{
			name:    "dynamic tools remain non-stream until exact deltas exist",
			body:    `{"model":"kimi-k3","stream":true,"messages":[{"role":"system","tools":[` + tool("lookup") + `]}]}`,
			wantErr: "function tools require stream=false",
		},
		{
			name:    "dynamic declaration cannot carry content",
			body:    `{"model":"kimi-k3","messages":[{"role":"system","content":null,"tools":[` + tool("lookup") + `]}]}`,
			wantErr: "dynamic tools cannot be used with content",
		},
		{
			name:    "duplicate across top level and dynamic fails closed",
			body:    `{"model":"kimi-k3","tools":[` + tool("lookup") + `],"messages":[{"role":"system","tools":[` + tool("lookup") + `]}]}`,
			wantErr: "duplicate function name lookup",
		},
		{
			name:    "duplicate across dynamic declarations fails closed",
			body:    `{"model":"kimi-k3","messages":[{"role":"system","tools":[` + tool("lookup") + `]},{"role":"system","tools":[` + tool("lookup") + `]}]}`,
			wantErr: "duplicate function name lookup",
		},
		{
			name:    "dynamic names use the exact K3 function grammar",
			body:    `{"model":"kimi-k3","messages":[{"role":"system","tools":[` + tool("9invalid") + `]}]}`,
			wantErr: "function name must match",
		},
		{
			name:    "named choice stays closed while K3 thinking is always on",
			body:    `{"model":"kimi-k3","tools":[` + tool("lookup") + `],"tool_choice":{"type":"function","function":{"name":"lookup"}},"messages":[]}`,
			wantErr: "named tool_choice is not supported",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.True(t, evidence.FunctionToolsRequested)
			require.Equal(t, test.wantChoice, evidence.FunctionToolChoiceEffective)
			require.Equal(t, test.wantOrigin, evidence.FunctionToolChoiceOrigin)
			require.Equal(t, test.wantNames, evidence.DeclaredFunctionNames)
			require.Equal(t, test.wantDynamic, evidence.DynamicToolsRequested)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestKimiK3ExactDynamicToolsCandidateBindsUnionAndNoneToOnePlan(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	basePlan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.moonshot.cn", encKey, nil)
	setExecutionTestProfile(t, &basePlan, "kimi.k3.api", "1.8.0")
	basePlan.Routes[0].Provider = "moonshot"
	basePlan.Routes[0].UpstreamModel = "kimi-k3"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: basePlan}, &executionCoordinatorStub{}, routingpolicy.Default()))

	topLevel := `{"type":"function","function":{"name":"global_search","description":"Search","parameters":{"type":"object"}}}`
	dynamic := `{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}`
	explicitOutput := 128
	prepare := func(requestID, choice string) *preparedModelExecutionBatch {
		body := []byte(`{"model":"public-model","tools":[` + topLevel + `],"tool_choice":"` + choice + `","messages":[{"role":"system","tools":[` + dynamic + `]},{"role":"user","content":"weather"}],"max_completion_tokens":128}`)
		batch, err := relay.prepareModelExecutionRoutes(
			context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
			"public-model", "public-model", requestID,
			modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
			body, time.Now(),
		)
		require.NoError(t, err)
		require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
		require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
		actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
		require.NoError(t, err)
		require.Contains(t, string(actual), `"role":"system","tools"`)
		return batch
	}

	requiredBatch := prepare("req-kimi-k3-dynamic-required", "required")
	require.Contains(t, requiredBatch.plan.CapabilityRequirements, modelrouting.CapabilityRequirement{
		CapabilityID:       modelprofile.CapabilityToolChoiceReq,
		Origins:            []string{modelrouting.CapabilityOriginExplicitRequest},
		FunctionToolChoice: toolChoiceConstraint([]string{"global_search", "lookup"}, ""),
	})
	requiredRoute := &RouteResult{
		ModelProtocolProfile:       "protocol.kimi-openai-chat.k3.v5",
		PlanCapabilityRequirements: requiredBatch.routes[0].compat.PlanCapabilityRequirements,
	}
	require.True(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"lookup"}), requiredRoute).complete())
	require.True(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"global_search"}), requiredRoute).complete())
	require.False(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse(nil), requiredRoute).complete())
	require.False(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"other"}), requiredRoute).complete())

	noneBatch := prepare("req-kimi-k3-dynamic-none", "none")
	require.Contains(t, noneBatch.plan.CapabilityRequirements, modelrouting.CapabilityRequirement{
		CapabilityID:       modelprofile.CapabilityFunctionTools,
		Origins:            []string{modelrouting.CapabilityOriginExplicitRequest},
		FunctionToolChoice: toolChoiceConstraint([]string{"global_search", "lookup"}, ""),
	})
	noneRoute := &RouteResult{
		ModelProtocolProfile:       "protocol.kimi-openai-chat.k3.v5",
		PlanCapabilityRequirements: noneBatch.routes[0].compat.PlanCapabilityRequirements,
	}
	require.True(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse(nil), noneRoute).complete())
	require.False(t, inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse([]string{"lookup"}), noneRoute).complete())
}
