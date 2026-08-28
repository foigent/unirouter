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

const doubaoEvolvingStreamProtocol = "protocol.doubao-seed-evolving-openai-chat.v3"

func TestDoubaoSeedEvolvingStreamingCandidateRequiresTerminalUsageRequest(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.4.0")
	tests := []struct {
		name              string
		body              string
		wantNormalization string
		wantErr           string
	}{
		{
			name:              "omitted stream options are normalized",
			body:              `{"model":"doubao-seed-evolving","messages":[],"stream":true}`,
			wantNormalization: "stream_options.include_usage=omitted->true",
		},
		{
			name:              "null stream options are normalized",
			body:              `{"model":"doubao-seed-evolving","messages":[],"stream":true,"stream_options":null}`,
			wantNormalization: "stream_options.include_usage=omitted->true",
		},
		{
			name: "explicit terminal usage is retained",
			body: `{"model":"doubao-seed-evolving","messages":[],"stream":true,"stream_options":{"include_usage":true}}`,
		},
		{
			name:    "explicitly disabling terminal usage is rejected",
			body:    `{"model":"doubao-seed-evolving","messages":[],"stream":true,"stream_options":{"include_usage":false}}`,
			wantErr: "include_usage=false is not supported",
		},
		{
			name:    "obfuscation option is not modeled",
			body:    `{"model":"doubao-seed-evolving","messages":[],"stream":true,"stream_options":{"include_usage":true,"include_obfuscation":true}}`,
			wantErr: "include_obfuscation\" has no bounded forwarding contract",
		},
		{
			name:    "cumulative chunk usage is deferred",
			body:    `{"model":"doubao-seed-evolving","messages":[],"stream":true,"stream_options":{"chunk_include_usage":true}}`,
			wantErr: "chunk_include_usage\" has no bounded forwarding contract",
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
			require.Equal(t, doubaoEvolvingStreamProtocol, evidence.ModelProtocolProfile)
			require.Equal(t, test.wantNormalization, strings.Join(evidence.ProviderRequestNormalizations, ","))
			require.Zero(t, evidence.ReasoningTimeoutSeconds, "non-stream timeout must not become stream policy")
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}

	nonStream, err := validateChatExecutionProfileRequest(
		[]byte(`{"model":"doubao-seed-evolving","messages":[]}`), plan,
	)
	require.NoError(t, err)
	require.Equal(t, 1_800, nonStream.ReasoningTimeoutSeconds)
	require.Empty(t, nonStream.ProviderRequestNormalizations)
}

func TestDoubaoSeedEvolvingStreamingCandidateNormalizesProviderPayloadOnce(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &plan, "doubao.seed-evolving.api", "1.4.0")
	plan.Routes[0].Provider = "doubao"
	plan.Routes[0].UpstreamModel = "doubao-seed-evolving"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	planner := &executionPlannerStub{plan: plan}
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true,"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-evolving-stream",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []string{modelprofile.CapabilityReasoningEffort, modelprofile.CapabilityStreaming}, batch.plan.RequiredCapabilities)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityStreaming, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Zero(t, batch.profileEvidence.ReasoningTimeoutSeconds)
	require.Equal(t, []string{"stream_options.include_usage=omitted->true"}, batch.profileEvidence.ProviderRequestNormalizations)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"hello"}],"stream":true,"stream_options":{"include_usage":true},"max_tokens":128}`, string(actual))
}

func TestDoubaoSeedEvolvingExactStreamFramesEnforcePhasesAndTerminalUsage(t *testing.T) {
	state := exactStreamStateForProtocol(doubaoEvolvingStreamProtocol)
	require.NotNil(t, state)
	frames := []string{
		`{"id":"chunk","object":"chat.completion.chunk","created":1,"model":"doubao-seed-evolving","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"summary"},"finish_reason":null}]}`,
		`{"id":"chunk","object":"chat.completion.chunk","created":1,"model":"doubao-seed-evolving","choices":[{"index":0,"delta":{"content":"","reasoning_content":"","encrypted_content":"opaque-block"},"finish_reason":null}]}`,
		`{"id":"chunk","object":"chat.completion.chunk","created":1,"model":"doubao-seed-evolving","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}]}`,
		`{"id":"chunk","object":"chat.completion.chunk","created":1,"model":"doubao-seed-evolving","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"chunk","object":"chat.completion.chunk","created":1,"model":"doubao-seed-evolving","choices":[],"usage":{"prompt_tokens":13,"completion_tokens":4,"total_tokens":17,"completion_tokens_details":{"reasoning_tokens":2}}}`,
	}
	for _, raw := range frames {
		frame, err := parseGuardedStreamFrameForProtocol([]byte(raw), doubaoEvolvingStreamProtocol)
		require.NoError(t, err)
		require.False(t, frame.unsupported, raw)
		require.NoError(t, state.accept(frame, frame.terminalUsage), raw)
	}
	require.NoError(t, state.validateDone())

	withoutOpaque := exactStreamStateForProtocol(doubaoEvolvingStreamProtocol)
	for _, raw := range []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"summary"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
	} {
		frame, err := parseGuardedStreamFrameForProtocol([]byte(raw), doubaoEvolvingStreamProtocol)
		require.NoError(t, err)
		require.False(t, frame.unsupported)
		require.NoError(t, withoutOpaque.accept(frame, frame.terminalUsage))
	}
	require.NoError(t, withoutOpaque.validateDone(), "encrypted_content is optional evidence")

	tests := []struct {
		name, raw string
	}{
		{"reasoning alias", `{"choices":[{"index":0,"delta":{"reasoning":"hidden"},"finish_reason":null}]}`},
		{"function tool", `{"choices":[{"index":0,"delta":{"tool_calls":[]},"finish_reason":"tool_calls"}]}`},
		{"service tier", `{"service_tier":"default","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}`},
		{"service status", `{"service_status":{},"choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}`},
		{"empty opaque", `{"choices":[{"index":0,"delta":{"encrypted_content":""},"finish_reason":null}]}`},
		{"usage mixed with choice", `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`},
		{"unmodeled logprobs", `{"choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null,"logprobs":null}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frame, err := parseGuardedStreamFrameForProtocol([]byte(test.raw), doubaoEvolvingStreamProtocol)
			require.NoError(t, err)
			require.True(t, frame.unsupported)
		})
	}

	missingUsage := exactStreamStateForProtocol(doubaoEvolvingStreamProtocol)
	frame, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`),
		doubaoEvolvingStreamProtocol,
	)
	require.NoError(t, err)
	require.NoError(t, missingUsage.accept(frame, false))
	require.ErrorContains(t, missingUsage.validateDone(), "terminal usage")

	outOfOrder := exactStreamStateForProtocol(doubaoEvolvingStreamProtocol)
	require.NoError(t, outOfOrder.accept(frame, false))
	opaque, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[{"index":0,"delta":{"encrypted_content":"late"},"finish_reason":null}]}`),
		doubaoEvolvingStreamProtocol,
	)
	require.NoError(t, err)
	require.ErrorContains(t, outOfOrder.accept(opaque, false), "encrypted_content phase")

	duplicateUsage := exactStreamStateForProtocol(doubaoEvolvingStreamProtocol)
	terminalUsage, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`),
		doubaoEvolvingStreamProtocol,
	)
	require.NoError(t, err)
	require.NoError(t, duplicateUsage.accept(terminalUsage, true))
	require.ErrorContains(t, duplicateUsage.accept(terminalUsage, true), "more than once")
	require.ErrorContains(t, duplicateUsage.accept(frame, false), "after terminal usage")

	emptyUsage, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[],"usage":{}}`),
		doubaoEvolvingStreamProtocol,
	)
	require.NoError(t, err)
	require.True(t, emptyUsage.terminalUsage)
	require.ErrorContains(t, exactStreamStateForProtocol(doubaoEvolvingStreamProtocol).accept(emptyUsage, false), "provider usage contract")

	duplicateOpaque := exactStreamStateForProtocol(doubaoEvolvingStreamProtocol)
	require.NoError(t, duplicateOpaque.accept(opaque, false))
	require.ErrorContains(t, duplicateOpaque.accept(opaque, false), "encrypted_content phase")

	// Existing protocol profiles with a zero-value exact stream contract retain
	// the historical generic parser rather than inheriting Evolving semantics.
	historical, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"service_tier":"default","choices":[{"index":0,"delta":{"reasoning":"legacy"},"finish_reason":"tool_calls"}]}`),
		"protocol.kimi-openai-chat.v3",
	)
	require.NoError(t, err)
	require.False(t, historical.unsupported)
}

