package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const doubaoSeed21TurboToolsProtocol = "protocol.doubao-seed-2.1-turbo-openai-chat.v3"

func TestDoubaoSeed21TurboToolsCandidateDerivesTypedNonStreamInvocation(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.3.0")
	tool := `{"type":"function","function":{"name":"lookup_weather","parameters":{"type":"object"}}}`
	tests := []struct {
		name                string
		body                string
		wantChoiceEffective string
		wantChoiceOrigin    string
		wantParallel        bool
		wantParallelOrigin  string
		wantErr             string
	}{
		{
			name:                "provider tool defaults with provider reasoning default",
			body:                `{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
			wantChoiceEffective: "auto", wantChoiceOrigin: modelrouting.CapabilityOriginProviderDefault,
			wantParallel: true, wantParallelOrigin: modelrouting.CapabilityOriginProviderDefault,
		},
		{
			name:                "explicit required serial and high reasoning",
			body:                `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"tools":[` + tool + `],"tool_choice":"required","parallel_tool_calls":false,"reasoning_effort":"high"}`,
			wantChoiceEffective: "required", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantParallelOrigin: modelrouting.CapabilityOriginExplicitRequest,
		},
		{
			name:                "named tool with reasoning disabled",
			body:                `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup_weather"}},"reasoning_effort":"minimal"}`,
			wantChoiceEffective: "named", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantParallel: true, wantParallelOrigin: modelrouting.CapabilityOriginProviderDefault,
		},
		{name: "auto needs tools", body: `{"model":"m","messages":[],"tool_choice":"auto"}`, wantErr: "requires at least one function tool"},
		{name: "named tool must be declared", body: `{"model":"m","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"other"}}}`, wantErr: "is not declared"},
		{name: "stream tools remain deferred", body: `{"model":"m","messages":[],"stream":true,"tools":[` + tool + `]}`, wantErr: "require stream=false"},
		{name: "strict tool schema remains closed", body: `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"lookup","strict":true}}]}`, wantErr: `function field "strict" has no bounded forwarding contract`},
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
			require.Equal(t, test.wantChoiceEffective, evidence.FunctionToolChoiceEffective)
			require.Equal(t, test.wantChoiceOrigin, evidence.FunctionToolChoiceOrigin)
			require.Equal(t, test.wantParallel, evidence.ParallelToolCallsEffective)
			require.Equal(t, test.wantParallelOrigin, evidence.ParallelToolCallsOrigin)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestDoubaoSeed21TurboToolsCandidateRequiresOpaqueReasoningReplay(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.3.0")
	history := `{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"assistant","content":null,%s"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]%s}`

	_, err := validateChatExecutionProfileRequest([]byte(fmt.Sprintf(history, "", "")), plan)
	require.ErrorContains(t, err, "must preserve encrypted_content")

	body := []byte(fmt.Sprintf(history, `"encrypted_content":"opaque-signature",`, ""))
	_, err = validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)

	disabled := []byte(fmt.Sprintf(history, "", `,"reasoning_effort":"minimal"`))
	_, err = validateChatExecutionProfileRequest(disabled, plan)
	require.NoError(t, err, "minimal reasoning must not manufacture opaque state")

	historical := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.2.0")
	_, err = validateChatExecutionProfileRequest(body, historical)
	require.ErrorContains(t, err, "encrypted_content history is not supported")
}

func TestDoubaoSeed21TurboToolsCandidateUsesOnePlannerCapabilitySnapshot(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &plan, "doubao.seed-2.1-turbo.api", "1.3.0")
	plan.Routes[0].Provider = "doubao"
	plan.Routes[0].UpstreamModel = "doubao-seed-2-1-turbo-260628"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-seed21-turbo-tools",
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
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actual, &payload))
	require.NotContains(t, payload, "tool_choice")
	require.NotContains(t, payload, "parallel_tool_calls")
}

func TestDoubaoSeed21TurboToolsCandidateAcceptsOnlyExactToolCallResponse(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-doubao-turbo-tools", "model": "doubao-seed-2-1-turbo-260628",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "tool_calls",
			"message": map[string]interface{}{
				"role": "assistant", "content": nil, "reasoning_content": "need lookup", "encrypted_content": "opaque",
				"tool_calls": []interface{}{map[string]interface{}{
					"id": "call_1", "type": "function",
					"function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
				}},
			},
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForProtocol(response, doubaoSeed21TurboToolsProtocol).complete())

	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["function_call"] = map[string]interface{}{"name": "lookup", "arguments": `{}`}
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoSeed21TurboToolsProtocol).complete())
	delete(message, "function_call")
	response["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] = "stop"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoSeed21TurboToolsProtocol).complete())
}
