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

const miniMaxM3ToolsProtocol = "protocol.minimax-m3-openai-chat.v9"

func TestMiniMaxM3ExactFunctionToolsCandidateUsesDocumentedRequestSubset(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m3.token-plan", "1.7.0")
	tool := `{"type":"function","function":{"name":"lookup_weather","description":"Look up weather","parameters":{"type":"object"}}}`
	body := []byte(`{"model":"MiniMax-M3","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"max_completion_tokens":128}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.True(t, evidence.FunctionToolInvocationModeled)
	require.True(t, evidence.FunctionToolsRequested)
	require.Equal(t, "omitted", evidence.FunctionToolChoiceRequest)
	require.Equal(t, "auto", evidence.FunctionToolChoiceEffective)
	require.Equal(t, modelrouting.CapabilityOriginProviderDefault, evidence.FunctionToolChoiceOrigin)
	_, err = estimateChatAdmissionForPlan(body, plan)
	require.NoError(t, err)

	for _, test := range []struct {
		name string
		body string
		err  string
	}{
		{name: "explicit auto", body: `{"model":"MiniMax-M3","messages":[],"tools":[` + tool + `],"tool_choice":"auto"}`, err: "tool_choice is not supported"},
		{name: "explicit none", body: `{"model":"MiniMax-M3","messages":[],"tool_choice":"none"}`, err: "tool_choice is not supported"},
		{name: "named choice", body: `{"model":"MiniMax-M3","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup_weather"}}}`, err: "tool_choice is not supported"},
		{name: "parallel", body: `{"model":"MiniMax-M3","messages":[],"tools":[` + tool + `],"parallel_tool_calls":false}`, err: "parallel_tool_calls is not supported"},
		{name: "missing parameters", body: `{"model":"MiniMax-M3","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}]}`, err: "parameters must be a JSON schema object"},
		{name: "non-object parameters", body: `{"model":"MiniMax-M3","messages":[],"tools":[{"type":"function","function":{"name":"lookup","parameters":true}}]}`, err: "parameters must be a JSON schema object"},
		{name: "stream tools", body: `{"model":"MiniMax-M3","messages":[],"thinking":{"type":"disabled"},"stream":true,"tools":[` + tool + `]}`, err: "require stream=false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.ErrorContains(t, err, test.err)
		})
	}
}

func TestMiniMaxM3ExactFunctionToolsCandidateRequiresCompleteHistory(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m3.token-plan", "1.7.0")
	complete := []byte(`{"model":"MiniMax-M3","messages":[{"role":"assistant","content":"<think>lookup</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	_, err := validateChatExecutionProfileRequest(complete, plan)
	require.NoError(t, err)
	_, err = estimateChatAdmissionForPlan(complete, plan)
	require.NoError(t, err)

	for _, test := range []struct {
		name string
		body string
		err  string
	}{
		{name: "missing replay content", body: `{"model":"MiniMax-M3","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`, err: "must preserve content"},
		{name: "wrong tool id", body: `{"model":"MiniMax-M3","messages":[{"role":"assistant","content":"<think>lookup</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_2","content":"ok"}]}`, err: "does not reference prior assistant tool_calls"},
		{name: "unresolved call", body: `{"model":"MiniMax-M3","messages":[{"role":"assistant","content":"<think>lookup</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}]}`, err: "must be followed by matching tool messages"},
		{name: "message before tool result", body: `{"model":"MiniMax-M3","messages":[{"role":"assistant","content":"<think>lookup</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"user","content":"skip"}]}`, err: "appears before prior assistant tool_calls are resolved"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.ErrorContains(t, err, test.err)
		})
	}
}

func TestMiniMaxM3ExactFunctionToolsCandidateUsesOnePlanSnapshot(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m3.token-plan", "1.7.0")
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].UpstreamModel = "MiniMax-M3"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"max_completion_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-tools",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityCacheReadUsage, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityFunctionTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
		{CapabilityID: modelprofile.CapabilityPromptCache, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityToolChoiceAuto, Origins: []string{modelrouting.CapabilityOriginProviderDefault}, FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, "")},
		{CapabilityID: modelprofile.CapabilityTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actual, &payload))
	require.Equal(t, `"MiniMax-M3"`, string(payload["model"]))
	require.Contains(t, payload, "tools")
	require.NotContains(t, payload, "tool_choice")
	require.NotContains(t, payload, "parallel_tool_calls")
}

func TestMiniMaxM3ExactFunctionToolsCandidateAcceptsOnlyExactResponse(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-minimax-m3-tools", "model": "MiniMax-M3",
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
	require.True(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM3ToolsProtocol).complete())

	choice := response["choices"].([]interface{})[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	choice["finish_reason"] = "stop"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM3ToolsProtocol).complete())
	choice["finish_reason"] = "tool_calls"
	message["content"] = nil
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM3ToolsProtocol).complete())
	message["content"] = "<think>lookup</think>"
	toolCall := message["tool_calls"].([]interface{})[0].(map[string]interface{})
	toolCall["index"] = json.Number("0")
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM3ToolsProtocol).complete())
	delete(toolCall, "index")
	message["function_call"] = map[string]interface{}{"name": "lookup", "arguments": `{}`}
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM3ToolsProtocol).complete())
}
