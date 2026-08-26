package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestDeepSeekV4FlashToolHistoryRequiresReasoningForEveryAssistantWhenToolsAreCarried(t *testing.T) {
	plan := executionProfilePlan(t, "deepseek.v4-flash.api", "1.4.0")
	tool := `{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}`
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "first tool-enabled turn has no assistant history",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
		},
		{
			name: "non-calling assistant reasoning is preserved",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi","reasoning_content":"greeting"},{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
		},
		{
			name: "tool-calling assistant reasoning is preserved",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"},{"role":"assistant","content":"","reasoning_content":"need lookup","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"},{"role":"user","content":"continue"}],"tools":[` + tool + `]}`,
		},
		{
			name:    "plain assistant without reasoning is rejected",
			body:    `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
			wantErr: "assistant history message 1 must preserve reasoning_content when function tools are requested",
		},
		{
			name:    "empty assistant reasoning is rejected",
			body:    `{"model":"deepseek-v4-flash","messages":[{"role":"assistant","content":"hi","reasoning_content":" "},{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
			wantErr: "assistant history message 0 must preserve reasoning_content when function tools are requested",
		},
		{
			name: "ordinary multi-turn without tools does not require replay",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"again"}]}`,
		},
		{
			name:    "no-tools history still rejects non-string reasoning",
			body:    `{"model":"deepseek-v4-flash","messages":[{"role":"assistant","content":"hi","reasoning_content":{}},{"role":"user","content":"again"}]}`,
			wantErr: "message 0 assistant reasoning_content must be a string or null",
		},
		{
			name: "disabled thinking with tools does not manufacture reasoning",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"weather"}],"tools":[` + tool + `],"thinking":{"type":"disabled"}}`,
		},
		{
			name:    "disabled thinking still rejects non-string reasoning",
			body:    `{"model":"deepseek-v4-flash","messages":[{"role":"assistant","content":"hi","reasoning_content":{}},{"role":"user","content":"weather"}],"tools":[` + tool + `],"thinking":{"type":"disabled"}}`,
			wantErr: "message 0 assistant reasoning_content must be a string or null",
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
			require.NoError(t, func() error { _, err := estimateChatAdmissionForPlan([]byte(test.body), plan); return err }())
			require.NotEmpty(t, evidence.AdmissionShadowBody)
		})
	}
}

func TestDeepSeekV4FlashToolHistoryPreservesReasoningInProviderPayloadAndOnePlan(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", encKey, nil)
	setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.4.0")
	plan.Routes[0].Provider = "deepseek"
	plan.Routes[0].UpstreamModel = "deepseek-v4-flash"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi","reasoning_content":"greeting"},{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-deepseek-flash-history",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	payloadBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payloadBody, &payload))
	require.Contains(t, string(payload["messages"]), `"reasoning_content":"greeting"`)
}
