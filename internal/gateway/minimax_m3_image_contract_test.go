package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
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

const miniMaxM3InlineJPEGProtocol = "protocol.minimax-m3-openai-chat.v6"

func testMiniMaxM3ImageBody(t *testing.T, role string, images []string, extra map[string]any) []byte {
	t.Helper()
	parts := []any{map[string]any{"type": "text", "text": "describe the image"}}
	for _, dataURL := range images {
		parts = append(parts, map[string]any{
			"type": "image_url", "image_url": map[string]any{"url": dataURL},
		})
	}
	envelope := map[string]any{
		"model": "MiniMax-M3", "messages": []any{map[string]any{"role": role, "content": parts}}, "max_tokens": 128,
	}
	for key, value := range extra {
		envelope[key] = value
	}
	body, err := json.Marshal(envelope)
	require.NoError(t, err)
	return body
}

func TestMiniMaxM3InlineJPEGCandidateIsPlanBoundAndPreserved(t *testing.T) {
	dataURL := testJPEGDataURL(t, 32, 32)
	body := testMiniMaxM3ImageBody(t, "user", []string{dataURL}, nil)
	plan := executionProfilePlan(t, "minimax.m3.token-plan", "1.4.0")
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.True(t, evidence.ImageRequested)
	require.Equal(t, int64(32*32*4), evidence.ImageAdmissionTokenUpper)
	require.Equal(t, "chat-inline-jpeg-rgba-pixel-upper.v1", evidence.ImageAdmissionEstimateVersion)
	require.Equal(t, "adaptive", evidence.ReasoningProviderDefault)
	require.NotContains(t, string(evidence.AdmissionShadowBody), dataURL)

	evidence.bindAdmissionBody(body)
	estimate, err := estimateChatAdmissionForValidatedPlan(body, plan, evidence)
	require.NoError(t, err)
	require.GreaterOrEqual(t, estimate.InputTokens, 32*32*4)
	require.Equal(t, "chat-inline-jpeg-rgba-pixel-upper.v1+"+chatAdmissionEstimateVersion, estimate.Version)

	encKey := []byte("0123456789abcdef0123456789abcdef")
	executionPlan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &executionPlan, "minimax.m3.token-plan", "1.4.0")
	executionPlan.Routes[0].Provider = "minimax"
	executionPlan.Routes[0].UpstreamModel = "MiniMax-M3"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: executionPlan}, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-image",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityVision, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var want, got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &want))
	require.NoError(t, json.Unmarshal(actual, &got))
	require.Equal(t, want, got)
}

func TestMiniMaxM3InlineJPEGCandidateRejectsUnprovenShapes(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m3.token-plan", "1.4.0")
	valid := testJPEGDataURL(t, 32, 32)
	for _, test := range []struct {
		name    string
		body    []byte
		wantErr string
	}{
		{name: "remote URL", body: testMiniMaxM3ImageBody(t, "user", []string{"https://example.com/image.jpg"}, nil), wantErr: "currently supports only inline data URLs"},
		{name: "system image", body: testMiniMaxM3ImageBody(t, "system", []string{valid}, nil), wantErr: "not supported on system messages"},
		{name: "invalid JPEG", body: testMiniMaxM3ImageBody(t, "user", []string{"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("not-a-jpeg"))}, nil), wantErr: "valid JPEG"},
		{name: "wrong MIME", body: testMiniMaxM3ImageBody(t, "user", []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not-a-png"))}, nil), wantErr: "MIME type image/png"},
		{name: "stream", body: testMiniMaxM3ImageBody(t, "user", []string{valid}, map[string]any{"stream": true, "thinking": map[string]any{"type": "disabled"}}), wantErr: "requires stream=false"},
		{name: "tools", body: testMiniMaxM3ImageBody(t, "user", []string{valid}, map[string]any{"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}}}}), wantErr: "incompatible with tools"},
		{name: "strict JSON", body: testMiniMaxM3ImageBody(t, "user", []string{valid}, map[string]any{"response_format": map[string]any{"type": "json_schema"}}), wantErr: "response_format.type must be one of"},
		{name: "frequency penalty", body: testMiniMaxM3ImageBody(t, "user", []string{valid}, map[string]any{"frequency_penalty": 0}), wantErr: "incompatible with frequency_penalty"},
		{name: "n", body: testMiniMaxM3ImageBody(t, "user", []string{valid}, map[string]any{"n": 1}), wantErr: "incompatible with n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateChatExecutionProfileRequest(test.body, plan)
			require.ErrorContains(t, err, test.wantErr)
		})
	}

	historical := executionProfilePlan(t, "minimax.m3.token-plan", "1.3.0")
	_, err := validateChatExecutionProfileRequest(testMiniMaxM3ImageBody(t, "user", []string{valid}, nil), historical)
	require.ErrorContains(t, err, "image input is not supported")
}

func TestMiniMaxM3InlineJPEGCombinationRulesAreInvocationScoped(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m3.token-plan", "1.4.0")
	valid := testJPEGDataURL(t, 32, 32)
	imageDisabled := testMiniMaxM3ImageBody(t, "user", []string{valid}, map[string]any{"thinking": map[string]any{"type": "disabled"}})
	_, err := validateChatExecutionProfileRequest(imageDisabled, plan)
	require.NoError(t, err)

	plainStream := []byte(`{"model":"MiniMax-M3","messages":[{"role":"user","content":"hello"}],"stream":true,"thinking":{"type":"disabled"},"max_tokens":128}`)
	_, err = validateChatExecutionProfileRequest(plainStream, plan)
	require.NoError(t, err)
	plainTools := []byte(`{"model":"MiniMax-M3","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"max_tokens":128}`)
	_, err = validateChatExecutionProfileRequest(plainTools, plan)
	require.NoError(t, err)

	mutated := append(bytes.Clone(imageDisabled), ' ')
	evidence, err := validateChatExecutionProfileRequest(imageDisabled, plan)
	require.NoError(t, err)
	evidence.bindAdmissionBody(imageDisabled)
	_, err = estimateChatAdmissionForValidatedPlan(mutated, plan, evidence)
	require.ErrorContains(t, err, "exact profile admission snapshot is unavailable")
}
