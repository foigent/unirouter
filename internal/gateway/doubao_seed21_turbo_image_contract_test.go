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

const doubaoSeed21TurboInlineJPEGProtocol = "protocol.doubao-seed-2.1-turbo-openai-chat.v5"

func testDoubaoTurboImageBody(t *testing.T, role string, images []string, extra map[string]any) []byte {
	t.Helper()
	body := testDoubaoImageBody(t, role, images, extra)
	return bytes.ReplaceAll(body, []byte("doubao-seed-evolving"), []byte("doubao-seed-2-1-turbo-260628"))
}

func TestDoubaoSeed21TurboInlineJPEGCurrentIsExactPlanBoundAndPreserved(t *testing.T) {
	dataURL := testJPEGDataURL(t, 32, 32)
	body := testDoubaoTurboImageBody(t, "user", []string{dataURL}, nil)
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.6.0")
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.True(t, evidence.ImageRequested)
	require.Equal(t, int64(32*32*4), evidence.ImageAdmissionTokenUpper)
	require.Equal(t, "chat-inline-jpeg-rgba-pixel-upper.v1", evidence.ImageAdmissionEstimateVersion)
	require.NotContains(t, string(evidence.AdmissionShadowBody), dataURL)

	evidence.bindAdmissionBody(body)
	estimate, err := estimateChatAdmissionForValidatedPlan(body, plan, evidence)
	require.NoError(t, err)
	require.GreaterOrEqual(t, estimate.InputTokens, 32*32*4)
	require.Equal(t, "chat-inline-jpeg-rgba-pixel-upper.v1+"+chatAdmissionEstimateVersion, estimate.Version)
	_, err = estimateChatAdmissionForValidatedPlan(append(bytes.Clone(body), ' '), plan, evidence)
	require.ErrorContains(t, err, "exact profile admission snapshot is unavailable")

	profile, ok := modelprofile.Lookup("doubao.seed-2.1-turbo.api", "1.6.0")
	require.True(t, ok)
	require.True(t, profile.Current)
	require.Equal(t, doubaoSeed21TurboInlineJPEGProtocol, profile.ProtocolProfile)
	requirements, err := invocationCapabilityRequirementsForChat(profile, evidence, body)
	require.NoError(t, err)
	require.Contains(t, requirements, modelrouting.CapabilityRequirement{
		CapabilityID: modelprofile.CapabilityVision,
		Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
	})

	encKey := []byte("0123456789abcdef0123456789abcdef")
	executionPlan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &executionPlan, "doubao.seed-2.1-turbo.api", "1.6.0")
	executionPlan.Routes[0].Provider = "doubao"
	executionPlan.Routes[0].UpstreamModel = "doubao-seed-2-1-turbo-260628"
	planner := &executionPlannerStub{plan: executionPlan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-turbo-inline-jpeg",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityCacheReadUsage, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityPromptCache, Origins: []string{modelrouting.CapabilityOriginProfileAlwaysRequired}},
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityVision, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Equal(t, "chat-inline-jpeg-rgba-pixel-upper.v1+"+chatAdmissionEstimateVersion, batch.limits.EstimateVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var want, got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &want))
	require.NoError(t, json.Unmarshal(actual, &got))
	require.Equal(t, want, got, "typed validation shadow must never replace provider image bytes")
}

func TestDoubaoSeed21TurboInlineJPEGCurrentRejectsUnprovenShapes(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.6.0")
	valid := testJPEGDataURL(t, 32, 32)
	tests := []struct {
		name    string
		body    []byte
		wantErr string
	}{
		{name: "remote URL", body: testDoubaoTurboImageBody(t, "user", []string{"https://example.com/image.jpg"}, nil), wantErr: "currently supports only inline data URLs"},
		{name: "system image", body: testDoubaoTurboImageBody(t, "system", []string{valid}, nil), wantErr: "not supported on system messages"},
		{name: "non JPEG payload", body: testDoubaoTurboImageBody(t, "user", []string{"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("not-a-jpeg"))}, nil), wantErr: "valid JPEG"},
		{name: "wrong MIME", body: testDoubaoTurboImageBody(t, "user", []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not-a-png"))}, nil), wantErr: "MIME type image/png"},
		{name: "stream", body: testDoubaoTurboImageBody(t, "user", []string{valid}, map[string]any{"stream": true}), wantErr: "requires stream=false"},
		{name: "tools", body: testDoubaoTurboImageBody(t, "user", []string{valid}, map[string]any{"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}}}}), wantErr: "incompatible with tools"},
		{name: "strict JSON", body: testDoubaoTurboImageBody(t, "user", []string{valid}, map[string]any{"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "answer", "strict": true, "schema": map[string]any{"type": "object"}}}}), wantErr: "supports only text response format"},
		{name: "frequency penalty", body: testDoubaoTurboImageBody(t, "user", []string{valid}, map[string]any{"frequency_penalty": 0}), wantErr: "incompatible with frequency_penalty"},
		{name: "presence penalty", body: testDoubaoTurboImageBody(t, "user", []string{valid}, map[string]any{"presence_penalty": 0}), wantErr: "incompatible with presence_penalty"},
		{name: "n", body: testDoubaoTurboImageBody(t, "user", []string{valid}, map[string]any{"n": 1}), wantErr: "incompatible with n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateChatExecutionProfileRequest(test.body, plan)
			require.ErrorContains(t, err, test.wantErr)
		})
	}

	nineImages := make([]string, 9)
	for index := range nineImages {
		nineImages[index] = valid
	}
	_, err := validateChatExecutionProfileRequest(testDoubaoTurboImageBody(t, "user", nineImages, nil), plan)
	require.ErrorContains(t, err, "image input count must be between 1 and 8")

	historical := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.5.0")
	_, err = validateChatExecutionProfileRequest(testDoubaoTurboImageBody(t, "user", []string{valid}, nil), historical)
	require.ErrorContains(t, err, "image input is not supported")
}

func TestDoubaoSeed21TurboInlineJPEGCombinationRulesAreInvocationScoped(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.6.0")
	plainStream := []byte(`{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"user","content":"hello"}],"stream":true,"stream_options":{"include_usage":true},"max_tokens":128}`)
	_, err := validateChatExecutionProfileRequest(plainStream, plan)
	require.NoError(t, err, "v5 must preserve the v4 plain streaming contract")
	plainTools := []byte(`{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"max_tokens":128}`)
	_, err = validateChatExecutionProfileRequest(plainTools, plan)
	require.NoError(t, err, "v5 must preserve the v4 non-stream function-tools contract")
	plainStrict := []byte(`{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"user","content":"json"}],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object"}}},"max_tokens":128}`)
	_, err = validateChatExecutionProfileRequest(plainStrict, plan)
	require.NoError(t, err, "v5 must preserve the v4 strict JSON contract without images")
}
