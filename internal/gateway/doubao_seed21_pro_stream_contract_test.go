package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const doubaoSeed21ProStreamProtocol = "protocol.doubao-seed-2.1-pro-openai-chat.v2"

func TestDoubaoSeed21ProStreamingCandidateRequiresTerminalUsageRequest(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-pro.api", "1.2.0")
	tests := []struct {
		name              string
		body              string
		wantNormalization string
		wantErr           string
	}{
		{
			name:              "omitted stream options are normalized",
			body:              `{"model":"doubao-seed-2-1-pro-260628","messages":[],"stream":true}`,
			wantNormalization: "stream_options.include_usage=omitted->true",
		},
		{
			name: "explicit terminal usage is retained",
			body: `{"model":"doubao-seed-2-1-pro-260628","messages":[],"stream":true,"stream_options":{"include_usage":true}}`,
		},
		{
			name:    "explicitly disabling terminal usage is rejected",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"stream":true,"stream_options":{"include_usage":false}}`,
			wantErr: "include_usage=false is not supported",
		},
		{
			name:    "cumulative chunk usage is deferred",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"stream":true,"stream_options":{"chunk_include_usage":true}}`,
			wantErr: `chunk_include_usage" has no bounded forwarding contract`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, doubaoSeed21ProStreamProtocol, evidence.ModelProtocolProfile)
			require.Equal(t, test.wantNormalization, strings.Join(evidence.ProviderRequestNormalizations, ","))
			require.Equal(t, "omitted", evidence.ReasoningToggleRequest)
			require.Equal(t, "enabled", evidence.ReasoningProviderDefault)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}

	_, err := validateChatExecutionProfileRequest(
		[]byte(`{"model":"doubao-seed-2-1-pro-260628","messages":[],"stream":false}`), plan,
	)
	require.NoError(t, err, "streaming candidate must retain the historical non-stream core")
}

func TestDoubaoSeed21ProStreamingCandidateNormalizesProviderPayloadAndPlanOnce(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &plan, "doubao.seed-2.1-pro.api", "1.2.0")
	plan.Routes[0].Provider = "doubao"
	plan.Routes[0].UpstreamModel = "doubao-seed-2-1-pro-260628"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	planner := &executionPlannerStub{plan: plan}
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true,"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-seed21-pro-stream",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []string{modelprofile.CapabilityReasoning, modelprofile.CapabilityStreaming}, batch.plan.RequiredCapabilities)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityStreaming, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	require.Equal(t, []string{"stream_options.include_usage=omitted->true"}, batch.profileEvidence.ProviderRequestNormalizations)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"hello"}],"stream":true,"stream_options":{"include_usage":true},"max_tokens":128}`, string(actual))
}

func TestDoubaoSeed21ProExactStreamFramesEnforceReasoningPhasesAndTerminalUsage(t *testing.T) {
	state := exactStreamStateForProtocol(doubaoSeed21ProStreamProtocol, "")
	require.NotNil(t, state)
	frames := []string{
		`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"summary"},"finish_reason":null}]}`,
		`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"","reasoning_content":"","encrypted_content":"opaque-block"},"finish_reason":null}]}`,
		`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}]}`,
		`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":13,"completion_tokens":4,"total_tokens":17,"completion_tokens_details":{"reasoning_tokens":2}}}`,
	}
	for _, raw := range frames {
		frame, err := parseGuardedStreamFrameForProtocol([]byte(raw), doubaoSeed21ProStreamProtocol)
		require.NoError(t, err)
		require.False(t, frame.unsupported, raw)
		require.NoError(t, state.accept(frame, frame.terminalUsage), raw)
	}
	require.NoError(t, state.validateDone())

	for _, raw := range []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[]},"finish_reason":"tool_calls"}]}`,
		`{"service_tier":"default","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
	} {
		frame, err := parseGuardedStreamFrameForProtocol([]byte(raw), doubaoSeed21ProStreamProtocol)
		require.NoError(t, err)
		require.True(t, frame.unsupported, raw)
	}

	missingUsage := exactStreamStateForProtocol(doubaoSeed21ProStreamProtocol, "")
	finished, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`),
		doubaoSeed21ProStreamProtocol,
	)
	require.NoError(t, err)
	require.NoError(t, missingUsage.accept(finished, false))
	require.ErrorContains(t, missingUsage.validateDone(), "terminal usage")
}

func TestDoubaoSeed21ProRelayStreamPreservesOpaqueEvidenceAndUsesFinalUsage(t *testing.T) {
	var settled billing.SettleRequest
	b := &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
		settled = req
		return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
	}}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"summary\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\",\"reasoning_content\":\"\",\"encrypted_content\":\"opaque-block\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":13,\"completion_tokens\":4,\"total_tokens\":17,\"completion_tokens_details\":{\"reasoning_tokens\":2}}}\n\n" +
		"data: [DONE]\n\n"
	recorder := httptest.NewRecorder()
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{
		Channel:              models.Channel{ID: 1, Name: "Doubao Seed 2.1 Pro", Provider: "doubao"},
		ModelProtocolProfile: doubaoSeed21ProStreamProtocol,
	}
	requestID := "req-doubao-seed21-pro-exact-stream"
	relay.relayStreamRequest(p6f11StreamContext(nil, requestID), recorder, response, route,
		requestID, 1, 2, "doubao-seed-2-1-pro-260628", "doubao-seed-2-1-pro-260628", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 1_000, nil, "")

	body := recorder.Body.String()
	require.Contains(t, body, `"reasoning_content":"summary"`)
	require.Contains(t, body, `"encrypted_content":"opaque-block"`)
	require.Contains(t, body, `"content":"answer"`)
	require.NotContains(t, body, "stream_error.invalid_frame")
	require.Equal(t, 1, strings.Count(body, "data: [DONE]"))
	require.Equal(t, 13, settled.PromptTokens)
	require.Equal(t, 4, settled.CompletionTokens)
	require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)
}
