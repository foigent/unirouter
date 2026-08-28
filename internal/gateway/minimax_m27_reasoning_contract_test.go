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

const miniMaxM27ReasoningProtocol = "protocol.minimax-m2-openai-chat.v5"

func TestMiniMaxM27RequiredReasoningCandidateValidatesExactRequestBoundary(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m2.7.token-plan", "1.3.0")
	tests := []struct {
		name, body, split, err string
	}{
		{name: "required provider behavior", body: `{"model":"MiniMax-M2.7","messages":[]}`, split: "omitted"},
		{name: "embedded split explicit", body: `{"model":"MiniMax-M2.7","messages":[],"reasoning_split":false}`, split: "false"},
		{name: "plain stream remains supported", body: `{"model":"MiniMax-M2.7","messages":[],"stream":true}`, split: "omitted"},
		{name: "separate split rejected", body: `{"model":"MiniMax-M2.7","messages":[],"reasoning_split":true}`, err: "reasoning_split=true is not supported"},
		{name: "ineffective disabled control rejected", body: `{"model":"MiniMax-M2.7","messages":[],"thinking":{"type":"disabled"}}`, err: "does not accept thinking"},
		{name: "ineffective adaptive control rejected", body: `{"model":"MiniMax-M2.7","messages":[],"thinking":{"type":"adaptive"}}`, err: "does not accept thinking"},
		{name: "stream tools remain closed", body: `{"model":"MiniMax-M2.7","messages":[],"stream":true,"tools":[{"type":"function","function":{"name":"lookup"}}]}`, err: "require stream=false"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.err != "" {
				require.ErrorContains(t, err, test.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, modelprofile.ReasoningModeRequired, evidence.ReasoningMode)
			require.Equal(t, "required", evidence.ReasoningRequest)
			require.Equal(t, test.split, evidence.ReasoningSplitRequest)
			require.Equal(t, "false", evidence.ReasoningSplitDefault)
			require.NoError(t, func() error { _, err := estimateChatAdmissionForPlan([]byte(test.body), plan); return err }())
		})
	}

	historical := executionProfilePlan(t, "minimax.m2.7.token-plan", "1.2.0")
	for _, body := range []string{
		`{"model":"MiniMax-M2.7","messages":[],"reasoning_split":false}`,
		`{"model":"MiniMax-M2.7","messages":[],"thinking":{"type":"disabled"}}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), historical)
		require.ErrorContains(t, err, "reasoning control is not supported")
	}
}

func TestMiniMaxM27RequiredReasoningCandidateUsesOnePlannerSnapshot(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m2.7.token-plan", "1.3.0")
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].UpstreamModel = "MiniMax-M2.7"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"reasoning_split":false,"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m27-reasoning",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{{
		CapabilityID: modelprofile.CapabilityReasoning,
		Origins:      []string{modelrouting.CapabilityOriginProfileAlwaysRequired},
	}}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"MiniMax-M2.7","messages":[{"role":"user","content":"hello"}],"reasoning_split":false,"max_tokens":128}`, string(actual))
}

func TestMiniMaxM27RequiredReasoningCandidateEnforcesEmbeddedReplayAndResponse(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m2.7.token-plan", "1.3.0")
	complete := []byte(`{"model":"MiniMax-M2.7","messages":[{"role":"assistant","content":"<think>lookup</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	_, err := validateChatExecutionProfileRequest(complete, plan)
	require.NoError(t, err)
	missing := []byte(`{"model":"MiniMax-M2.7","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	_, err = validateChatExecutionProfileRequest(missing, plan)
	require.ErrorContains(t, err, "must preserve content")

	response := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "tool_calls",
			"message": map[string]interface{}{
				"role": "assistant", "content": "<think>lookup</think>",
				"tool_calls": []interface{}{map[string]interface{}{
					"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
				}},
			},
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM27ReasoningProtocol).complete())
	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["reasoning_content"] = "lookup"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM27ReasoningProtocol).complete())
	delete(message, "reasoning_content")
	message["content"] = nil
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM27ReasoningProtocol).complete())
}

func TestMiniMaxM27RequiredReasoningCandidateAcceptsOnlyEmbeddedStreamContent(t *testing.T) {
	content, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[{"index":0,"delta":{"content":"<think>work</think>answer"},"finish_reason":null}]}`), miniMaxM27ReasoningProtocol,
	)
	require.NoError(t, err)
	require.False(t, content.unsupported)

	for _, raw := range []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"work"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"reasoning":null},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[]},"finish_reason":"tool_calls"}]}`,
	} {
		frame, parseErr := parseGuardedStreamFrameForProtocol([]byte(raw), miniMaxM27ReasoningProtocol)
		require.NoError(t, parseErr)
		require.True(t, frame.unsupported, raw)
	}
}
