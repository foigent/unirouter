package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const doubaoEvolvingToolsProtocol = "protocol.doubao-seed-evolving-openai-chat.v4"

func TestDoubaoSeedEvolvingToolsDeriveTypedInvocationCapabilities(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.5.0")
	tool := `{"type":"function","function":{"name":"lookup_weather","description":"Look up weather","parameters":{"type":"object"},"strict":true}}`
	tests := []struct {
		name                  string
		body                  string
		wantChoiceRequest     string
		wantChoiceEffective   string
		wantChoiceOrigin      string
		wantParallelRequest   string
		wantParallelEffective bool
		wantParallelOrigin    string
		wantErr               string
	}{
		{
			name:              "provider defaults are typed evidence",
			body:              `{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
			wantChoiceRequest: "omitted", wantChoiceEffective: "auto", wantChoiceOrigin: modelrouting.CapabilityOriginProviderDefault,
			wantParallelRequest: "omitted", wantParallelEffective: true, wantParallelOrigin: modelrouting.CapabilityOriginProviderDefault,
		},
		{
			name:              "description and parameters are optional",
			body:              `{"model":"doubao-seed-evolving","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}]}`,
			wantChoiceRequest: "omitted", wantChoiceEffective: "auto", wantChoiceOrigin: modelrouting.CapabilityOriginProviderDefault,
			wantParallelRequest: "omitted", wantParallelEffective: true, wantParallelOrigin: modelrouting.CapabilityOriginProviderDefault,
		},
		{
			name:              "explicit required and serial",
			body:              `{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"tool_choice":"required","parallel_tool_calls":false}`,
			wantChoiceRequest: "required", wantChoiceEffective: "required", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantParallelRequest: "false", wantParallelOrigin: modelrouting.CapabilityOriginExplicitRequest,
		},
		{
			name:              "named tool choice",
			body:              `{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup_weather"}}}`,
			wantChoiceRequest: "named", wantChoiceEffective: "named", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantParallelRequest: "omitted", wantParallelEffective: true, wantParallelOrigin: modelrouting.CapabilityOriginProviderDefault,
		},
		{
			name:              "explicit none without tools is bounded",
			body:              `{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"hello"}],"tool_choice":"none"}`,
			wantChoiceRequest: "none", wantChoiceEffective: "none", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantParallelRequest: "omitted",
		},
		{name: "auto without tools", body: `{"model":"doubao-seed-evolving","messages":[],"tool_choice":"auto"}`, wantErr: "requires at least one function tool"},
		{name: "required without tools", body: `{"model":"doubao-seed-evolving","messages":[],"tool_choice":"required"}`, wantErr: "requires at least one function tool"},
		{name: "named undeclared tool", body: `{"model":"doubao-seed-evolving","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"other"}}}`, wantErr: "is not declared"},
		{name: "parallel without callable tools", body: `{"model":"doubao-seed-evolving","messages":[],"parallel_tool_calls":true}`, wantErr: "requires callable function tools"},
		{name: "parallel with tool choice none", body: `{"model":"doubao-seed-evolving","messages":[],"tools":[` + tool + `],"tool_choice":"none","parallel_tool_calls":false}`, wantErr: "requires callable function tools"},
		{name: "stream tools are deferred", body: `{"model":"doubao-seed-evolving","messages":[],"stream":true,"tools":[` + tool + `]}`, wantErr: "require stream=false"},
		{name: "parameters must be object when present", body: `{"model":"doubao-seed-evolving","messages":[],"tools":[{"type":"function","function":{"name":"lookup","parameters":true}}]}`, wantErr: "parameters must be a JSON schema object"},
		{name: "strict must be boolean", body: `{"model":"doubao-seed-evolving","messages":[],"tools":[{"type":"function","function":{"name":"lookup","strict":"true"}}]}`, wantErr: "strict must be a boolean"},
		{name: "name uses local portable subset", body: `{"model":"doubao-seed-evolving","messages":[],"tools":[{"type":"function","function":{"name":"lookup.weather"}}]}`, wantErr: "function name must match"},
		{name: "duplicate names are rejected", body: `{"model":"doubao-seed-evolving","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}},{"type":"function","function":{"name":"lookup"}}]}`, wantErr: "duplicate function name"},
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
			require.Equal(t, test.wantChoiceRequest, evidence.FunctionToolChoiceRequest)
			require.Equal(t, test.wantChoiceEffective, evidence.FunctionToolChoiceEffective)
			require.Equal(t, test.wantChoiceOrigin, evidence.FunctionToolChoiceOrigin)
			require.Equal(t, test.wantParallelRequest, evidence.ParallelToolCallsRequest)
			require.Equal(t, test.wantParallelEffective, evidence.ParallelToolCallsEffective)
			require.Equal(t, test.wantParallelOrigin, evidence.ParallelToolCallsOrigin)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}

	tools := make([]string, 129)
	for index := range tools {
		tools[index] = fmt.Sprintf(`{"type":"function","function":{"name":"tool_%d"}}`, index)
	}
	tooMany := `{"model":"doubao-seed-evolving","messages":[],"tools":[` + strings.Join(tools, ",") + `]}`
	_, err := validateChatExecutionProfileRequest([]byte(tooMany), plan)
	require.ErrorContains(t, err, "at most 128 functions")
}

func TestDoubaoSeedEvolvingToolsRequireOpaqueReasoningStateForToolHistory(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.5.0")
	base := `{"model":"doubao-seed-evolving","messages":[{"role":"assistant","content":null,%s"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","name":"lookup","content":"ok"}]}`

	_, err := validateChatExecutionProfileRequest([]byte(formatTestJSON(base, "")), plan)
	require.ErrorContains(t, err, "must preserve encrypted_content")

	body := []byte(formatTestJSON(base, `"encrypted_content":"opaque-signature",`))
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.NoError(t, func() error { _, err := estimateChatPreProfileAdmission(body); return err }())
	require.NoError(t, func() error { _, err := estimateChatAdmissionForPlan(body, plan); return err }())
	require.Empty(t, evidence.ProviderRequestNormalizations)

	_, err = validateChatExecutionProfileRequest([]byte(formatTestJSON(base, `"encrypted_content":"",`)), plan)
	require.ErrorContains(t, err, "must be a non-empty assistant string")

	badReference := []byte(`{"model":"doubao-seed-evolving","messages":[{"role":"tool","tool_call_id":"missing","content":"ok"}]}`)
	_, err = validateChatExecutionProfileRequest(badReference, plan)
	require.ErrorContains(t, err, "does not reference prior assistant tool_calls")

	unresolved := []byte(formatTestJSON(
		`{"model":"doubao-seed-evolving","messages":[{"role":"assistant","content":null,%s"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}]}`,
		`"encrypted_content":"opaque-signature",`,
	))
	_, err = validateChatExecutionProfileRequest(unresolved, plan)
	require.ErrorContains(t, err, "must be followed by matching tool messages")

	historical := executionProfilePlan(t, "doubao.seed-evolving.api", "1.4.0")
	_, err = validateChatExecutionProfileRequest(body, historical)
	require.ErrorContains(t, err, "encrypted_content history is not supported")

	evidence, err = validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.True(t, evidence.FunctionToolsRequested, "validated exact tool history is invocation-required even without a new tools declaration")
}

func TestDoubaoSeedEvolvingToolsRejectToolFieldsOutsideExactHistoryRoles(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.5.0")
	contract, modeled := modelprofile.ResolveChatProtocolContract(doubaoEvolvingToolsProtocol)
	require.True(t, modeled)
	tests := []struct {
		name    string
		message string
		wantErr string
	}{
		{
			name:    "user cannot carry tool calls",
			message: `{"role":"user","content":"weather","tool_calls":[]}`,
			wantErr: `user message field "tool_calls" has no bounded forwarding contract`,
		},
		{
			name:    "system cannot carry tool call id",
			message: `{"role":"system","content":"help","tool_call_id":"call_1"}`,
			wantErr: `system message field "tool_call_id" has no bounded forwarding contract`,
		},
		{
			name:    "unknown role is rejected",
			message: `{"role":"developer","content":"help"}`,
			wantErr: `role "developer" is not supported`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var messages []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(`[`+test.message+`]`), &messages))
			require.ErrorContains(t, validateExactFunctionToolHistory(messages, contract), test.wantErr)

			body := []byte(`{"model":"doubao-seed-evolving","messages":[` + test.message + `]}`)
			_, err := validateChatExecutionProfileRequest(body, plan)
			require.Error(t, err)
		})
	}
}

