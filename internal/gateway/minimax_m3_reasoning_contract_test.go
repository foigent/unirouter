package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const miniMaxM3ReasoningProtocol = "protocol.minimax-m3-openai-chat.v5"

func TestMiniMaxM3ReasoningCandidateValidatesTypedControls(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m3.token-plan", "1.3.0")
	tests := []struct {
		name, body, toggle, split, err string
	}{
		{name: "provider adaptive default", body: `{"model":"MiniMax-M3","messages":[]}`, toggle: "omitted", split: "omitted"},
		{name: "explicit adaptive", body: `{"model":"MiniMax-M3","messages":[],"thinking":{"type":"adaptive"}}`, toggle: "adaptive", split: "omitted"},
		{name: "explicit disabled", body: `{"model":"MiniMax-M3","messages":[],"thinking":{"type":"disabled"}}`, toggle: "disabled", split: "omitted"},
		{name: "embedded split explicit", body: `{"model":"MiniMax-M3","messages":[],"reasoning_split":false}`, toggle: "omitted", split: "false"},
		{name: "separate split rejected", body: `{"model":"MiniMax-M3","messages":[],"reasoning_split":true}`, err: "reasoning_split=true is not supported"},
		{name: "split type rejected", body: `{"model":"MiniMax-M3","messages":[],"reasoning_split":"false"}`, err: "reasoning_split must be a boolean"},
		{name: "unknown thinking rejected", body: `{"model":"MiniMax-M3","messages":[],"thinking":{"type":"enabled"}}`, err: "thinking must be one of adaptive, disabled"},
		{name: "active stream rejected", body: `{"model":"MiniMax-M3","messages":[],"stream":true}`, err: "streaming is not supported while reasoning is active"},
		{name: "disabled plain stream accepted", body: `{"model":"MiniMax-M3","messages":[],"stream":true,"thinking":{"type":"disabled"}}`, toggle: "disabled", split: "omitted"},
		{name: "stream tools rejected", body: `{"model":"MiniMax-M3","messages":[],"stream":true,"thinking":{"type":"disabled"},"tools":[{"type":"function","function":{"name":"lookup"}}]}`, err: "require stream=false"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.err != "" {
				require.ErrorContains(t, err, test.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.toggle, evidence.ReasoningToggleRequest)
			require.Equal(t, test.split, evidence.ReasoningSplitRequest)
			require.Equal(t, "false", evidence.ReasoningSplitDefault)
			require.NoError(t, func() error { _, err := estimateChatAdmissionForPlan([]byte(test.body), plan); return err }())
		})
	}

	historical := executionProfilePlan(t, "minimax.m3.token-plan", "1.2.0")
	for _, body := range []string{
		`{"model":"MiniMax-M3","messages":[],"thinking":{"type":"adaptive"}}`,
		`{"model":"MiniMax-M3","messages":[],"reasoning_split":false}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), historical)
		require.ErrorContains(t, err, "reasoning control is not supported")
	}
}

func TestMiniMaxM3ReasoningCandidateUsesOnePlannerCapabilitySnapshot(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m3.token-plan", "1.3.0")
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].UpstreamModel = "MiniMax-M3"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"reasoning_split":false,"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-reasoning",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{{
		CapabilityID: modelprofile.CapabilityReasoning,
		Origins:      []string{modelrouting.CapabilityOriginProviderDefault},
	}}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Equal(t, "false", batch.profileEvidence.ReasoningSplitRequest)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"MiniMax-M3","messages":[{"role":"user","content":"hello"}],"reasoning_split":false,"max_tokens":128}`, string(actual))
}

