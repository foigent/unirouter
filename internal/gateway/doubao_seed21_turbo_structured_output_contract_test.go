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

const doubaoSeed21TurboStrictJSONSchemaProtocol = "protocol.doubao-seed-2.1-turbo-openai-chat.v4"

func TestDoubaoSeed21TurboStrictJSONSchemaCandidateUsesOnePlanSnapshot(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.4.0")
	body := []byte(`{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"user","content":"return coordinates"}],"response_format":{"type":"json_schema","json_schema":{"name":"coordinates","description":"A coordinate pair","strict":true,"schema":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"],"additionalProperties":false}}}}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	_, err = estimateChatAdmissionForPlan(body, plan)
	require.NoError(t, err)

	profile, ok := modelprofile.Lookup("doubao.seed-2.1-turbo.api", "1.4.0")
	require.True(t, ok)
	require.Contains(t, profile.Capabilities, modelprofile.CapabilityStrictJSONSchema)
	require.Equal(t, doubaoSeed21TurboStrictJSONSchemaProtocol, profile.ProtocolProfile)
	requirements, err := invocationCapabilityRequirementsForChat(profile, evidence, body)
	require.NoError(t, err)
	require.Contains(t, requirements, modelrouting.CapabilityRequirement{
		CapabilityID: modelprofile.CapabilityStrictJSONSchema,
		Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
	})

	encKey := []byte("0123456789abcdef0123456789abcdef")
	executionPlan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &executionPlan, "doubao.seed-2.1-turbo.api", "1.4.0")
	executionPlan.Routes[0].Provider = "doubao"
	executionPlan.Routes[0].UpstreamModel = "doubao-seed-2-1-turbo-260628"
	planner := &executionPlannerStub{plan: executionPlan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-seed21-turbo-strict-json",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityStrictJSONSchema, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var want, got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &want))
	require.NoError(t, json.Unmarshal(actual, &got))
	want["model"] = json.RawMessage(`"doubao-seed-2-1-turbo-260628"`)
	require.Equal(t, want, got)
}

func TestDoubaoSeed21TurboStrictJSONSchemaCandidateRejectsUnprovenCombinations(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.4.0")
	definition := `"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object"}}}`
	tool := `{"type":"function","function":{"name":"lookup"}}`
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "json object remains closed", body: `{"model":"m","messages":[],"response_format":{"type":"json_object"}}`, wantErr: "must be one of text, json_schema"},
		{name: "strict false", body: `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":false,"schema":{}}}}`, wantErr: "strict must be true"},
		{name: "strict omitted", body: `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{}}}}`, wantErr: "strict must be true"},
		{name: "schema must be object", body: `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":true}}}`, wantErr: "schema must be a JSON schema object"},
		{name: "description must be string", body: `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","description":null,"strict":true,"schema":{}}}}`, wantErr: "description must be a string when present"},
		{name: "stream remains closed", body: `{"model":"m","messages":[],"stream":true,` + definition + `}`, wantErr: "requires stream=false"},
		{name: "tools remain closed", body: `{"model":"m","messages":[],"tools":[` + tool + `],` + definition + `}`, wantErr: "cannot be combined with function tools"},
		{name: "function tool history remains closed", body: `{"model":"m","messages":[{"role":"assistant","content":null,"encrypted_content":"opaque-signature","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}],` + definition + `}`, wantErr: "cannot be combined with function tool history"},
		{name: "tool choice remains closed", body: `{"model":"m","messages":[],"tool_choice":"none",` + definition + `}`, wantErr: "cannot be combined with tool_choice"},
		{name: "frequency penalty remains closed", body: `{"model":"m","messages":[],"frequency_penalty":0,` + definition + `}`, wantErr: "cannot be combined with frequency_penalty"},
		{name: "presence penalty remains closed", body: `{"model":"m","messages":[],"presence_penalty":0,` + definition + `}`, wantErr: "cannot be combined with presence_penalty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.ErrorContains(t, err, test.wantErr)
		})
	}

	_, err := validateChatExecutionProfileRequest([]byte(`{"model":"m","messages":[],"stream":true}`), plan)
	require.NoError(t, err, "v4 must retain ordinary streaming")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"m","messages":[],"tools":[`+tool+`]}`), plan)
	require.NoError(t, err, "v4 must retain non-stream function tools")

	historical := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.3.0")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"m","messages":[],`+definition+`}`), historical)
	require.ErrorContains(t, err, "must be one of text")
}

func TestDoubaoSeed21TurboStrictJSONSchemaCandidateResponseUsesInvocationSnapshot(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-json", "model": "doubao-seed-2-1-turbo-260628",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "stop",
			"message": map[string]interface{}{"role": "assistant", "content": `{"x":1,"y":2}`},
		}},
	}
	strictRoute := &RouteResult{
		ModelProtocolProfile: doubaoSeed21TurboStrictJSONSchemaProtocol,
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID: modelprofile.CapabilityStrictJSONSchema,
			Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForRoute(response, strictRoute).complete())

	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["content"] = "not json"
	require.False(t, inspectNonStreamOutputTextsForRoute(response, strictRoute).complete())
	require.True(t, inspectNonStreamOutputTextsForRoute(response, &RouteResult{ModelProtocolProfile: doubaoSeed21TurboStrictJSONSchemaProtocol}).complete())

	message["content"] = `{"x":1}`
	message["refusal"] = "unmodeled refusal"
	require.False(t, inspectNonStreamOutputTextsForRoute(response, strictRoute).complete())
	delete(message, "refusal")
	response["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] = "length"
	require.False(t, inspectNonStreamOutputTextsForRoute(response, strictRoute).complete())
}