func TestDoubaoSeedEvolvingToolsUseOnePlannerCapabilitySnapshot(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &plan, "doubao.seed-evolving.api", "1.5.0")
	plan.Routes[0].Provider = "doubao"
	plan.Routes[0].UpstreamModel = "doubao-seed-evolving"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-evolving-tools",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityFunctionTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
		{CapabilityID: modelprofile.CapabilityParallelTools, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityToolChoiceAuto, Origins: []string{modelrouting.CapabilityOriginProviderDefault}, FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, "")},
		{CapabilityID: modelprofile.CapabilityTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, []string{
		modelprofile.CapabilityFunctionTools, modelprofile.CapabilityParallelTools,
		modelprofile.CapabilityReasoningEffort, modelprofile.CapabilityToolChoiceAuto, modelprofile.CapabilityTools,
	}, batch.plan.RequiredCapabilities)
	planner.mu.Lock()
	require.Len(t, planner.requests, 1)
	require.Equal(t, batch.plan.CapabilityRequirements, planner.requests[0].InvocationCapabilities)
	planner.mu.Unlock()

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actual, &payload))
	require.NotContains(t, payload, "tool_choice", "provider default must remain wire omission")
	require.NotContains(t, payload, "parallel_tool_calls", "provider default must remain wire omission")
}