func TestMiniMaxM3ReasoningCandidateRequiresAndPreservesToolHistoryContent(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m3.token-plan", "1.3.0")
	complete := []byte(`{"model":"MiniMax-M3","messages":[{"role":"assistant","content":"<think>lookup</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	_, err := validateChatExecutionProfileRequest(complete, plan)
	require.NoError(t, err)

	missing := []byte(`{"model":"MiniMax-M3","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	_, err = validateChatExecutionProfileRequest(missing, plan)
	require.ErrorContains(t, err, "must preserve content")

	encKey := []byte("0123456789abcdef0123456789abcdef")
	routePlan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &routePlan, "minimax.m3.token-plan", "1.3.0")
	routePlan.Routes[0].Provider = "minimax"
	routePlan.Routes[0].UpstreamModel = "MiniMax-M3"
	freezeCalls := 0
	billingMock := &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
		freezeCalls++
		return &billing.FreezeResult{Success: true}, nil
	}}
	coordinator := &executionCoordinatorStub{}
	relay := NewRelay(executionTestDB(), nil, nil, billingMock, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: routePlan}, coordinator, routingpolicy.Default()))

	changed := []byte(`{"model":"MiniMax-M3","messages":[{"role":"assistant","content":"redacted","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}],"max_tokens":128}`)
	ctx := contextWithAssistantToolHistorySnapshot(context.Background(), complete)
	explicitOutput := 128
	_, err = relay.prepareModelExecutionRoutes(ctx, modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-preserve",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		changed, time.Now())
	require.ErrorContains(t, err, "was changed before exact-profile execution")
	require.Zero(t, freezeCalls)
	require.Zero(t, coordinator.authorizeCount.Load())

	changedArguments := []byte(`{"model":"MiniMax-M3","messages":[{"role":"assistant","content":"<think>lookup</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"redacted\":true}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}],"max_tokens":128}`)
	_, err = relay.prepareModelExecutionRoutes(ctx, modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-preserve-arguments",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		changedArguments, time.Now())
	require.ErrorContains(t, err, "was changed before exact-profile execution")
	require.Zero(t, freezeCalls)
	require.Zero(t, coordinator.authorizeCount.Load())
}

func TestMiniMaxM3ReasoningCandidateAcceptsOnlyEmbeddedResponseAndProtectsToolTranscript(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-m3", "model": "MiniMax-M3",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "stop",
			"message": map[string]interface{}{"role": "assistant", "content": "<think>work</think>answer"},
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM3ReasoningProtocol).complete())
	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["reasoning_content"] = "work"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, miniMaxM3ReasoningProtocol).complete())
	delete(message, "reasoning_content")

	route := &RouteResult{ReasoningProfile: "reasoning.minimax-m3-adaptive-embedded.v1", ModelProtocolProfile: miniMaxM3ReasoningProtocol}
	plain := inspectNonStreamOutputTextsForRoute(response, route)
	transformed := guardrail.Result{Transformed: true, Texts: append([]guardrail.Text(nil), plain.texts...)}
	transformed.Texts[0].Content = "safe answer"
	require.NoError(t, applyProfileAwareOutputGuardrailTransform(route, transformed, plain.refs))

	message["content"] = "<think>lookup</think>"
	message["tool_calls"] = []interface{}{map[string]interface{}{
		"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
	}}
	response["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] = "tool_calls"
	toolResponse := inspectNonStreamOutputTextsForRoute(response, route)
	require.True(t, toolResponse.complete())
	message["content"] = nil
	require.False(t, inspectNonStreamOutputTextsForRoute(response, route).complete())
	delete(message, "content")
	require.False(t, inspectNonStreamOutputTextsForRoute(response, route).complete())
	message["content"] = "<think>lookup</think>"
	toolResponse = inspectNonStreamOutputTextsForRoute(response, route)
	toolTransform := guardrail.Result{Transformed: true, Texts: append([]guardrail.Text(nil), toolResponse.texts...)}
	for index := range toolTransform.Texts {
		if toolTransform.Texts[index].Path == "choices[0].message.content" {
			toolTransform.Texts[index].Content = "redacted"
		}
	}
	require.ErrorContains(t, applyProfileAwareOutputGuardrailTransform(route, toolTransform, toolResponse.refs), "preserved assistant tool transcript")

	argumentTransform := guardrail.Result{Transformed: true, Texts: append([]guardrail.Text(nil), toolResponse.texts...)}
	for index := range argumentTransform.Texts {
		if argumentTransform.Texts[index].Path == "choices[0].message.tool_calls[0].function.arguments" {
			argumentTransform.Texts[index].Content = `{"redacted":true}`
		}
	}
	require.ErrorContains(t, applyProfileAwareOutputGuardrailTransform(route, argumentTransform, toolResponse.refs), "preserved assistant tool transcript")
}

func TestMiniMaxM3ReasoningCandidateRejectsSeparateStreamReasoning(t *testing.T) {
	content, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}]}`), miniMaxM3ReasoningProtocol,
	)
	require.NoError(t, err)
	require.False(t, content.unsupported)

	separate, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[{"index":0,"delta":{"reasoning_content":"work"},"finish_reason":null}]}`), miniMaxM3ReasoningProtocol,
	)
	require.NoError(t, err)
	require.True(t, separate.unsupported)

	for _, raw := range []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":null},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"index":0,"delta":{"function_call":{"name":"lookup","arguments":"{}"}},"finish_reason":"function_call"}]}`,
	} {
		frame, parseErr := parseGuardedStreamFrameForProtocol([]byte(raw), miniMaxM3ReasoningProtocol)
		require.NoError(t, parseErr)
		require.True(t, frame.unsupported, raw)
	}
}
