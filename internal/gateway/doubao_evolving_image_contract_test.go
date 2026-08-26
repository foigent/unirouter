package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const doubaoEvolvingInlineJPEGProtocol = "protocol.doubao-seed-evolving-openai-chat.v6"

func testJPEGDataURL(t *testing.T, width, height int) string {
	t.Helper()
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, width, height)), &jpeg.Options{Quality: 80}))
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
}

func testDoubaoImageBody(t *testing.T, role string, images []string, extra map[string]any) []byte {
	t.Helper()
	parts := []any{map[string]any{"type": "text", "text": "describe the image"}}
	for _, dataURL := range images {
		parts = append(parts, map[string]any{
			"type": "image_url", "image_url": map[string]any{"url": dataURL},
		})
	}
	envelope := map[string]any{
		"model":      "doubao-seed-evolving",
		"messages":   []any{map[string]any{"role": role, "content": parts}},
		"max_tokens": 128,
	}
	for key, value := range extra {
		envelope[key] = value
	}
	body, err := json.Marshal(envelope)
	require.NoError(t, err)
	return body
}

func TestDoubaoSeedEvolvingHistoricalInlineJPEGIsExactPlanBoundAndPreserved(t *testing.T) {
	dataURL := testJPEGDataURL(t, 32, 32)
	body := testDoubaoImageBody(t, "user", []string{dataURL}, nil)
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.7.0")
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

	profile, ok := modelprofile.Lookup("doubao.seed-evolving.api", "1.7.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, doubaoEvolvingInlineJPEGProtocol, profile.ProtocolProfile)
	requirements, err := invocationCapabilityRequirementsForChat(profile, evidence, body)
	require.NoError(t, err)
	require.Contains(t, requirements, modelrouting.CapabilityRequirement{
		CapabilityID: modelprofile.CapabilityVision,
		Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
	})

	encKey := []byte("0123456789abcdef0123456789abcdef")
	executionPlan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &executionPlan, "doubao.seed-evolving.api", "1.7.0")
	executionPlan.Routes[0].Provider = "doubao"
	executionPlan.Routes[0].UpstreamModel = "doubao-seed-evolving"
	planner := &executionPlannerStub{plan: executionPlan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-evolving-inline-jpeg",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
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

func TestDoubaoSeedEvolvingInlineJPEGRejectsUnprovenShapes(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.7.0")
	valid := testJPEGDataURL(t, 32, 32)
	tooLargeDecoded := valid + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0}, 256*1024))
	const dataURLPrefix = "data:image/jpeg;base64,"
	validDecoded, err := base64.StdEncoding.DecodeString(valid[len(dataURLPrefix):])
	require.NoError(t, err)
	truncated := dataURLPrefix + base64.StdEncoding.EncodeToString(validDecoded[:len(validDecoded)-8])
	tests := []struct {
		name    string
		body    []byte
		wantErr string
	}{
		{name: "remote URL", body: testDoubaoImageBody(t, "user", []string{"https://example.com/image.jpg"}, nil), wantErr: "currently supports only inline data URLs"},
		{name: "system image", body: testDoubaoImageBody(t, "system", []string{valid}, nil), wantErr: "not supported on system messages"},
		{name: "non JPEG payload", body: testDoubaoImageBody(t, "user", []string{"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("not-a-jpeg"))}, nil), wantErr: "valid JPEG"},
		{name: "wrong MIME", body: testDoubaoImageBody(t, "user", []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not-a-png"))}, nil), wantErr: "MIME type image/png"},
		{name: "truncated JPEG", body: testDoubaoImageBody(t, "user", []string{truncated}, nil), wantErr: "complete JPEG"},
		{name: "decoded byte ceiling", body: testDoubaoImageBody(t, "user", []string{tooLargeDecoded}, nil), wantErr: "decoded bytes must not exceed"},
		{name: "minimum dimensions", body: testDoubaoImageBody(t, "user", []string{testJPEGDataURL(t, 14, 15)}, nil), wantErr: "at least 15x15"},
		{name: "pixel ceiling", body: testDoubaoImageBody(t, "user", []string{testJPEGDataURL(t, 512, 512)}, nil), wantErr: "image pixels must be between"},
		{name: "aspect ceiling", body: testDoubaoImageBody(t, "user", []string{testJPEGDataURL(t, 2251, 15)}, nil), wantErr: "aspect ratio"},
		{name: "stream", body: testDoubaoImageBody(t, "user", []string{valid}, map[string]any{"stream": true}), wantErr: "requires stream=false"},
		{name: "tools", body: testDoubaoImageBody(t, "user", []string{valid}, map[string]any{"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}}}}), wantErr: "incompatible with tools"},
		{name: "strict JSON", body: testDoubaoImageBody(t, "user", []string{valid}, map[string]any{"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "answer", "strict": true, "schema": map[string]any{"type": "object"}}}}), wantErr: "supports only text response format"},
		{name: "frequency penalty", body: testDoubaoImageBody(t, "user", []string{valid}, map[string]any{"frequency_penalty": 0}), wantErr: "incompatible with frequency_penalty"},
		{name: "presence penalty", body: testDoubaoImageBody(t, "user", []string{valid}, map[string]any{"presence_penalty": 0}), wantErr: "incompatible with presence_penalty"},
		{name: "n", body: testDoubaoImageBody(t, "user", []string{valid}, map[string]any{"n": 1}), wantErr: "incompatible with n"},
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
	_, err = validateChatExecutionProfileRequest(testDoubaoImageBody(t, "user", nineImages, nil), plan)
	require.ErrorContains(t, err, "image input count must be between 1 and 8")

	stringShorthand := []byte(`{"model":"doubao-seed-evolving","messages":[{"role":"user","content":[{"type":"image_url","image_url":` + string(mustJSON(t, valid)) + `}]}],"max_tokens":128}`)
	_, err = validateChatExecutionProfileRequest(stringShorthand, plan)
	require.ErrorContains(t, err, "must use an object with a url field")

	detail := []byte(`{"model":"doubao-seed-evolving","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":` + string(mustJSON(t, valid)) + `,"detail":"low"}}]}],"max_tokens":128}`)
	_, err = validateChatExecutionProfileRequest(detail, plan)
	require.ErrorContains(t, err, `image_url field "detail" has no bounded forwarding contract`)

	historical := executionProfilePlan(t, "doubao.seed-evolving.api", "1.6.0")
	_, err = validateChatExecutionProfileRequest(testDoubaoImageBody(t, "user", []string{valid}, nil), historical)
	require.ErrorContains(t, err, "image input is not supported")
}

func TestDoubaoSeedEvolvingInlineJPEGCombinationRulesAreInvocationScoped(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.7.0")
	plainStream := []byte(`{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"hello"}],"stream":true,"stream_options":{"include_usage":true},"max_tokens":128}`)
	_, err := validateChatExecutionProfileRequest(plainStream, plan)
	require.NoError(t, err, "v6 must preserve the v5 plain streaming contract")
	plainTools := []byte(`{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"max_tokens":128}`)
	_, err = validateChatExecutionProfileRequest(plainTools, plan)
	require.NoError(t, err, "v6 must preserve the v5 non-stream function-tools contract")
	plainStrict := []byte(`{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"json"}],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object"}}},"max_tokens":128}`)
	_, err = validateChatExecutionProfileRequest(plainStrict, plan)
	require.NoError(t, err, "v6 must preserve the v5 strict JSON contract without images")

	twoImages := testDoubaoImageBody(t, "user", []string{testJPEGDataURL(t, 32, 32), testJPEGDataURL(t, 32, 32)}, map[string]any{"response_format": map[string]any{"type": "text"}})
	evidence, err := validateChatExecutionProfileRequest(twoImages, plan)
	require.NoError(t, err)
	require.Equal(t, int64(2*32*32*4), evidence.ImageAdmissionTokenUpper)
	require.NoError(t, func() error { _, err := estimateChatPreProfileAdmission(twoImages); return err }())
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}
