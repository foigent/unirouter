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

const deepSeekV4FunctionTool = `{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}`

func TestDeepSeekV4CurrentProfilesRejectExplicitToolChoiceWhileReasoningIsActive(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "omitted provider default auto remains valid",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + deepSeekV4FunctionTool + `]}`,
		},
		{
			name: "redundant explicit auto is normalized to provider default",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + deepSeekV4FunctionTool + `],"tool_choice":"auto"}`,
		},
		{
			name:    "provider default reasoning rejects required",
			body:    `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + deepSeekV4FunctionTool + `],"tool_choice":"required"}`,
			wantErr: "tool_choice is not supported while reasoning is active",
		},
		{
			name:    "explicit reasoning rejects named",
			body:    `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + deepSeekV4FunctionTool + `],"tool_choice":{"type":"function","function":{"name":"lookup"}},"thinking":{"type":"enabled"}}`,
			wantErr: "tool_choice is not supported while reasoning is active",
		},
		{
			name:    "active reasoning rejects explicit none",
			body:    `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}],"tool_choice":"none"}`,
			wantErr: "tool_choice is not supported while reasoning is active",
		},
		{
			name: "disabled reasoning accepts required",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + deepSeekV4FunctionTool + `],"tool_choice":"required","thinking":{"type":"disabled"}}`,
		},
		{
			name: "disabled reasoning accepts named",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + deepSeekV4FunctionTool + `],"tool_choice":{"type":"function","function":{"name":"lookup"}},"thinking":{"type":"disabled"}}`,
		},
	}

	for _, profileID := range []string{"deepseek.v4-flash.api", "deepseek.v4-pro.api"} {
		t.Run(profileID, func(t *testing.T) {
			plan := executionProfilePlan(t, profileID, "1.5.0")
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
					if test.wantErr != "" {
						require.ErrorContains(t, err, test.wantErr)
						return
					}
					require.NoError(t, err)
					require.NoError(t, func() error {
						_, admissionErr := estimateChatAdmissionForPlan([]byte(test.body), plan)
						return admissionErr
					}())
					if test.name == "redundant explicit auto is normalized to provider default" {
						require.Equal(t, []string{"tool_choice=auto->omitted"}, evidence.ProviderRequestNormalizations)
					}
				})
			}
		})
	}
}

func TestDeepSeekV4FlashCurrentProfileNormalizesOnlyRedundantAutoToolChoiceOnProviderWire(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", encKey, nil)
	setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.5.0")
	plan.Routes[0].Provider = "deepseek"
	plan.Routes[0].UpstreamModel = "deepseek-v4-flash"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[` + deepSeekV4FunctionTool + `],"tool_choice":"auto","max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-deepseek-flash-auto-normalization",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Len(t, batch.routes, 1)

	providerBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(providerBody, &payload))
	require.NotContains(t, payload, "tool_choice")
	require.Contains(t, payload, "tools")
	require.NotContains(t, payload, "thinking", "omitted provider reasoning control must remain omitted")
}

func TestDeepSeekV4FlashCurrentProfileFailsClosedDuringPrepareBeforeInvalidToolChoiceCanDispatch(t *testing.T) {
	for _, choice := range []string{
		`"required"`,
		`{"type":"function","function":{"name":"lookup"}}`,
	} {
		encKey := []byte("0123456789abcdef0123456789abcdef")
		plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", encKey, nil)
		setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.5.0")
		plan.Routes[0].Provider = "deepseek"
		plan.Routes[0].UpstreamModel = "deepseek-v4-flash"
		planner := &executionPlannerStub{plan: plan}
		relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
		require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

		explicitOutput := 128
		body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[` + deepSeekV4FunctionTool + `],"tool_choice":` + choice + `,"max_tokens":128}`)
		batch, err := relay.prepareModelExecutionRoutes(
			context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
			"public-model", "public-model", "req-deepseek-flash-active-tool-choice-rejected",
			modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
			body, time.Now(),
		)
		require.ErrorContains(t, err, "tool_choice is not supported while reasoning is active")
		require.Nil(t, batch)
	}
}

func TestDeepSeekV4FlashCurrentProfilePreservesNamedChoiceOnlyWhenReasoningDisabled(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", encKey, nil)
	setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.5.0")
	plan.Routes[0].Provider = "deepseek"
	plan.Routes[0].UpstreamModel = "deepseek-v4-flash"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[` + deepSeekV4FunctionTool + `],"tool_choice":{"type":"function","function":{"name":"lookup"}},"thinking":{"type":"disabled"},"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-deepseek-flash-disabled-named",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Len(t, batch.routes, 1)

	providerBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(providerBody, &payload))
	require.JSONEq(t, `{"type":"function","function":{"name":"lookup"}}`, string(payload["tool_choice"]))
	require.JSONEq(t, `{"type":"disabled"}`, string(payload["thinking"]))
}
