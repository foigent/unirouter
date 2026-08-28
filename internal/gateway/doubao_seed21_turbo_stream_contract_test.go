package gateway

import (
	"context"
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

const doubaoSeed21TurboStreamProtocol = "protocol.doubao-seed-2.1-turbo-openai-chat.v2"

func TestDoubaoSeed21TurboStreamingCandidateKeepsEffortSemanticsAndRequiresTerminalUsage(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.2.0")
	tests := []struct {
		name, body, wantRequest, wantEffective, wantNormalization, wantErr string
	}{
		{
			name:        "provider default high",
			body:        `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"stream":true}`,
			wantRequest: "omitted", wantEffective: "omitted",
			wantNormalization: "stream_options.include_usage=omitted->true",
		},
		{
			name:        "explicit minimal remains disabled",
			body:        `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"stream":true,"stream_options":{"include_usage":true},"reasoning_effort":"minimal"}`,
			wantRequest: "minimal", wantEffective: "disabled",
		},
		{
			name:        "explicit high",
			body:        `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"stream":true,"reasoning_effort":"high"}`,
			wantRequest: "high", wantEffective: "high",
			wantNormalization: "stream_options.include_usage=omitted->true",
		},
		{
			name:    "terminal usage disabled",
			body:    `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"stream":true,"stream_options":{"include_usage":false}}`,
			wantErr: "include_usage=false is not supported",
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
			require.Equal(t, doubaoSeed21TurboStreamProtocol, evidence.ModelProtocolProfile)
			require.Equal(t, test.wantRequest, evidence.ReasoningEffortRequest)
			require.Equal(t, test.wantEffective, evidence.ReasoningEffortEffective)
			require.Equal(t, "high", evidence.ReasoningProviderDefault)
			require.Equal(t, test.wantNormalization, strings.Join(evidence.ProviderRequestNormalizations, ","))
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestDoubaoSeed21TurboStreamingCandidateUsesOnePlanAndProviderPayload(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &plan, "doubao.seed-2.1-turbo.api", "1.2.0")
	plan.Routes[0].Provider = "doubao"
	plan.Routes[0].UpstreamModel = "doubao-seed-2-1-turbo-260628"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	planner := &executionPlannerStub{plan: plan}
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true,"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-seed21-turbo-stream",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []string{modelprofile.CapabilityReasoningEffort, modelprofile.CapabilityStreaming}, batch.plan.RequiredCapabilities)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityStreaming, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"user","content":"hello"}],"stream":true,"stream_options":{"include_usage":true},"max_tokens":128}`, string(actual))
}

func TestDoubaoSeed21TurboExactStreamContractUsesSharedArkTerminalState(t *testing.T) {
	state := exactStreamStateForProtocol(doubaoSeed21TurboStreamProtocol)
	require.NotNil(t, state)
	for _, raw := range []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"summary"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"content":"","reasoning_content":"","encrypted_content":"opaque"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11,"completion_tokens_details":{"reasoning_tokens":1}}}`,
	} {
		frame, err := parseGuardedStreamFrameForProtocol([]byte(raw), doubaoSeed21TurboStreamProtocol)
		require.NoError(t, err)
		require.False(t, frame.unsupported, raw)
		require.NoError(t, state.accept(frame, frame.terminalUsage), raw)
	}
	require.NoError(t, state.validateDone())

	missingUsage := exactStreamStateForProtocol(doubaoSeed21TurboStreamProtocol)
	finished, err := parseGuardedStreamFrameForProtocol(
		[]byte(`{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`),
		doubaoSeed21TurboStreamProtocol,
	)
	require.NoError(t, err)
	require.NoError(t, missingUsage.accept(finished, false))
	require.ErrorContains(t, missingUsage.validateDone(), "terminal usage")
}
