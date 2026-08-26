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
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestMiniMaxM25HighspeedExactFunctionToolsCandidateUsesReviewedM2Contract(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m2.5-highspeed.token-plan", "1.5.0")
	tool := `{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}`
	body := []byte(`{"model":"MiniMax-M2.5-highspeed","messages":[{"role":"user","content":"lookup"}],"tools":[` + tool + `]}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.Equal(t, modelprofile.ReasoningModeRequired, evidence.ReasoningMode)
	require.True(t, evidence.FunctionToolInvocationModeled)
	require.Equal(t, "auto", evidence.FunctionToolChoiceEffective)
	require.Equal(t, modelrouting.CapabilityOriginProviderDefault, evidence.FunctionToolChoiceOrigin)

	for _, test := range []struct {
		body string
		err  string
	}{
		{body: `{"model":"MiniMax-M2.5-highspeed","messages":[],"tools":[` + tool + `],"tool_choice":"auto"}`, err: "tool_choice is not supported"},
		{body: `{"model":"MiniMax-M2.5-highspeed","messages":[],"tools":[` + tool + `],"parallel_tool_calls":false}`, err: "parallel_tool_calls is not supported"},
		{body: `{"model":"MiniMax-M2.5-highspeed","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}]}`, err: "parameters must be a JSON schema object"},
	} {
		_, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
		require.ErrorContains(t, err, test.err)
	}

	history := []byte(`{"model":"MiniMax-M2.5-highspeed","messages":[{"role":"assistant","content":"<think>lookup</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	_, err = validateChatExecutionProfileRequest(history, plan)
	require.NoError(t, err)
	mismatch := []byte(`{"model":"MiniMax-M2.5-highspeed","messages":[{"role":"assistant","content":"<think>lookup</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_2","content":"ok"}]}`)
	_, err = validateChatExecutionProfileRequest(mismatch, plan)
	require.ErrorContains(t, err, "does not reference prior assistant tool_calls")
}

func TestMiniMaxM25HighspeedExactFunctionToolsCandidateUsesOnePlanSnapshot(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m2.5-highspeed.token-plan", "1.5.0")
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].UpstreamModel = "MiniMax-M2.5-highspeed"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"max_completion_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m25-tools",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityCacheReadUsage, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityFunctionTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
		{CapabilityID: modelprofile.CapabilityPromptCache, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityToolChoiceAuto, Origins: []string{modelrouting.CapabilityOriginProviderDefault}, FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, "")},
		{CapabilityID: modelprofile.CapabilityTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actual, &payload))
	require.Equal(t, `"MiniMax-M2.5-highspeed"`, string(payload["model"]))
	require.Contains(t, payload, "tools")
	require.NotContains(t, payload, "tool_choice")
}

func TestMiniMaxM25HighspeedExactFunctionToolsCandidateAcceptsOnlyExactResponse(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-minimax-m25-tools", "model": "MiniMax-M2.5-highspeed",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "tool_calls",
			"message": map[string]interface{}{
				"role": "assistant", "content": "<think>lookup</think>",
				"tool_calls": []interface{}{map[string]interface{}{
					"id": "call_1", "type": "function",
					"function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
				}},
			},
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM27ToolsProtocol).complete())
	choice := response["choices"].([]interface{})[0].(map[string]interface{})
	choice["finish_reason"] = "stop"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM27ToolsProtocol).complete())
}