func TestDoubaoSeedEvolvingRelayStreamPreservesOpaqueEvidenceAndUsesFinalUsage(t *testing.T) {
	var settled billing.SettleRequest
	b := &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
		settled = req
		return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
	}}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"summary\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\",\"reasoning_content\":\"\",\"encrypted_content\":\"opaque-block\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":13,\"completion_tokens\":4,\"total_tokens\":17,\"completion_tokens_details\":{\"reasoning_tokens\":2}}}\n\n" +
		"data: [DONE]\n\n"
	recorder := httptest.NewRecorder()
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{
		Channel:              models.Channel{ID: 1, Name: "Doubao Seed Evolving", Provider: "doubao"},
		ModelProtocolProfile: doubaoEvolvingStreamProtocol,
	}
	requestID := "req-doubao-evolving-exact-stream"
	relay.relayStreamRequest(p6f11StreamContext(nil, requestID), recorder, response, route,
		requestID, 1, 2, "doubao-seed-evolving", "doubao-seed-evolving", "127.0.0.1", nil,
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

	missingUsageRecorder := httptest.NewRecorder()
	missingUsageResponse := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
	))}
	missingUsageID := "req-doubao-evolving-missing-usage"
	relay.relayStreamRequest(p6f11StreamContext(nil, missingUsageID), missingUsageRecorder, missingUsageResponse, route,
		missingUsageID, 1, 2, "doubao-seed-evolving", "doubao-seed-evolving", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: missingUsageID, OrgID: 1}), time.Now(), 1_000, nil, "")
	require.Contains(t, missingUsageRecorder.Body.String(), "stream_error.invalid_frame")
	require.NotContains(t, missingUsageRecorder.Body.String(), "data: [DONE]")

	missingDoneRecorder := httptest.NewRecorder()
	missingDoneResponse := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n",
	))}
	missingDoneID := "req-doubao-evolving-missing-done"
	relay.relayStreamRequest(p6f11StreamContext(nil, missingDoneID), missingDoneRecorder, missingDoneResponse, route,
		missingDoneID, 1, 2, "doubao-seed-evolving", "doubao-seed-evolving", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: missingDoneID, OrgID: 1}), time.Now(), 1_000, nil, "")
	require.Contains(t, missingDoneRecorder.Body.String(), "stream_error.invalid_frame")
	require.NotContains(t, missingDoneRecorder.Body.String(), "data: [DONE]")

	var emptyUsageSettlement billing.SettleRequest
	emptyUsageSettled := false
	emptyUsageBilling := &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
		emptyUsageSettled = true
		emptyUsageSettlement = req
		return &billing.SettleResult{SettleID: 2, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
	}}
	emptyUsageRelay := newTestRelay(p6f11RelayDB(), emptyUsageBilling)
	emptyUsageRecorder := httptest.NewRecorder()
	emptyUsageResponse := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: {\"choices\":[],\"usage\":{}}\n\n" +
			"data: [DONE]\n\n",
	))}
	emptyUsageID := "req-doubao-evolving-empty-usage"
	emptyUsageRelay.relayStreamRequest(p6f11StreamContext(nil, emptyUsageID), emptyUsageRecorder, emptyUsageResponse, route,
		emptyUsageID, 1, 2, "doubao-seed-evolving", "doubao-seed-evolving", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: emptyUsageID, OrgID: 1}), time.Now(), 1_000, nil, "")
	require.Contains(t, emptyUsageRecorder.Body.String(), "stream_error.invalid_frame")
	require.NotContains(t, emptyUsageRecorder.Body.String(), "data: [DONE]")
	require.True(t, emptyUsageSettled)
	require.Equal(t, metering.UsageSourceCounterV2, emptyUsageSettlement.UsageSource)
}