func TestDoubaoSeedEvolvingToolsResponseIsExactToolCallsOnly(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-doubao-tools", "model": "doubao-seed-evolving",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "tool_calls",
			"message": map[string]interface{}{
				"role": "assistant", "content": nil, "reasoning_content": "need lookup", "encrypted_content": "opaque",
				"tool_calls": []interface{}{map[string]interface{}{
					"id": "call_1", "type": "function",
					"function": map[string]interface{}{"name": "lookup", "arguments": `{"city":"Shanghai"}`},
				}},
			},
		}},
	}
	inspection := inspectNonStreamOutputTextsForProtocol(response, doubaoEvolvingToolsProtocol)
	require.True(t, inspection.complete(), inspection.incompletePaths)

	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["function_call"] = map[string]interface{}{"name": "lookup", "arguments": `{}`}
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoEvolvingToolsProtocol).complete())
	delete(message, "function_call")

	message["role"] = "tool"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoEvolvingToolsProtocol).complete())
	message["role"] = "assistant"

	toolCall := message["tool_calls"].([]interface{})[0].(map[string]interface{})
	toolCall["index"] = json.Number("0")
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoEvolvingToolsProtocol).complete())
	delete(toolCall, "index")
	toolCall["id"] = ""
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoEvolvingToolsProtocol).complete())
	toolCall["id"] = "call_1"
	toolCall["function"].(map[string]interface{})["name"] = ""
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoEvolvingToolsProtocol).complete())
	toolCall["function"].(map[string]interface{})["name"] = "lookup"

	choice := response["choices"].([]interface{})[0].(map[string]interface{})
	choice["text"] = "legacy function-call text"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoEvolvingToolsProtocol).complete())
	delete(choice, "text")

	choice["finish_reason"] = "stop"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoEvolvingToolsProtocol).complete())
}

func formatTestJSON(format, insertion string) string {
	return jsonFormat(format, insertion)
}

func jsonFormat(format, insertion string) string {
	return fmt.Sprintf(format, insertion)
}
