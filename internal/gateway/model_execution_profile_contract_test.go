package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

type processingTierReadinessStub struct {
	result billing.AdmissionReadiness
	err    error
	calls  int
}

func (stub *processingTierReadinessStub) InspectAdmissionReadiness(context.Context, int64, string) (billing.AdmissionReadiness, error) {
	stub.calls++
	return stub.result, stub.err
}

func executionProfilePlan(t *testing.T, id, version string) modelrouting.Plan {
	t.Helper()
	plan := modelrouting.Plan{}
	setExecutionTestProfile(t, &plan, id, version)
	return plan
}

func TestKimiK3ReasoningProfileValidatesEffortToolChoiceAndPreservedThinking(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k3.api", "1.1.0")
	tests := []struct {
		name        string
		body        string
		wantRequest string
		wantErr     string
	}{
		{
			name:        "provider default remains omitted",
			body:        `{"model":"kimi-k3","messages":[{"role":"user","content":"hello"}]}`,
			wantRequest: "omitted",
		},
		{
			name:        "explicit low",
			body:        `{"model":"kimi-k3","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"low"}`,
			wantRequest: "low",
		},
		{
			name:        "required tool choice is supported",
			body:        `{"model":"kimi-k3","messages":[{"role":"user","content":"hello"}],"tool_choice":"required"}`,
			wantRequest: "omitted",
		},
		{
			name:        "preserved assistant reasoning",
			body:        `{"model":"kimi-k3","messages":[{"role":"assistant","content":null,"reasoning_content":"checked","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`,
			wantRequest: "omitted",
		},
		{
			name:    "unknown effort",
			body:    `{"model":"kimi-k3","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"medium"}`,
			wantErr: "low, high, max",
		},
		{
			name:    "named tool choice",
			body:    `{"model":"kimi-k3","messages":[{"role":"user","content":"hello"}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
			wantErr: "named tool_choice is incompatible",
		},
		{
			name:    "K2 thinking field",
			body:    `{"model":"kimi-k3","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled"}}`,
			wantErr: "does not accept thinking",
		},
		{
			name:    "missing preserved reasoning",
			body:    `{"model":"kimi-k3","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}]}`,
			wantErr: "must preserve reasoning_content",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error=%v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if evidence.ReasoningRequest != test.wantRequest || evidence.ReasoningMode != "always_on" ||
				evidence.ReasoningProviderDefault != "max" || evidence.ReasoningDefaultAuthority != "provider" {
				t.Fatalf("unexpected evidence: %+v", evidence)
			}
		})
	}
}

func TestDeepSeekReasoningProfileValidatesBothControlsAndNoOpCombinations(t *testing.T) {
	plan := executionProfilePlan(t, "deepseek.v4-pro.api", "1.0.0")
	tests := []struct {
		name              string
		body              string
		wantToggle        string
		wantEffort        string
		wantNormalization string
		wantErr           string
	}{
		{
			name:       "provider defaults remain omitted",
			body:       `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}`,
			wantToggle: "omitted", wantEffort: "omitted",
		},
		{
			name:       "explicit enabled max",
			body:       `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled"},"reasoning_effort":"max"}`,
			wantToggle: "enabled", wantEffort: "max",
		},
		{
			name:       "disabled without effort",
			body:       `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled"}}`,
			wantToggle: "disabled", wantEffort: "omitted",
		},
		{
			name:    "unsupported compatibility effort remains fail closed",
			body:    `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"medium"}`,
			wantErr: "reasoning_effort must be one of high, max",
		},
		{
			name:    "effort cannot accompany disabled thinking",
			body:    `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled"},"reasoning_effort":"high"}`,
			wantErr: "requires active reasoning",
		},
		{
			name:    "ignored sampling controls are rejected",
			body:    `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"temperature":0.7}`,
			wantErr: "temperature is ineffective while reasoning is active",
		},
		{
			name:       "redundant auto tool choice with tools is normalized",
			body:       `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{}}}}],"tool_choice":"auto"}`,
			wantToggle: "omitted", wantEffort: "omitted", wantNormalization: "tool_choice=auto->omitted",
		},
		{
			name:    "auto tool choice without tools remains rejected",
			body:    `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"tool_choice":"auto"}`,
			wantErr: "tool_choice is not supported while reasoning is active",
		},
		{
			name:    "non-default tool choice remains rejected",
			body:    `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{}}}}],"tool_choice":"none"}`,
			wantErr: "tool_choice is not supported while reasoning is active",
		},
		{
			name:       "tool history preserves reasoning",
			body:       `{"model":"deepseek-v4-pro","messages":[{"role":"assistant","content":null,"reasoning_content":"checked","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`,
			wantToggle: "omitted", wantEffort: "omitted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error=%v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if evidence.ReasoningToggleRequest != test.wantToggle || evidence.ReasoningEffortRequest != test.wantEffort ||
				evidence.ReasoningProviderDefault != "thinking=enabled;reasoning_effort=high" {
				t.Fatalf("unexpected evidence: %+v", evidence)
			}
			if got := strings.Join(evidence.ProviderRequestNormalizations, ","); got != test.wantNormalization {
				t.Fatalf("normalizations=%q want=%q", got, test.wantNormalization)
			}
		})
	}
}

func TestDeepSeekCurrentProfilePreservesCompatibilityEffortEvidence(t *testing.T) {
	plan := executionProfilePlan(t, "deepseek.v4-pro.api", "1.2.0")
	for _, test := range []struct {
		request   string
		effective string
	}{
		{request: "low", effective: "high"},
		{request: "medium", effective: "high"},
		{request: "high", effective: "high"},
		{request: "xhigh", effective: "max"},
		{request: "max", effective: "max"},
	} {
		t.Run(test.request, func(t *testing.T) {
			body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"` + test.request + `"}`)
			evidence, err := validateChatExecutionProfileRequest(body, plan)
			require.NoError(t, err)
			require.Equal(t, test.request, evidence.ReasoningEffortRequest)
			require.Equal(t, test.effective, evidence.ReasoningEffortEffective)
			normalized, err := evidence.normalizeProviderRequest(body)
			require.NoError(t, err)
			require.JSONEq(t, string(body), string(normalized))
		})
	}

	_, err := validateChatExecutionProfileRequest([]byte(`{"model":"deepseek-v4-pro","messages":[],"reasoning_effort":"ultra"}`), plan)
	require.ErrorContains(t, err, "high, max, low, medium, xhigh")
}

func TestDeepSeekAutoToolChoiceIsOmittedFromProviderRequest(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.test", encKey, nil)
	setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.0.0")
	plan.Routes[0].Provider = "deepseek"
	plan.Routes[0].UpstreamModel = "deepseek-v4-flash"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-deepseek-auto-tool-choice",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{}}}}],"tool_choice":"auto","max_tokens":128}`), time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(actual, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["tool_choice"]; exists {
		t.Fatalf("redundant tool_choice reached provider request: %s", actual)
	}
	if _, exists := payload["tools"]; !exists {
		t.Fatalf("tool definitions were removed: %s", actual)
	}
	if got := strings.Join(batch.profileEvidence.ProviderRequestNormalizations, ","); got != "tool_choice=auto->omitted" {
		t.Fatalf("normalizations=%q", got)
	}
}

func TestUnmodeledReasoningControlsFailClosed(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m2.7.token-plan", "1.0.0")
	for _, body := range []string{
		`{"model":"MiniMax-M2.7","messages":[],"thinking":{"type":"enabled"}}`,
		`{"model":"MiniMax-M2.7","messages":[],"reasoning_effort":"high"}`,
		`{"model":"MiniMax-M2.7","messages":[],"reasoning":{"effort":"high"}}`,
	} {
		if _, err := validateChatExecutionProfileRequest([]byte(body), plan); err == nil ||
			!strings.Contains(err.Error(), "reasoning control is not supported") {
			t.Fatalf("expected unmodeled reasoning rejection, got %v", err)
		}
	}
}

func TestMiniMaxCurrentOutputContractsBindBothFieldsToTheExactModelMaximum(t *testing.T) {
	for _, test := range []struct {
		name, profileID, model string
		maximum                int
	}{
		{name: "M2.7", profileID: "minimax.m2.7.token-plan", model: "MiniMax-M2.7", maximum: 204_800},
		{name: "M2.5 highspeed", profileID: "minimax.m2.5-highspeed.token-plan", model: "MiniMax-M2.5-highspeed", maximum: 204_800},
		{name: "M3", profileID: "minimax.m3.token-plan", model: "MiniMax-M3", maximum: 524_288},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := executionProfilePlan(t, test.profileID, "1.2.0")
			for _, field := range []string{"max_tokens", "max_completion_tokens"} {
				accepted := fmt.Sprintf(`{"model":%q,"messages":[],%q:%d}`, test.model, field, test.maximum)
				_, err := validateChatExecutionProfileRequest([]byte(accepted), plan)
				require.NoError(t, err, field)

				rejected := fmt.Sprintf(`{"model":%q,"messages":[],%q:%d}`, test.model, field, test.maximum+1)
				_, err = validateChatExecutionProfileRequest([]byte(rejected), plan)
				require.ErrorContains(t, err, fmt.Sprintf("between 1 and %d", test.maximum), field)
			}

			_, err := validateChatExecutionProfileRequest([]byte(fmt.Sprintf(
				`{"model":%q,"messages":[],"service_tier":"priority"}`, test.model,
			)), plan)
			require.ErrorContains(t, err, "service_tier is not supported")
		})
	}
}

func TestGrok46CoreChatAcceptsOnlyTheDocumentedMaxTokensFieldWithinPolicy(t *testing.T) {
	plan := executionProfilePlan(t, "xai.grok-4.6.api", "1.1.0")
	_, err := validateChatExecutionProfileRequest(
		[]byte(`{"model":"grok-4.6","messages":[],"stream":true,"max_tokens":131072}`), plan,
	)
	require.NoError(t, err)

	_, err = validateChatExecutionProfileRequest(
		[]byte(`{"model":"grok-4.6","messages":[],"max_tokens":131073}`), plan,
	)
	require.ErrorContains(t, err, "max_tokens must be between 1 and 131072")

	_, err = validateChatExecutionProfileRequest(
		[]byte(`{"model":"grok-4.6","messages":[],"max_completion_tokens":128}`), plan,
	)
	require.ErrorContains(t, err, "max_completion_tokens is not supported")

	for _, body := range []string{
		`{"model":"grok-4.6","messages":[],"service_tier":"priority"}`,
		`{"model":"grok-4.6","messages":[],"reasoning_effort":"high"}`,
		`{"model":"grok-4.6","messages":[],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`,
		`{"model":"grok-4.6","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":{"type":"object"}}}}`,
	} {
		_, err = validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestHistoricalProtocolAuthoritiesPreserveTheirOriginalGenericAndPartialChatBoundary(t *testing.T) {
	miniMaxBody := []byte(`{"model":"MiniMax-M2.7","messages":[],"functions":[{"name":"lookup","parameters":{"type":"object"}}],"function_call":"auto","parallel_tool_calls":true,"tool_choice":{"type":"function","function":{"name":"lookup"}},"service_tier":"legacy"}`)
	_, err := estimateChatPreProfileAdmission(miniMaxBody)
	require.NoError(t, err)
	_, err = validateChatExecutionProfileRequest(
		miniMaxBody,
		executionProfilePlan(t, "minimax.m2.7.token-plan", "1.0.0"),
	)
	require.NoError(t, err)

	deepSeekBody := []byte(`{"model":"deepseek-v4-pro","messages":[],"thinking":{"type":"disabled"},"functions":[{"name":"lookup","parameters":{"type":"object"}}],"function_call":"auto","parallel_tool_calls":true,"tool_choice":{"type":"function","function":{"name":"lookup"}},"service_tier":"legacy"}`)
	_, err = validateChatExecutionProfileRequest(
		deepSeekBody,
		executionProfilePlan(t, "deepseek.v4-pro.api", "1.1.0"),
	)
	require.NoError(t, err)

	kimiBody := []byte(`{"model":"kimi-k2.6","messages":[],"functions":[{"name":"lookup","parameters":{"type":"object"}}],"function_call":"auto","parallel_tool_calls":true,"service_tier":"legacy"}`)
	_, err = validateChatExecutionProfileRequest(
		kimiBody,
		executionProfilePlan(t, "kimi.k2.6.api", "1.5.0"),
	)
	require.NoError(t, err)

	for _, current := range []struct {
		profileID, version string
	}{
		{profileID: "minimax.m2.7.token-plan", version: "1.2.0"},
		{profileID: "deepseek.v4-pro.api", version: "1.2.0"},
		{profileID: "kimi.k2.6.api", version: "1.7.0"},
	} {
		_, err = validateChatExecutionProfileRequest(
			miniMaxBody,
			executionProfilePlan(t, current.profileID, current.version),
		)
		require.Error(t, err, current.profileID)
	}
}

func TestGLM53BaseNonStreamChatUsesTypedProtocolWithoutAdvancedClaims(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.3.api", "1.1.0")
	body := []byte(`{"model":"glm-5.3","messages":[{"role":"system","content":"You are concise."},{"role":"user","content":"Hello"}],"stream":false}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.v1", evidence.ModelProtocolProfile)
	require.Empty(t, evidence.ReasoningMode)
	require.Empty(t, evidence.ReservedMeterUsage)
	require.Empty(t, evidence.ProviderRequestNormalizations)

	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.3","messages":"invalid","stream":false}`), plan)
	require.ErrorContains(t, err, "messages must be an array")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"Hello"}],"thinking":{"type":"enabled"}}`), plan)
	require.ErrorContains(t, err, "reasoning control is not supported")
}

func TestGLM52BaseNonStreamChatUsesTypedProtocolWithoutAdvancedClaims(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.2.api", "1.1.0")
	body := []byte(`{"model":"glm-5.2","messages":[{"role":"system","content":"You are concise."},{"role":"user","content":"Hello"}],"stream":false}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.v1", evidence.ModelProtocolProfile)
	require.Empty(t, evidence.ReasoningMode)
	require.Empty(t, evidence.ReservedMeterUsage)
	require.Empty(t, evidence.ProviderRequestNormalizations)

	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.2","messages":"invalid","stream":false}`), plan)
	require.ErrorContains(t, err, "messages must be an array")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.2","messages":[],"stream":true}`), plan)
	require.ErrorContains(t, err, "streaming is not supported")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.2","messages":[],"thinking":{"type":"enabled"}}`), plan)
	require.ErrorContains(t, err, "reasoning control is not supported")
}

func TestGLM5VTurboBaseTextNonStreamChatUsesTypedProtocolWithoutAdvancedClaims(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5v-turbo.api", "1.1.0")
	body := []byte(`{"model":"glm-5v-turbo","messages":[{"role":"system","content":"You are concise."},{"role":"user","content":"Hello"}],"stream":false}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.v1", evidence.ModelProtocolProfile)
	require.Empty(t, evidence.ReasoningMode)
	require.Empty(t, evidence.ReservedMeterUsage)
	require.Empty(t, evidence.ProviderRequestNormalizations)

	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5v-turbo","messages":[],"stream":true}`), plan)
	require.ErrorContains(t, err, "streaming is not supported")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5v-turbo","messages":[],"thinking":{"type":"enabled"}}`), plan)
	require.ErrorContains(t, err, "reasoning control is not supported")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]}]}`), plan)
	require.ErrorContains(t, err, "image input is not supported")
}

func TestGLM5VTurboStreamingChatUsesTypedProtocolAndPreservesBaseVersion(t *testing.T) {
	streaming := executionProfilePlan(t, "glm.5v-turbo.api", "1.2.0")
	body := []byte(`{"model":"glm-5v-turbo","messages":[{"role":"user","content":"Hello"}],"stream":true}`)
	evidence, err := validateChatExecutionProfileRequest(body, streaming)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.v2", evidence.ModelProtocolProfile)

	base := executionProfilePlan(t, "glm.5v-turbo.api", "1.1.0")
	_, err = validateChatExecutionProfileRequest(body, base)
	require.ErrorContains(t, err, "streaming is not supported")
}

func TestGLM5VTurboExplicitMaxTokensUsesOfficialFieldAndBounds(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5v-turbo.api", "1.3.0")
	for _, limit := range []int{1, 65_536, 131_072} {
		body := []byte(fmt.Sprintf(`{"model":"glm-5v-turbo","messages":[],"max_tokens":%d}`, limit))
		_, err := validateChatExecutionProfileRequest(body, plan)
		require.NoError(t, err)
	}
	for _, body := range []string{
		`{"model":"glm-5v-turbo","messages":[],"max_tokens":0}`,
		`{"model":"glm-5v-turbo","messages":[],"max_tokens":131073}`,
		`{"model":"glm-5v-turbo","messages":[],"max_completion_tokens":1024}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err)
	}
}

func TestGLM5VTurboReasoningModelsToggleEffortAliasesAndDisableValues(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5v-turbo.api", "1.4.0")
	tests := []struct {
		name          string
		body          string
		wantToggle    string
		wantEffort    string
		wantEffective string
		wantClear     string
	}{
		{name: "provider defaults", body: `{"model":"glm-5v-turbo","messages":[]}`, wantToggle: "omitted", wantEffort: "omitted", wantEffective: "omitted", wantClear: "omitted"},
		{name: "explicit disabled", body: `{"model":"glm-5v-turbo","messages":[],"thinking":{"type":"disabled"}}`, wantToggle: "disabled", wantEffort: "omitted", wantEffective: "omitted", wantClear: "omitted"},
		{name: "compatibility low", body: `{"model":"glm-5v-turbo","messages":[],"reasoning_effort":"low"}`, wantToggle: "omitted", wantEffort: "low", wantEffective: "high", wantClear: "omitted"},
		{name: "compatibility none", body: `{"model":"glm-5v-turbo","messages":[],"reasoning_effort":"none"}`, wantToggle: "omitted", wantEffort: "none", wantEffective: "disabled", wantClear: "omitted"},
		{name: "preserved thinking", body: `{"model":"glm-5v-turbo","messages":[],"thinking":{"type":"enabled","clear_thinking":false}}`, wantToggle: "enabled", wantEffort: "omitted", wantEffective: "omitted", wantClear: "false"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.NoError(t, err)
			require.Equal(t, modelprofile.ReasoningModeToggle, evidence.ReasoningMode)
			require.Equal(t, test.wantToggle, evidence.ReasoningToggleRequest)
			require.Equal(t, test.wantEffort, evidence.ReasoningEffortRequest)
			require.Equal(t, test.wantEffective, evidence.ReasoningEffortEffective)
			require.Equal(t, test.wantClear, evidence.ReasoningThinkingClearRequest)
			require.Equal(t, "true", evidence.ReasoningThinkingClearDefault)
		})
	}

	for _, body := range []string{
		`{"model":"glm-5v-turbo","messages":[],"thinking":{"type":"auto"}}`,
		`{"model":"glm-5v-turbo","messages":[],"reasoning_effort":"ultra"}`,
		`{"model":"glm-5v-turbo","messages":[],"thinking":{"type":"disabled"},"reasoning_effort":"high"}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestGLM5VTurboFunctionToolsUseOfficialSchemaAndAutoChoiceOnly(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5v-turbo.api", "1.5.0")
	valid := []byte(`{"model":"glm-5v-turbo","messages":[],"tools":[{"type":"function","function":{"name":"inspect_layout","description":"Inspect a layout","parameters":{"type":"object","properties":{"selector":{"type":"string"}}}}}],"tool_choice":"auto"}`)
	evidence, err := validateChatExecutionProfileRequest(valid, plan)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.v4", evidence.ModelProtocolProfile)

	for _, body := range []string{
		`{"model":"glm-5v-turbo","messages":[],"tools":[{"type":"function","function":{"name":"bad name","description":"d","parameters":{}}}]}`,
		`{"model":"glm-5v-turbo","messages":[],"tools":[{"type":"function","function":{"name":"ok","parameters":{}}}]}`,
		`{"model":"glm-5v-turbo","messages":[],"tools":[{"type":"function","function":{"name":"ok","description":"d"}}]}`,
		`{"model":"glm-5v-turbo","messages":[],"tools":[{"type":"function","function":{"name":"ok","description":"d","parameters":{}}}],"tool_choice":"required"}`,
		`{"model":"glm-5v-turbo","messages":[],"tools":[{"type":"function","function":{"name":"ok","description":"d","parameters":{}}}],"tool_choice":{"type":"function","function":{"name":"ok"}}}`,
		`{"model":"glm-5v-turbo","messages":[],"tools":[{"type":"web_search","web_search":{}}]}`,
		`{"model":"glm-5v-turbo","messages":[],"tool_stream":true}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestGLM5VTurboStructuredOutputFailsClosedForVisionModel(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5v-turbo.api", "1.6.0")
	evidence, err := validateChatExecutionProfileRequest([]byte(`{"model":"glm-5v-turbo","messages":[]}`), plan)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.glm5v.v1", evidence.ModelProtocolProfile)

	for _, body := range []string{
		`{"model":"glm-5v-turbo","messages":[],"response_format":{"type":"text"}}`,
		`{"model":"glm-5v-turbo","messages":[],"response_format":{"type":"json_object"}}`,
		`{"model":"glm-5v-turbo","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"x","schema":{}}}}`,
		`{"model":"glm-5v-turbo","messages":[],"guided_json":{}}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestGLM5VTurboImageInputAcceptsBoundedOfficialWireShapes(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5v-turbo.api", "1.8.0")
	for _, body := range []string{
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://cdn.example.test/layout.png"}},{"type":"text","text":"Describe"}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,aA=="}},{"type":"text","text":"Describe"}]}]}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.glm-openai-chat.glm5v.v2", evidence.ModelProtocolProfile)
	}

	for _, body := range []string{
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://example.test/image.png"}}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/gif;base64,aA=="}}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,not-base64"}}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://cdn.example.test/video.mp4"}}]}]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestGLM5VTurboVideoInputAcceptsHTTPSAndRejectsMixedMedia(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5v-turbo.api", "1.9.0")
	body := []byte(`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://cdn.example.test/demo.mov"}},{"type":"text","text":"Track the object"}]}]}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.glm5v.v3", evidence.ModelProtocolProfile)

	for _, body := range []string{
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"http://example.test/demo.mov"}}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"data:video/quicktime;base64,aA=="}}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/image.png"}},{"type":"video_url","video_url":{"url":"https://example.test/video.mov"}}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"file_url","file_url":{"url":"https://example.test/file.pdf"}}]}]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestGLM5VTurboFileInputAcceptsDocumentedHTTPSShapesAndRejectsMixedMedia(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5v-turbo.api", "1.10.0")
	for _, body := range []string{
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"file_url","file_url":{"url":"https://cdn.example.test/report.pdf"}},{"type":"text","text":"Summarize"}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"file_url","file_url":{"url":"https://cdn.example.test/notes.txt?signature=x"}},{"type":"text","text":"Summarize"}]}]}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.glm-openai-chat.glm5v.v4", evidence.ModelProtocolProfile)
	}

	for _, body := range []string{
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"file_url","file_url":{"url":"http://example.test/report.pdf"}}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"file_url","file_url":{"url":"https://example.test/archive.zip"}}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"file_url","file_url":{"url":"data:application/pdf;base64,aA=="}}]}]}`,
		`{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"file_url","file_url":{"url":"https://example.test/report.pdf"}},{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]}]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestGLM52StreamingChatUsesTypedProtocolAndPreservesBaseVersion(t *testing.T) {
	streaming := executionProfilePlan(t, "glm.5.2.api", "1.2.0")
	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"Hello"}],"stream":true}`)
	evidence, err := validateChatExecutionProfileRequest(body, streaming)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.v2", evidence.ModelProtocolProfile)

	base := executionProfilePlan(t, "glm.5.2.api", "1.1.0")
	_, err = validateChatExecutionProfileRequest(body, base)
	require.ErrorContains(t, err, "streaming is not supported")
}

func TestGLM52ExplicitMaxTokensUsesOfficialFieldAndBounds(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.2.api", "1.3.0")
	for _, limit := range []int{1, 65_536, 131_072} {
		body := []byte(fmt.Sprintf(`{"model":"glm-5.2","messages":[],"max_tokens":%d}`, limit))
		_, err := validateChatExecutionProfileRequest(body, plan)
		require.NoError(t, err)
	}
	for _, body := range []string{
		`{"model":"glm-5.2","messages":[],"max_tokens":0}`,
		`{"model":"glm-5.2","messages":[],"max_tokens":131073}`,
		`{"model":"glm-5.2","messages":[],"max_completion_tokens":1024}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err)
	}
}

func TestGLM52ReasoningModelsToggleEffortAliasesAndDisableValues(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.2.api", "1.4.0")
	tests := []struct {
		name          string
		body          string
		wantToggle    string
		wantEffort    string
		wantEffective string
	}{
		{name: "provider defaults", body: `{"model":"glm-5.2","messages":[]}`, wantToggle: "omitted", wantEffort: "omitted", wantEffective: "omitted"},
		{name: "disabled", body: `{"model":"glm-5.2","messages":[],"thinking":{"type":"disabled"}}`, wantToggle: "disabled", wantEffort: "omitted", wantEffective: "omitted"},
		{name: "low maps high", body: `{"model":"glm-5.2","messages":[],"reasoning_effort":"low"}`, wantToggle: "omitted", wantEffort: "low", wantEffective: "high"},
		{name: "medium maps high", body: `{"model":"glm-5.2","messages":[],"reasoning_effort":"medium"}`, wantToggle: "omitted", wantEffort: "medium", wantEffective: "high"},
		{name: "xhigh maps max", body: `{"model":"glm-5.2","messages":[],"reasoning_effort":"xhigh"}`, wantToggle: "omitted", wantEffort: "xhigh", wantEffective: "max"},
		{name: "none disables", body: `{"model":"glm-5.2","messages":[],"reasoning_effort":"none"}`, wantToggle: "omitted", wantEffort: "none", wantEffective: "disabled"},
		{name: "minimal disables", body: `{"model":"glm-5.2","messages":[],"reasoning_effort":"minimal"}`, wantToggle: "omitted", wantEffort: "minimal", wantEffective: "disabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.NoError(t, err)
			require.Equal(t, test.wantToggle, evidence.ReasoningToggleRequest)
			require.Equal(t, test.wantEffort, evidence.ReasoningEffortRequest)
			require.Equal(t, test.wantEffective, evidence.ReasoningEffortEffective)
		})
	}

	_, err := validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.2","messages":[],"thinking":{"type":"disabled"},"reasoning_effort":"none"}`), plan)
	require.ErrorContains(t, err, "requires active reasoning")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.2","messages":[],"reasoning_effort":"future"}`), plan)
	require.ErrorContains(t, err, "reasoning_effort must be one of")
}

func TestGLM52FunctionToolsUseOfficialSchemaAndAutoChoiceOnly(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.2.api", "1.5.0")
	valid := []byte(`{"model":"glm-5.2","messages":[],"tools":[{"type":"function","function":{"name":"lookup","description":"Look up data","parameters":{"type":"object"}}}],"tool_choice":"auto"}`)
	_, err := validateChatExecutionProfileRequest(valid, plan)
	require.NoError(t, err)

	for _, body := range []string{
		`{"model":"glm-5.2","messages":[],"tools":[{"type":"function","function":{"name":"lookup","description":"d","parameters":{}}}],"tool_choice":"required"}`,
		`{"model":"glm-5.2","messages":[],"tools":[{"type":"retrieval","retrieval":{"knowledge_id":"x"}}]}`,
		`{"model":"glm-5.2","messages":[],"tools":[{"type":"mcp","mcp":{}}]}`,
		`{"model":"glm-5.2","messages":[],"tool_stream":true}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err)
	}
}

func TestGLM52JSONObjectAcceptsOnlyOfficialResponseFormats(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.2.api", "1.6.0")
	for _, kind := range []string{"text", "json_object"} {
		_, err := validateChatExecutionProfileRequest([]byte(fmt.Sprintf(`{"model":"glm-5.2","messages":[],"response_format":{"type":%q}}`, kind)), plan)
		require.NoError(t, err)
	}
	for _, body := range []string{
		`{"model":"glm-5.2","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"x","schema":{}}}}`,
		`{"model":"glm-5.2","messages":[],"guided_json":{}}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err)
	}
}

func TestGLM53StreamingChatUsesTypedProtocolAndPreservesBaseVersion(t *testing.T) {
	streaming := executionProfilePlan(t, "glm.5.3.api", "1.2.0")
	body := []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"Hello"}],"stream":true}`)
	evidence, err := validateChatExecutionProfileRequest(body, streaming)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.v2", evidence.ModelProtocolProfile)
	require.Empty(t, evidence.ReasoningMode)
	require.Empty(t, evidence.ReservedMeterUsage)
	require.Empty(t, evidence.ProviderRequestNormalizations)

	base := executionProfilePlan(t, "glm.5.3.api", "1.1.0")
	_, err = validateChatExecutionProfileRequest(body, base)
	require.ErrorContains(t, err, "streaming is not supported by protocol profile protocol.glm-openai-chat.v1")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.3","messages":[],"stream":"true"}`), streaming)
	require.ErrorContains(t, err, "stream must be a boolean")
}

func TestGLM53ExplicitMaxTokensUsesOfficialFieldAndBounds(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.3.api", "1.3.0")
	for _, limit := range []int{1, 65_536, 131_072} {
		body := []byte(fmt.Sprintf(`{"model":"glm-5.3","messages":[{"role":"user","content":"Hello"}],"max_tokens":%d}`, limit))
		evidence, err := validateChatExecutionProfileRequest(body, plan)
		require.NoError(t, err)
		require.Equal(t, "protocol.glm-openai-chat.v3", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"glm-5.3","messages":[],"max_tokens":0}`,
		`{"model":"glm-5.3","messages":[],"max_tokens":131073}`,
		`{"model":"glm-5.3","messages":[],"max_tokens":1.5}`,
		`{"model":"glm-5.3","messages":[],"max_tokens":null}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err)
	}
	_, err := validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.3","messages":[],"max_completion_tokens":1024}`), plan)
	require.ErrorContains(t, err, "max_completion_tokens is not supported")

	streaming := executionProfilePlan(t, "glm.5.3.api", "1.2.0")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"glm-5.3","messages":[],"max_tokens":1024}`), streaming)
	require.ErrorContains(t, err, "max_tokens is not supported")
}

func TestGLM53ReasoningUsesRequiredThinkingAndModelSpecificEffort(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.3.api", "1.4.0")
	for _, test := range []struct {
		name          string
		body          string
		wantToggle    string
		wantEffort    string
		wantEffective string
		wantClear     string
	}{
		{name: "provider defaults", body: `{"model":"glm-5.3","messages":[]}`, wantToggle: "omitted", wantEffort: "omitted", wantEffective: "omitted", wantClear: "omitted"},
		{name: "explicit enabled low", body: `{"model":"glm-5.3","messages":[],"thinking":{"type":"enabled","clear_thinking":false},"reasoning_effort":"low"}`, wantToggle: "enabled", wantEffort: "low", wantEffective: "low", wantClear: "false"},
		{name: "explicit high", body: `{"model":"glm-5.3","messages":[],"thinking":{"type":"enabled"},"reasoning_effort":"high"}`, wantToggle: "enabled", wantEffort: "high", wantEffective: "high", wantClear: "omitted"},
		{name: "explicit max", body: `{"model":"glm-5.3","messages":[],"reasoning_effort":"max"}`, wantToggle: "omitted", wantEffort: "max", wantEffective: "max", wantClear: "omitted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.NoError(t, err)
			require.Equal(t, modelprofile.ReasoningModeRequired, evidence.ReasoningMode)
			require.Equal(t, "thinking=enabled;reasoning_effort=max", evidence.ReasoningProviderDefault)
			require.Equal(t, test.wantToggle, evidence.ReasoningToggleRequest)
			require.Equal(t, test.wantEffort, evidence.ReasoningEffortRequest)
			require.Equal(t, test.wantEffective, evidence.ReasoningEffortEffective)
			require.Equal(t, test.wantClear, evidence.ReasoningThinkingClearRequest)
			require.Equal(t, "true", evidence.ReasoningThinkingClearDefault)
		})
	}

	for _, body := range []string{
		`{"model":"glm-5.3","messages":[],"thinking":{"type":"disabled"}}`,
		`{"model":"glm-5.3","messages":[],"thinking":{"type":"enabled","clear_thinking":"false"}}`,
		`{"model":"glm-5.3","messages":[],"reasoning_effort":"none"}`,
		`{"model":"glm-5.3","messages":[],"reasoning_effort":"minimal"}`,
		`{"model":"glm-5.3","messages":[],"reasoning_effort":"medium"}`,
		`{"model":"glm-5.3","messages":[],"reasoning_effort":"xhigh"}`,
		`{"model":"glm-5.3","messages":[],"reasoning":{"effort":"high"}}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err)
	}
}

func TestGLM53FunctionToolsUseOfficialSchemaAndAutoChoiceOnly(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.3.api", "1.5.0")
	valid := []byte(`{
		"model":"glm-5.3","messages":[{"role":"user","content":"Weather?"}],
		"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}],
		"tool_choice":"auto"
	}`)
	evidence, err := validateChatExecutionProfileRequest(valid, plan)
	require.NoError(t, err)
	require.Equal(t, "protocol.glm-openai-chat.v4", evidence.ModelProtocolProfile)
	require.Empty(t, evidence.ReservedMeterUsage)
	require.Empty(t, evidence.ProviderRequestNormalizations)

	tooMany := make([]string, 129)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf(`{"type":"function","function":{"name":"fn_%d","description":"d","parameters":{}}}`, index)
	}
	invalid := []string{
		`{"model":"glm-5.3","messages":[],"tools":null}`,
		`{"model":"glm-5.3","messages":[],"tools":[{"type":"function","function":{"name":"bad name","description":"d","parameters":{}}}]}`,
		`{"model":"glm-5.3","messages":[],"tools":[{"type":"function","function":{"name":"ok","parameters":{}}}]}`,
		`{"model":"glm-5.3","messages":[],"tools":[{"type":"function","function":{"name":"ok","description":"d"}}]}`,
		`{"model":"glm-5.3","messages":[],"tools":[{"type":"function","function":{"name":"ok","description":"d","parameters":{},"strict":true}}]}`,
		`{"model":"glm-5.3","messages":[],"tools":[{"type":"web_search","web_search":{}}]}`,
		`{"model":"glm-5.3","messages":[],"tools":[],"tool_choice":"auto"}`,
		`{"model":"glm-5.3","messages":[],"tools":[{"type":"function","function":{"name":"ok","description":"d","parameters":{}}}],"tool_choice":"required"}`,
		`{"model":"glm-5.3","messages":[],"tools":[{"type":"function","function":{"name":"ok","description":"d","parameters":{}}}],"tool_choice":{"type":"function","function":{"name":"ok"}}}`,
		`{"model":"glm-5.3","messages":[],"functions":[]}`,
		`{"model":"glm-5.3","messages":[],"parallel_tool_calls":true}`,
		`{"model":"glm-5.3","messages":[],"tool_stream":true}`,
		fmt.Sprintf(`{"model":"glm-5.3","messages":[],"tools":[%s]}`, strings.Join(tooMany, ",")),
	}
	for _, body := range invalid {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen38MaxChatTextStreamingAndOutputContract(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.8-max.api", "1.1.0")
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"qwen3.8-max","messages":[],"stream":true,"stream_options":{"include_usage":true}}`,
		`{"model":"qwen3.8-max","messages":[],"max_completion_tokens":1}`,
		`{"model":"qwen3.8-max","messages":[],"max_completion_tokens":131072}`,
		`{"model":"qwen3.8-max","messages":[],"max_tokens":131072}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.qwen-openai-chat.v1", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[],"stream":"true"}`,
		`{"model":"qwen3.8-max","messages":[],"max_completion_tokens":0}`,
		`{"model":"qwen3.8-max","messages":[],"max_completion_tokens":131073}`,
		`{"model":"qwen3.8-max","messages":[],"max_tokens":128,"max_completion_tokens":128}`,
		`{"model":"qwen3.8-max","messages":[],"max_tokens":64,"max_completion_tokens":128}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen38MaxReasoningControlsAndConditionalOutputLimit(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.8-max.api", "1.2.0")
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[]}`,
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":true,"reasoning_effort":"low","max_tokens":32768}`,
		`{"model":"qwen3.8-max","messages":[],"reasoning_effort":"medium"}`,
		`{"model":"qwen3.8-max","messages":[],"reasoning_effort":"xhigh"}`,
		`{"model":"qwen3.8-max","messages":[],"reasoning_effort":"minimal"}`,
		`{"model":"qwen3.8-max","messages":[],"reasoning_effort":"high"}`,
		`{"model":"qwen3.8-max","messages":[],"reasoning_effort":"max"}`,
		`{"model":"qwen3.8-max","messages":[],"reasoning_effort":"none"}`,
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":false,"max_tokens":131072}`,
		`{"model":"qwen3.8-max","messages":[],"thinking_budget":0}`,
		`{"model":"qwen3.8-max","messages":[],"thinking_budget":262144,"max_completion_tokens":131072}`,
		`{"model":"qwen3.8-max","messages":[],"preserve_thinking":true}`,
		`{"model":"qwen3.8-max","messages":[],"preserve_thinking":false}`,
		`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":"answer","reasoning_content":"complete chain"}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":"answer"}]}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "reasoning.qwen3.8-max-hybrid.v1", evidence.ReasoningProfile)
		require.Equal(t, "thinking=enabled;reasoning_effort=xhigh", evidence.ReasoningProviderDefault)
		require.Equal(t, "131072", evidence.ReasoningBudgetDefault)
		require.Equal(t, "true", evidence.ReasoningPreserveDefault)
	}
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":"true"}`,
		`{"model":"qwen3.8-max","messages":[],"reasoning_effort":"ultra"}`,
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":false,"reasoning_effort":"low"}`,
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":false,"thinking_budget":1}`,
		`{"model":"qwen3.8-max","messages":[],"reasoning_effort":"low","thinking_budget":4096}`,
		`{"model":"qwen3.8-max","messages":[],"thinking_budget":262145}`,
		`{"model":"qwen3.8-max","messages":[],"preserve_thinking":"true"}`,
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":true,"max_tokens":32769}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen38MaxCorrectedReasoningOutputLimitKeepsHistoricalContractImmutable(t *testing.T) {
	current := executionProfilePlan(t, "qwen.3.8-max.api", "1.8.0")
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":true,"max_tokens":131072}`,
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":true,"max_completion_tokens":131072}`,
		`{"model":"qwen3.8-max","messages":[],"thinking_budget":262144,"max_tokens":131072}`,
		`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":"answer","reasoning_content":"complete chain"}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":"answer"}],"preserve_thinking":true}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), current)
		require.NoError(t, err, body)
		require.Equal(t, "reasoning.qwen3.8-max-hybrid.v2", evidence.ReasoningProfile)
		require.Equal(t, "true", evidence.ReasoningPreserveDefault)
	}
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":true,"max_tokens":131073}`,
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":true,"max_completion_tokens":131073}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), current)
		require.Error(t, err, body)
	}

	historical := executionProfilePlan(t, "qwen.3.8-max.api", "1.7.0")
	_, err := validateChatExecutionProfileRequest([]byte(
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":true,"max_tokens":32769}`,
	), historical)
	require.Error(t, err, "historical reasoning contract must not be backfilled")
}

func TestQwen38MaxFunctionToolsChoicesParallelAndToolStream(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.8-max.api", "1.3.0")
	tool := `{"type":"function","function":{"name":"lookup-weather","description":"Look up weather"}}`
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[],"tools":[` + tool + `],"tool_choice":"auto"}`,
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":false,"tools":[` + tool + `],"tool_choice":"required"}`,
		`{"model":"qwen3.8-max","messages":[],"enable_thinking":false,"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup-weather"}}}`,
		`{"model":"qwen3.8-max","messages":[],"tools":[` + tool + `],"parallel_tool_calls":true}`,
		`{"model":"qwen3.8-max","messages":[],"stream":true,"tools":[` + tool + `],"tool_stream":true}`,
		`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup-weather","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}],"tools":[` + tool + `]}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.qwen-openai-chat.v2", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[],"tools":null}`,
		`{"model":"qwen3.8-max","messages":[],"tools":[{"type":"function","function":{"name":"bad name","description":"d"}}]}`,
		`{"model":"qwen3.8-max","messages":[],"tools":[{"type":"function","function":{"name":"ok"}}]}`,
		`{"model":"qwen3.8-max","messages":[],"tools":[{"type":"function","function":{"name":"ok","description":"d","strict":true}}]}`,
		`{"model":"qwen3.8-max","messages":[],"tools":[],"tool_choice":"auto"}`,
		`{"model":"qwen3.8-max","messages":[],"tools":[` + tool + `],"tool_choice":"required"}`,
		`{"model":"qwen3.8-max","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup-weather"}}}`,
		`{"model":"qwen3.8-max","messages":[],"tools":[` + tool + `],"parallel_tool_calls":"true"}`,
		`{"model":"qwen3.8-max","messages":[],"stream":true,"tools":[` + tool + `],"tool_stream":"true"}`,
		`{"model":"qwen3.8-max","messages":[],"tools":[` + tool + `],"tool_stream":true}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwenReasoningAndToolExtensionsPassTheFullAdmissionPath(t *testing.T) {
	qwen38Reasoning := executionProfilePlan(t, "qwen.3.8-max.api", "1.2.0")
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":"answer","reasoning_content":"complete chain"}],"thinking_budget":4096,"preserve_thinking":true}`,
		`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":"answer"}],"preserve_thinking":true}`,
	} {
		_, err := estimateChatAdmissionForPlan([]byte(body), qwen38Reasoning)
		require.NoError(t, err, body)
	}
	_, err := estimateChatAdmissionForPlan([]byte(
		`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":"answer","reasoning_content":{"text":"invalid"}}],"preserve_thinking":true}`,
	), qwen38Reasoning)
	require.Error(t, err)

	qwen38Tools := executionProfilePlan(t, "qwen.3.8-max.api", "1.3.0")
	_, err = estimateChatAdmissionForPlan([]byte(
		`{"model":"qwen3.8-max","messages":[],"stream":true,"tools":[{"type":"function","function":{"name":"lookup","description":"Lookup"}}],"tool_stream":true}`,
	), qwen38Tools)
	require.NoError(t, err)

	qwen37 := executionProfilePlan(t, "qwen.3.7-plus.api", "1.2.0")
	_, err = estimateChatAdmissionForPlan([]byte(
		`{"model":"qwen3.7-plus","messages":[],"enable_thinking":true,"thinking_budget":4096,"preserve_thinking":true}`,
	), qwen37)
	require.NoError(t, err)
}

func TestQwen38MaxStructuredOutputRequiresOfficialShapes(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.8-max.api", "1.4.0")
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[]}`,
		`{"model":"qwen3.8-max","messages":[],"response_format":{"type":"text"}}`,
		`{"model":"qwen3.8-max","messages":[{"role":"system","content":"Return JSON only"}],"response_format":{"type":"json_object"}}`,
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"text","text":"return json"}]}],"response_format":{"type":"json_object"}}`,
		`{"model":"qwen3.8-max","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}}}}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.qwen-openai-chat.v3", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[],"response_format":{"type":"json_object"}}`,
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":"return an object"}],"response_format":{"type":"json_object"}}`,
		`{"model":"qwen3.8-max","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}}}`,
		`{"model":"qwen3.8-max","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":false,"schema":{"type":"object"}}}}`,
		`{"model":"qwen3.8-max","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":true}}}`,
		`{"model":"qwen3.8-max","messages":[],"guided_json":{"type":"object"}}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen38MaxExplicitPromptCacheRequestContract(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.8-max.api", "1.5.0")
	marker := `{"type":"text","text":"long reusable prefix","cache_control":{"type":"ephemeral"}}`
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"system","content":[` + marker + `]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"system","content":[` + strings.Join([]string{marker, marker, marker, marker}, ",") + `]}]}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "cache-usage.qwen-context-cache.v1", evidence.CacheUsageProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[{"role":"system","content":[{"type":"text","text":"prefix","cache_control":{}}]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"system","content":[{"type":"text","text":"prefix","cache_control":{"type":"persistent"}}]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"system","content":[{"type":"text","text":"prefix","cache_control":{"type":"ephemeral","ttl":"5m"}}]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"system","content":[` + strings.Join([]string{marker, marker, marker, marker, marker}, ",") + `]}]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen38MaxImageAndVideoInputContract(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.8-max.api", "1.6.0")
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.jpg"}},{"type":"text","text":"describe"}]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}},{"type":"text","text":"describe"}]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.com/video.mp4"}},{"type":"text","text":"summarize"}]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AA=="}},{"type":"text","text":"summarize"}]}]}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.qwen-openai-chat.v5", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://example.com/image.jpg"}}]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/gif;base64,AA=="}}]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"data:video/webm;base64,AA=="}}]}]}`,
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.com/video.mp4"},"fps":2}]}]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen38MaxBuiltinSearchRequestIsModeledButNotBillingAuthority(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.8-max.api", "1.7.0")
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[]}`,
		`{"model":"qwen3.8-max","messages":[],"enable_search":false}`,
		`{"model":"qwen3.8-max","messages":[],"enable_search":true}`,
		`{"model":"qwen3.8-max","messages":[],"enable_search":true,"search_options":{"forced_search":true,"search_strategy":"turbo"}}`,
		`{"model":"qwen3.8-max","messages":[],"enable_search":true,"search_options":{"search_strategy":"max"}}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Empty(t, evidence.ReservedMeterUsage, "Chat Completions response cannot prove whether Qwen executed search")
	}
	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[],"enable_search":"true"}`,
		`{"model":"qwen3.8-max","messages":[],"search_options":{"forced_search":true}}`,
		`{"model":"qwen3.8-max","messages":[],"enable_search":true,"search_options":"turbo"}`,
		`{"model":"qwen3.8-max","messages":[],"enable_search":true,"search_options":{"forced_search":"true"}}`,
		`{"model":"qwen3.8-max","messages":[],"enable_search":true,"search_options":{"search_strategy":"agent"}}`,
		`{"model":"qwen3.8-max","messages":[],"enable_search":true,"search_options":{"enable_source":true}}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen37PlusChatTextStreamingAndOutputContract(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.7-plus.api", "1.1.0")
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"qwen3.7-plus","messages":[],"stream":true,"stream_options":{"include_usage":true}}`,
		`{"model":"qwen3.7-plus","messages":[],"max_completion_tokens":131072}`,
		`{"model":"qwen3.7-plus","messages":[],"max_tokens":131072}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.qwen-openai-chat.v1", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[],"stream":"true"}`,
		`{"model":"qwen3.7-plus","messages":[],"max_completion_tokens":0}`,
		`{"model":"qwen3.7-plus","messages":[],"max_tokens":131073}`,
		`{"model":"qwen3.7-plus","messages":[],"tools":[]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen37PlusReasoningBudgetAndPreserveThinkingContract(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.7-plus.api", "1.2.0")
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[]}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_thinking":true,"thinking_budget":1}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_thinking":true,"thinking_budget":262144,"preserve_thinking":true}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_thinking":false,"preserve_thinking":false}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "reasoning.qwen3.7-hybrid.v1", evidence.ReasoningProfile)
		require.Equal(t, "262144", evidence.ReasoningBudgetDefault)
		require.Equal(t, "false", evidence.ReasoningPreserveDefault)
	}
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[],"enable_thinking":"true"}`,
		`{"model":"qwen3.7-plus","messages":[],"thinking_budget":0}`,
		`{"model":"qwen3.7-plus","messages":[],"thinking_budget":262145}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_thinking":false,"thinking_budget":1}`,
		`{"model":"qwen3.7-plus","messages":[],"reasoning_effort":"high"}`,
		`{"model":"qwen3.7-plus","messages":[],"preserve_thinking":"true"}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen37PlusFunctionToolsChoicesParallelAndToolStream(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.7-plus.api", "1.3.0")
	tool := `{"type":"function","function":{"name":"lookup-weather","description":"Look up weather"}}`
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[],"tools":[` + tool + `],"tool_choice":"auto"}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_thinking":false,"tools":[` + tool + `],"tool_choice":"required"}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_thinking":false,"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup-weather"}}}`,
		`{"model":"qwen3.7-plus","messages":[],"tools":[` + tool + `],"parallel_tool_calls":true}`,
		`{"model":"qwen3.7-plus","messages":[],"stream":true,"tools":[` + tool + `],"tool_stream":true}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.qwen-openai-chat.v2", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[],"tools":null}`,
		`{"model":"qwen3.7-plus","messages":[],"tools":[{"type":"function","function":{"name":"bad name","description":"d"}}]}`,
		`{"model":"qwen3.7-plus","messages":[],"tools":[{"type":"function","function":{"name":"ok"}}]}`,
		`{"model":"qwen3.7-plus","messages":[],"tools":[` + tool + `],"tool_choice":"required"}`,
		`{"model":"qwen3.7-plus","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup-weather"}}}`,
		`{"model":"qwen3.7-plus","messages":[],"tools":[` + tool + `],"parallel_tool_calls":"true"}`,
		`{"model":"qwen3.7-plus","messages":[],"stream":true,"tools":[` + tool + `],"tool_stream":"true"}`,
		`{"model":"qwen3.7-plus","messages":[],"tools":[` + tool + `],"tool_stream":true}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen37PlusStructuredOutputRequiresOfficialShapes(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.7-plus.api", "1.4.0")
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[]}`,
		`{"model":"qwen3.7-plus","messages":[],"response_format":{"type":"text"}}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"system","content":"Return JSON only"}],"response_format":{"type":"json_object"}}`,
		`{"model":"qwen3.7-plus","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}}}`,
		`{"model":"qwen3.7-plus","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answers","strict":true,"schema":{"type":"array","items":{"type":"string"}}}}}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.qwen-openai-chat.v3", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[],"response_format":{"type":"json_object"}}`,
		`{"model":"qwen3.7-plus","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}}}`,
		`{"model":"qwen3.7-plus","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":false,"schema":{"type":"object"}}}}`,
		`{"model":"qwen3.7-plus","messages":[],"response_format":{"type":"yaml"}}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen37PlusExplicitPromptCacheRequestContract(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.7-plus.api", "1.5.0")
	marker := `{"type":"text","text":"long reusable prefix","cache_control":{"type":"ephemeral"}}`
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"system","content":[` + marker + `]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"system","content":[` + strings.Join([]string{marker, marker, marker, marker}, ",") + `]}]}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "cache-usage.qwen-context-cache.v1", evidence.CacheUsageProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[{"role":"system","content":[{"type":"text","text":"prefix","cache_control":{}}]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"system","content":[{"type":"text","text":"prefix","cache_control":{"type":"persistent"}}]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"system","content":[{"type":"text","text":"prefix","cache_control":{"type":"ephemeral","ttl":"5m"}}]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"system","content":[` + strings.Join([]string{marker, marker, marker, marker, marker}, ",") + `]}]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen37PlusImageAndVideoInputContract(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.7-plus.api", "1.6.0")
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.jpg"}},{"type":"text","text":"describe"}]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}},{"type":"text","text":"describe"}]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.com/video.mp4"}},{"type":"text","text":"summarize"}]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AA=="}},{"type":"text","text":"summarize"}]}]}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Equal(t, "protocol.qwen-openai-chat.v5", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://example.com/image.jpg"}}]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/gif;base64,AA=="}}]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"data:video/webm;base64,AA=="}}]}]}`,
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.com/video.mp4"},"fps":2}]}]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen37PlusBuiltinSearchRequestIsModeledButNotBillingAuthority(t *testing.T) {
	plan := executionProfilePlan(t, "qwen.3.7-plus.api", "1.7.0")
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[]}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_search":false}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_search":true}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_search":true,"search_options":{"forced_search":true,"search_strategy":"turbo"}}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_search":true,"search_options":{"search_strategy":"max"}}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
		require.Empty(t, evidence.ReservedMeterUsage, "Chat Completions response cannot prove whether Qwen executed search")
	}
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[],"enable_search":"true"}`,
		`{"model":"qwen3.7-plus","messages":[],"search_options":{"forced_search":true}}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_search":true,"search_options":"turbo"}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_search":true,"search_options":{"forced_search":"true"}}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_search":true,"search_options":{"search_strategy":"agent"}}`,
		`{"model":"qwen3.7-plus","messages":[],"enable_search":true,"search_options":{"enable_source":true}}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestQwen37FlashChatCapabilityMilestones(t *testing.T) {
	t.Run("text streaming and output", func(t *testing.T) {
		plan := executionProfilePlan(t, "qwen.3.7-flash.api", "1.1.0")
		for _, body := range []string{
			`{"model":"qwen3.7-flash","messages":[{"role":"user","content":"hello"}]}`,
			`{"model":"qwen3.7-flash","messages":[],"stream":true,"stream_options":{"include_usage":true}}`,
			`{"model":"qwen3.7-flash","messages":[],"max_completion_tokens":131072}`,
		} {
			evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
			require.NoError(t, err, body)
			require.Equal(t, "protocol.qwen-openai-chat.v1", evidence.ModelProtocolProfile)
		}
	})

	t.Run("reasoning", func(t *testing.T) {
		plan := executionProfilePlan(t, "qwen.3.7-flash.api", "1.2.0")
		evidence, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"qwen3.7-flash","messages":[],"enable_thinking":true,"thinking_budget":262144,"preserve_thinking":true}`,
		), plan)
		require.NoError(t, err)
		require.Equal(t, "reasoning.qwen3.7-hybrid.v1", evidence.ReasoningProfile)
		require.Equal(t, "262144", evidence.ReasoningBudgetRequest)
	})

	t.Run("function tools", func(t *testing.T) {
		plan := executionProfilePlan(t, "qwen.3.7-flash.api", "1.3.0")
		body := `{"model":"qwen3.7-flash","messages":[],"enable_thinking":false,"tools":[{"type":"function","function":{"name":"lookup-weather","description":"Look up weather"}}],"tool_choice":"required","parallel_tool_calls":true}`
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err)
	})

	t.Run("json object only", func(t *testing.T) {
		plan := executionProfilePlan(t, "qwen.3.7-flash.api", "1.4.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"qwen3.7-flash","messages":[{"role":"system","content":"Return JSON only"}],"response_format":{"type":"json_object"}}`,
		), plan)
		require.NoError(t, err)
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"qwen3.7-flash","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object"}}}}`,
		), plan)
		require.Error(t, err, "Qwen3.7 Flash is absent from the official JSON Schema support list")
	})

	t.Run("explicit cache and usage", func(t *testing.T) {
		plan := executionProfilePlan(t, "qwen.3.7-flash.api", "1.5.0")
		body := `{"model":"qwen3.7-flash","messages":[{"role":"system","content":[{"type":"text","text":"prefix","cache_control":{"type":"ephemeral"}}]}]}`
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err)
		require.Equal(t, "cache-usage.qwen-context-cache.v1", evidence.CacheUsageProfile)
	})

	t.Run("image and video", func(t *testing.T) {
		plan := executionProfilePlan(t, "qwen.3.7-flash.api", "1.6.0")
		for _, body := range []string{
			`{"model":"qwen3.7-flash","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.jpg"}}]}]}`,
			`{"model":"qwen3.7-flash","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AA=="}}]}]}`,
		} {
			_, err := validateChatExecutionProfileRequest([]byte(body), plan)
			require.NoError(t, err, body)
		}
	})

	t.Run("builtin search request without billing authority", func(t *testing.T) {
		plan := executionProfilePlan(t, "qwen.3.7-flash.api", "1.7.0")
		evidence, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"qwen3.7-flash","messages":[],"enable_search":true,"search_options":{"forced_search":true,"search_strategy":"turbo"}}`,
		), plan)
		require.NoError(t, err)
		require.Empty(t, evidence.ReservedMeterUsage)
	})
}

func TestGemini37FlashChatCapabilityMilestones(t *testing.T) {
	t.Run("text streaming and explicit output", func(t *testing.T) {
		plan := executionProfilePlan(t, "gemini.3.7-flash.api", "1.1.0")
		for _, body := range []string{
			`{"model":"gemini-3.7-flash","messages":[{"role":"user","content":"hello"}]}`,
			`{"model":"gemini-3.7-flash","messages":[],"stream":true,"stream_options":{"include_usage":true}}`,
			`{"model":"gemini-3.7-flash","messages":[],"max_completion_tokens":65536}`,
			`{"model":"gemini-3.7-flash","messages":[],"max_tokens":65536}`,
		} {
			evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
			require.NoError(t, err, body)
			require.Equal(t, "protocol.gemini-openai-chat.v1", evidence.ModelProtocolProfile)
		}
		for _, body := range []string{
			`{"model":"gemini-3.7-flash","messages":[],"max_completion_tokens":65537}`,
			`{"model":"gemini-3.7-flash","messages":[],"service_tier":"priority"}`,
		} {
			_, err := validateChatExecutionProfileRequest([]byte(body), plan)
			require.Error(t, err, body)
		}
	})

	t.Run("always on reasoning effort", func(t *testing.T) {
		plan := executionProfilePlan(t, "gemini.3.7-flash.api", "1.2.0")
		for _, effort := range []string{"low", "medium", "high"} {
			evidence, err := validateChatExecutionProfileRequest([]byte(
				`{"model":"gemini-3.7-flash","messages":[],"reasoning_effort":"`+effort+`"}`,
			), plan)
			require.NoError(t, err, effort)
			require.Equal(t, effort, evidence.ReasoningEffortEffective)
			require.Equal(t, "medium", evidence.ReasoningProviderDefault)
		}
		for _, effort := range []string{"minimal", "none", "xhigh"} {
			_, err := validateChatExecutionProfileRequest([]byte(
				`{"model":"gemini-3.7-flash","messages":[],"reasoning_effort":"`+effort+`"}`,
			), plan)
			require.Error(t, err, effort)
		}
	})

	t.Run("function tools with auto choice only", func(t *testing.T) {
		plan := executionProfilePlan(t, "gemini.3.7-flash.api", "1.3.0")
		tool := `{"type":"function","function":{"name":"lookup_weather","description":"Look up weather","parameters":{"type":"object"}}}`
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"gemini-3.7-flash","messages":[],"tools":[`+tool+`],"tool_choice":"auto"}`,
		), plan)
		require.NoError(t, err)
		for _, choice := range []string{`"none"`, `"required"`, `{"type":"function","function":{"name":"lookup_weather"}}`} {
			_, err = validateChatExecutionProfileRequest([]byte(
				`{"model":"gemini-3.7-flash","messages":[],"tools":[`+tool+`],"tool_choice":`+choice+`}`,
			), plan)
			require.Error(t, err, choice)
		}
	})

	t.Run("strict json schema", func(t *testing.T) {
		plan := executionProfilePlan(t, "gemini.3.7-flash.api", "1.4.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"gemini-3.7-flash","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}}}`,
		), plan)
		require.NoError(t, err)
		for _, responseFormat := range []string{
			`{"type":"json_object"}`,
			`{"type":"json_schema","json_schema":{"name":"answer","strict":false,"schema":{"type":"object"}}}`,
		} {
			_, err = validateChatExecutionProfileRequest([]byte(
				`{"model":"gemini-3.7-flash","messages":[],"response_format":`+responseFormat+`}`,
			), plan)
			require.Error(t, err, responseFormat)
		}
	})

	t.Run("inline jpeg", func(t *testing.T) {
		plan := executionProfilePlan(t, "gemini.3.7-flash.api", "1.5.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"gemini-3.7-flash","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,AA=="}}]}]}`,
		), plan)
		require.NoError(t, err)
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"gemini-3.7-flash","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}]}`,
		), plan)
		require.Error(t, err)
	})

	t.Run("inline wav audio", func(t *testing.T) {
		plan := executionProfilePlan(t, "gemini.3.7-flash.api", "1.6.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"gemini-3.7-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}}]}]}`,
		), plan)
		require.NoError(t, err)
		for _, inputAudio := range []string{
			`{"data":"AA==","format":"mp3"}`,
			`{"data":"not-base64","format":"wav"}`,
		} {
			_, err = validateChatExecutionProfileRequest([]byte(
				`{"model":"gemini-3.7-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":`+inputAudio+`}]}]}`,
			), plan)
			require.Error(t, err, inputAudio)
		}
	})

	t.Run("typed processing tier", func(t *testing.T) {
		plan := executionProfilePlan(t, "gemini.3.7-flash.api", "1.7.0")
		for _, test := range []struct {
			name      string
			field     string
			request   string
			effective string
			timeout   int
			explicit  bool
		}{
			{name: "omitted", request: "omitted", effective: modelprofile.ProcessingTierStandard, timeout: 300},
			{name: "default", field: `,"service_tier":"default"`, request: "default", effective: modelprofile.ProcessingTierStandard, timeout: 300, explicit: true},
			{name: "flex", field: `,"service_tier":"flex"`, request: "flex", effective: modelprofile.ProcessingTierFlex, timeout: 900, explicit: true},
			{name: "priority", field: `,"service_tier":"priority"`, request: "priority", effective: modelprofile.ProcessingTierPriority, timeout: 300, explicit: true},
		} {
			t.Run(test.name, func(t *testing.T) {
				evidence, err := validateChatExecutionProfileRequest([]byte(
					`{"model":"gemini-3.7-flash","messages":[]`+test.field+`}`,
				), plan)
				require.NoError(t, err)
				require.Equal(t, modelprofile.ProcessingTierPolicyGeminiOpenAICompatibilityV1, evidence.ProcessingTierPolicy)
				require.Equal(t, test.request, evidence.ProcessingTierRequest)
				require.Equal(t, test.effective, evidence.ProcessingTierEffective)
				require.Equal(t, test.timeout, evidence.ProcessingTierTimeoutSeconds)
				require.Equal(t, test.explicit, evidence.ProcessingTierExplicit)
				require.Equal(t, test.field != "", evidence.ProcessingTierFieldPresent)
			})
		}
		for _, field := range []string{
			`"standard"`, `"auto"`, `"fast"`, `" flex "`, `""`, `null`, `1`,
		} {
			_, err := validateChatExecutionProfileRequest([]byte(
				`{"model":"gemini-3.7-flash","messages":[],"service_tier":`+field+`}`,
			), plan)
			require.Error(t, err, field)
		}
	})
}

func TestLegacyGenericProcessingTierGrammarCannotBypassCustomerChargeBoundary(t *testing.T) {
	plan := executionProfilePlan(t, "deepseek.v4-pro.api", "1.1.0")
	evidence, err := validateChatExecutionProfileRequest([]byte(
		`{"model":"deepseek-v4-pro","messages":[],"service_tier":"legacy"}`,
	), plan)
	require.NoError(t, err)
	require.True(t, evidence.ProcessingTierFieldPresent)
	require.Empty(t, evidence.ProcessingTierPolicy)

	relay := &Relay{}
	require.ErrorIs(t,
		relay.validateProcessingTierBillingReadiness(t.Context(), "public-model", evidence),
		errProcessingTierContractUnavailable,
	)
}

func TestGemini37ProcessingTierSurvivesFullAdmissionAndFailsClosedBeforeSecretUse(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	newRelay := func(t *testing.T, inspector BillingAdmissionReadinessInspector, encryptedSecret string) *Relay {
		t.Helper()
		plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://generativelanguage.googleapis.com/v1beta/openai", encKey, nil)
		setExecutionTestProfile(t, &plan, "gemini.3.7-flash.api", "1.7.0")
		plan.Routes[0].Provider = "gemini"
		plan.Routes[0].UpstreamModel = "gemini-3.7-flash"
		if encryptedSecret != "" {
			plan.Routes[0].EncryptedSecret = encryptedSecret
		}
		relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
		relay.SetBillingAdmissionReadinessInspector(inspector)
		require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
		return relay
	}

	t.Run("cost observation preserves provider payload", func(t *testing.T) {
		inspector := &processingTierReadinessStub{result: billing.AdmissionReadiness{BillingMode: billing.BillingModeCostObservation}}
		relay := newRelay(t, inspector, "")
		explicitOutput := 128
		batch, err := relay.prepareModelExecutionRoutes(
			context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
			"public-model", "public-model", "req-gemini-flex",
			modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
			[]byte(`{"model":"public-model","messages":[],"service_tier":"flex","max_completion_tokens":128}`), time.Now(),
		)
		require.NoError(t, err)
		require.Equal(t, 1, inspector.calls)
		require.Equal(t, 900, batch.profileEvidence.ProcessingTierTimeoutSeconds)
		actual, bodyErr := providerRequestBody(batch.routes[0].upstreamRequest)
		require.NoError(t, bodyErr)
		var payload map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(actual, &payload))
		require.JSONEq(t, `"flex"`, string(payload["service_tier"]))
	})

	t.Run("prepaid blocks before decrypt", func(t *testing.T) {
		inspector := &processingTierReadinessStub{result: billing.AdmissionReadiness{BillingMode: billing.BillingModePrepaidEnforced}}
		relay := newRelay(t, inspector, "not-ciphertext")
		explicitOutput := 128
		batch, err := relay.prepareModelExecutionRoutes(
			context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
			"public-model", "public-model", "req-gemini-priority",
			modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
			[]byte(`{"model":"public-model","messages":[],"service_tier":"priority","max_completion_tokens":128}`), time.Now(),
		)
		require.Nil(t, batch)
		require.ErrorIs(t, err, errProcessingTierContractUnavailable)
		require.Equal(t, 1, inspector.calls)
	})

	t.Run("missing readiness blocks before decrypt", func(t *testing.T) {
		relay := newRelay(t, nil, "not-ciphertext")
		explicitOutput := 128
		batch, err := relay.prepareModelExecutionRoutes(
			context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
			"public-model", "public-model", "req-gemini-flex-state",
			modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
			[]byte(`{"model":"public-model","messages":[],"service_tier":"flex","max_completion_tokens":128}`), time.Now(),
		)
		require.Nil(t, batch)
		require.ErrorIs(t, err, errProcessingTierStateUnavailable)
	})

	t.Run("readiness failure blocks before decrypt", func(t *testing.T) {
		inspector := &processingTierReadinessStub{err: errors.New("readiness unavailable")}
		relay := newRelay(t, inspector, "not-ciphertext")
		explicitOutput := 128
		batch, err := relay.prepareModelExecutionRoutes(
			context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
			"public-model", "public-model", "req-gemini-flex-readiness",
			modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
			[]byte(`{"model":"public-model","messages":[],"service_tier":"flex","max_completion_tokens":128}`), time.Now(),
		)
		require.Nil(t, batch)
		require.ErrorIs(t, err, errProcessingTierStateUnavailable)
		require.Equal(t, 1, inspector.calls)
	})

	t.Run("default remains standard without billing mode lookup", func(t *testing.T) {
		relay := newRelay(t, nil, "")
		explicitOutput := 128
		batch, err := relay.prepareModelExecutionRoutes(
			context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
			"public-model", "public-model", "req-gemini-default",
			modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
			[]byte(`{"model":"public-model","messages":[],"service_tier":"default","max_completion_tokens":128}`), time.Now(),
		)
		require.NoError(t, err)
		require.Equal(t, modelprofile.ProcessingTierStandard, batch.profileEvidence.ProcessingTierEffective)
	})
}

func TestGemini35FlashChatCapabilityMilestones(t *testing.T) {
	tests := []struct {
		name    string
		version string
		body    string
	}{
		{
			name: "stream and output", version: "1.1.0",
			body: `{"model":"gemini-3.5-flash","messages":[],"stream":true,"max_completion_tokens":65536}`,
		},
		{
			name: "minimal reasoning", version: "1.2.0",
			body: `{"model":"gemini-3.5-flash","messages":[],"reasoning_effort":"minimal"}`,
		},
		{
			name: "function tool auto", version: "1.3.0",
			body: `{"model":"gemini-3.5-flash","messages":[],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto"}`,
		},
		{
			name: "strict json schema", version: "1.4.0",
			body: `{"model":"gemini-3.5-flash","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object"}}}}`,
		},
		{
			name: "jpeg image", version: "1.5.0",
			body: `{"model":"gemini-3.5-flash","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,AA=="}}]}]}`,
		},
		{
			name: "wav audio", version: "1.6.0",
			body: `{"model":"gemini-3.5-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}}]}]}`,
		},
		{
			name: "priority tier", version: "1.7.0",
			body: `{"model":"gemini-3.5-flash","messages":[],"service_tier":"priority"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := executionProfilePlan(t, "gemini.3.5-flash.api", test.version)
			_, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}

	current := executionProfilePlan(t, "gemini.3.5-flash.api", "1.7.0")
	for _, body := range []string{
		`{"model":"gemini-3.5-flash","messages":[],"reasoning_effort":"xhigh"}`,
		`{"model":"gemini-3.5-flash","messages":[],"service_tier":"auto"}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), current)
		require.Error(t, err, body)
	}

	historical := executionProfilePlan(t, "gemini.3.5-flash.api", "1.6.0")
	_, err := validateChatExecutionProfileRequest([]byte(
		`{"model":"gemini-3.5-flash","messages":[],"service_tier":"priority"}`,
	), historical)
	require.ErrorContains(t, err, "service_tier is not supported")
}

func TestGemini35ProfilesProcessingTierSurvivesFullAdmission(t *testing.T) {
	for _, profile := range []struct {
		id, model string
	}{
		{id: "gemini.3.5-flash.api", model: "gemini-3.5-flash"},
		{id: "gemini.3.5-flash-lite.api", model: "gemini-3.5-flash-lite"},
	} {
		t.Run(profile.model, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions,
				"https://generativelanguage.googleapis.com/v1beta/openai", encKey, nil)
			setExecutionTestProfile(t, &plan, profile.id, "1.7.0")
			plan.Routes[0].Provider = "gemini"
			plan.Routes[0].UpstreamModel = profile.model

			inspector := &processingTierReadinessStub{
				result: billing.AdmissionReadiness{BillingMode: billing.BillingModeCostObservation},
			}
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
			relay.SetBillingAdmissionReadinessInspector(inspector)
			require.NoError(t, relay.SetModelExecutionRuntime(
				modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
			))
			explicitOutput := 128
			batch, err := relay.prepareModelExecutionRoutes(
				context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
				"public-model", "public-model", "req-gemini-35-flex",
				modelExecutionRequestLimits{
					EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput,
					EstimateVersion: chatAdmissionEstimateVersion,
				},
				[]byte(`{"model":"public-model","messages":[],"service_tier":"flex","max_completion_tokens":128}`),
				time.Now(),
			)
			require.NoError(t, err)
			require.Equal(t, 1, inspector.calls)
			require.Equal(t, modelprofile.ProcessingTierPolicyGeminiOpenAICompatibilityV1, batch.profileEvidence.ProcessingTierPolicy)
			require.Equal(t, modelprofile.ProcessingTierFlex, batch.profileEvidence.ProcessingTierEffective)
			require.Equal(t, 900, batch.profileEvidence.ProcessingTierTimeoutSeconds)
			actual, bodyErr := providerRequestBody(batch.routes[0].upstreamRequest)
			require.NoError(t, bodyErr)
			var payload map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(actual, &payload))
			require.JSONEq(t, `"flex"`, string(payload["service_tier"]))
		})
	}
}

func TestGemini35FlashLiteChatCapabilityMilestones(t *testing.T) {
	plan := executionProfilePlan(t, "gemini.3.5-flash-lite.api", "1.7.0")
	evidence, err := validateChatExecutionProfileRequest([]byte(
		`{"model":"gemini-3.5-flash-lite","messages":[],"stream":true,"max_tokens":65536,"reasoning_effort":"minimal","tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto","response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object"}}}}`,
	), plan)
	require.NoError(t, err)
	require.Equal(t, "minimal", evidence.ReasoningEffortEffective)
	require.Equal(t, "minimal", evidence.ReasoningProviderDefault)
	require.Equal(t, modelprofile.ProcessingTierPolicyGeminiOpenAICompatibilityV1, evidence.ProcessingTierPolicy)

	for _, body := range []string{
		`{"model":"gemini-3.5-flash-lite","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,AA=="}}]}]}`,
		`{"model":"gemini-3.5-flash-lite","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}}]}]}`,
	} {
		_, err = validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
	}
	for _, body := range []string{
		`{"model":"gemini-3.5-flash-lite","messages":[],"reasoning_effort":"none"}`,
		`{"model":"gemini-3.5-flash-lite","messages":[],"tool_choice":"none"}`,
		`{"model":"gemini-3.5-flash-lite","messages":[],"response_format":{"type":"json_object"}}`,
	} {
		_, err = validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
	historical := executionProfilePlan(t, "gemini.3.5-flash-lite.api", "1.6.0")
	_, err = validateChatExecutionProfileRequest([]byte(
		`{"model":"gemini-3.5-flash-lite","messages":[],"service_tier":"flex"}`,
	), historical)
	require.ErrorContains(t, err, "service_tier is not supported")
}

func TestGeminiCurrentProfilesFailClosedOnUnmodeledOfficialFeatures(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	profiles := []struct {
		id      string
		version string
		model   string
	}{
		{id: "gemini.3.7-flash.api", version: "1.7.0", model: "gemini-3.7-flash"},
		{id: "gemini.3.5-flash.api", version: "1.7.0", model: "gemini-3.5-flash"},
		{id: "gemini.3.5-flash-lite.api", version: "1.7.0", model: "gemini-3.5-flash-lite"},
	}
	for _, profile := range profiles {
		t.Run(profile.model, func(t *testing.T) {
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.invalid", encKey, nil)
			setExecutionTestProfile(t, &plan, profile.id, profile.version)
			plan.Routes[0].EncryptedSecret = "not-ciphertext"
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
			require.NoError(t, relay.SetModelExecutionRuntime(
				modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
			))
			unsupported := map[string]string{
				"explicit cache":   `"messages":[],"extra_body":{"google":{"cached_content":"cachedContents/example"}}`,
				"search grounding": `"messages":[],"tools":[{"google_search":{}}]`,
				"video":            `"messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AA=="}}]}]`,
				"PDF":              `"messages":[{"role":"user","content":[{"type":"file","file":{"file_data":"data:application/pdf;base64,AA=="}}]}]`,
			}
			selectedProfile, ok := modelprofile.Lookup(profile.id, profile.version)
			require.True(t, ok)
			if selectedProfile.ProcessingTierPolicy == "" {
				unsupported["priority tier"] = `"messages":[],"service_tier":"priority"`
			}
			for name, suffix := range unsupported {
				body := []byte(`{"model":"` + profile.model + `",` + suffix + `}`)
				explicitOutput := 128
				batch, err := relay.prepareModelExecutionRoutes(
					context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
					"public-model", "public-model", "req-gemini-unmodeled-"+name,
					modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
					body, time.Now(),
				)
				require.Nil(t, batch, name)
				require.Error(t, err, name)
				require.NotContains(t, err.Error(), "decrypt", name)
			}
		})
	}
}

func TestMiMoV25ProChatCapabilityMilestones(t *testing.T) {
	t.Run("stream and exact output field", func(t *testing.T) {
		plan := executionProfilePlan(t, "mimo.v2.5-pro.api", "1.1.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"mimo-v2.5-pro","messages":[],"stream":true,"max_completion_tokens":131072}`,
		), plan)
		require.NoError(t, err)
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"mimo-v2.5-pro","messages":[],"max_tokens":131072}`,
		), plan)
		require.Error(t, err)
	})

	t.Run("thinking toggle and fixed sampling", func(t *testing.T) {
		plan := executionProfilePlan(t, "mimo.v2.5-pro.api", "1.2.0")
		evidence, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"mimo-v2.5-pro","messages":[],"thinking":{"type":"enabled"},"temperature":1,"top_p":0.95}`,
		), plan)
		require.NoError(t, err)
		require.Equal(t, "enabled", evidence.ReasoningToggleRequest)
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"mimo-v2.5-pro","messages":[],"temperature":0.8}`,
		), plan)
		require.Error(t, err)
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"mimo-v2.5-pro","messages":[],"thinking":{"type":"disabled"},"temperature":0.8}`,
		), plan)
		require.NoError(t, err)
	})

	t.Run("strict function tool with auto choice", func(t *testing.T) {
		plan := executionProfilePlan(t, "mimo.v2.5-pro.api", "1.3.0")
		tool := `{"type":"function","function":{"name":"lookup-weather","strict":true,"parameters":{"type":"object"}}}`
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"mimo-v2.5-pro","messages":[],"tools":[`+tool+`],"tool_choice":"auto"}`,
		), plan)
		require.NoError(t, err)
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"mimo-v2.5-pro","messages":[],"tools":[`+tool+`],"tool_choice":"required"}`,
		), plan)
		require.Error(t, err)
	})

	t.Run("json object only", func(t *testing.T) {
		plan := executionProfilePlan(t, "mimo.v2.5-pro.api", "1.4.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"mimo-v2.5-pro","messages":[{"role":"user","content":"Return a JSON object"}],"response_format":{"type":"json_object"}}`,
		), plan)
		require.NoError(t, err)
		for _, body := range []string{
			`{"model":"mimo-v2.5-pro","messages":[],"response_format":{"type":"json_object"}}`,
			`{"model":"mimo-v2.5-pro","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"x","strict":true,"schema":{"type":"object"}}}}`,
		} {
			_, err = validateChatExecutionProfileRequest([]byte(body), plan)
			require.Error(t, err, body)
		}
	})

	t.Run("cache profile and unsupported extras", func(t *testing.T) {
		plan := executionProfilePlan(t, "mimo.v2.5-pro.api", "1.5.0")
		evidence, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"mimo-v2.5-pro","messages":[]}`,
		), plan)
		require.NoError(t, err)
		require.Equal(t, "cache-usage.mimo-openai.v1", evidence.CacheUsageProfile)
		for _, body := range []string{
			`{"model":"mimo-v2.5-pro","messages":[],"service_tier":"priority"}`,
			`{"model":"mimo-v2.5-pro","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,AA=="}}]}]}`,
		} {
			_, err = validateChatExecutionProfileRequest([]byte(body), plan)
			require.Error(t, err, body)
		}
	})
}

func TestMiMoV25OmniModalChatShapes(t *testing.T) {
	plan := executionProfilePlan(t, "mimo.v2.5.api", "1.6.0")
	for _, body := range []string{
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.jpg"}}]}]}`,
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}]}`,
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA=="}}]}]}`,
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"https://example.com/audio.wav"}}]}]}`,
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.com/video.mp4"},"fps":0.1,"media_resolution":"default"}]}]}`,
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AA=="},"fps":10,"media_resolution":"max"}]}]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err, body)
	}
	for _, body := range []string{
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"http://example.com/audio.wav"}}]}]}`,
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}}]}]}`,
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.com/video.mp4"},"fps":0.09}]}]}`,
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.com/video.mp4"},"media_resolution":"high"}]}]}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestHy3ChatCapabilityMilestones(t *testing.T) {
	t.Run("stream and provider maximum output", func(t *testing.T) {
		plan := executionProfilePlan(t, "hy3.api", "1.1.0")
		for _, field := range []string{"max_tokens", "max_completion_tokens"} {
			_, err := validateChatExecutionProfileRequest([]byte(
				`{"model":"hy3","messages":[],"stream":true,"`+field+`":131072}`,
			), plan)
			require.NoError(t, err, field)
		}
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"hy3","messages":[],"max_tokens":131073}`,
		), plan)
		require.Error(t, err)
	})

	t.Run("effort enables reasoning and tool low maps to provider high", func(t *testing.T) {
		plan := executionProfilePlan(t, "hy3.api", "1.2.0")
		evidence, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"hy3","messages":[],"reasoning_effort":"medium"}`,
		), plan)
		require.NoError(t, err)
		require.Equal(t, "medium", evidence.ReasoningEffortEffective)

		plan = executionProfilePlan(t, "hy3.api", "1.3.0")
		evidence, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"hy3","messages":[],"reasoning_effort":"low","tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto"}`,
		), plan)
		require.NoError(t, err)
		require.Equal(t, "low", evidence.ReasoningEffortRequest)
		require.Equal(t, "high", evidence.ReasoningEffortEffective)
		for _, body := range []string{
			`{"model":"hy3","messages":[],"reasoning_effort":"max"}`,
			`{"model":"hy3","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":"required"}`,
			`{"model":"hy3","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
		} {
			_, err = validateChatExecutionProfileRequest([]byte(body), plan)
			require.Error(t, err, body)
		}
	})

	t.Run("json schema requires provider root constraint", func(t *testing.T) {
		plan := executionProfilePlan(t, "hy3.api", "1.4.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"hy3","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object","additionalProperties":false}}}}`,
		), plan)
		require.NoError(t, err)
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"hy3","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}}}`,
		), plan)
		require.ErrorContains(t, err, "additionalProperties")
	})

	t.Run("cache evidence and exact search request shape", func(t *testing.T) {
		plan := executionProfilePlan(t, "hy3.api", "1.6.0")
		evidence, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"hy3","messages":[],"web_search_options":{"enable":true,"search_source":"lite","user_location":{"type":"approximate","country":"CN","city":"Shenzhen","timezone":"Asia/Shanghai"}}}`,
		), plan)
		require.NoError(t, err)
		require.Equal(t, "cache-usage.hy3-openai.v1", evidence.CacheUsageProfile)
		require.Empty(t, evidence.ReservedMeterUsage, "search price is not frozen by the token-only draft")
		for _, body := range []string{
			`{"model":"hy3","messages":[],"web_search_options":{"enable":false}}`,
			`{"model":"hy3","messages":[],"web_search_options":{"enable":true,"search_source":"max"}}`,
			`{"model":"hy3","messages":[],"web_search_options":{"enable":true,"user_location":{"type":"precise"}}}`,
		} {
			_, err = validateChatExecutionProfileRequest([]byte(body), plan)
			require.Error(t, err, body)
		}
	})
}

func TestGPT56ChatCapabilityMilestones(t *testing.T) {
	t.Run("stream and provider maximum output", func(t *testing.T) {
		plan := executionProfilePlan(t, "openai.gpt-5.6-sol.api", "1.1.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"gpt-5.6-sol","messages":[],"stream":true,"max_completion_tokens":128000}`,
		), plan)
		require.NoError(t, err)
		for _, body := range []string{
			`{"model":"gpt-5.6-sol","messages":[],"max_completion_tokens":128001}`,
			`{"model":"gpt-5.6-sol","messages":[],"max_tokens":128000}`,
		} {
			_, err = validateChatExecutionProfileRequest([]byte(body), plan)
			require.Error(t, err, body)
		}
	})

	t.Run("reasoning effort", func(t *testing.T) {
		plan := executionProfilePlan(t, "openai.gpt-5.6-sol.api", "1.2.0")
		for _, effort := range []string{"none", "low", "medium", "high", "xhigh", "max"} {
			evidence, err := validateChatExecutionProfileRequest([]byte(
				`{"model":"gpt-5.6-sol","messages":[],"reasoning_effort":"`+effort+`"}`,
			), plan)
			require.NoError(t, err, effort)
			effective := effort
			if effort == "none" {
				effective = "disabled"
			}
			require.Equal(t, effective, evidence.ReasoningEffortEffective)
		}
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"gpt-5.6-sol","messages":[],"reasoning":{"mode":"pro"}}`,
		), plan)
		require.Error(t, err, "Responses-only pro mode must not leak into the Chat profile")
	})

	t.Run("function tools and structured output", func(t *testing.T) {
		toolsPlan := executionProfilePlan(t, "openai.gpt-5.6-sol.api", "1.3.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"gpt-5.6-sol","messages":[],"parallel_tool_calls":true,"tool_choice":"required","tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"},"strict":true}}]}`,
		), toolsPlan)
		require.NoError(t, err)
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"gpt-5.6-sol","messages":[],"tools":[{"type":"function","function":{"name":"ping"}}]}`,
		), toolsPlan)
		require.NoError(t, err, "OpenAI permits parameters to be omitted for no-argument functions")
		_, err = estimateChatAdmissionForPlan([]byte(
			`{"model":"gpt-5.6-sol","messages":[],"tools":[{"type":"function","function":{"name":"ping"}}]}`,
		), toolsPlan)
		require.NoError(t, err)
		for _, parameters := range []string{"null", "true", "false", `[]`, `"object"`} {
			body := []byte(`{"model":"gpt-5.6-sol","messages":[],"tools":[{"type":"function","function":{"name":"ping","parameters":` + parameters + `}}]}`)
			_, err = estimateChatAdmissionForPlan(body, toolsPlan)
			require.ErrorContains(t, err, "function parameters must be a JSON schema object", parameters)
		}

		structuredPlan := executionProfilePlan(t, "openai.gpt-5.6-sol.api", "1.4.0")
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"gpt-5.6-sol","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object","properties":{}}}}}`,
		), structuredPlan)
		require.NoError(t, err)
	})

	t.Run("vision detail", func(t *testing.T) {
		plan := executionProfilePlan(t, "openai.gpt-5.6-sol.api", "1.5.0")
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"gpt-5.6-sol","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png","detail":"original"}}]}]}`,
		), plan)
		require.NoError(t, err)
		_, err = validateChatExecutionProfileRequest([]byte(
			`{"model":"gpt-5.6-sol","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png","detail":"ultra"}}]}]}`,
		), plan)
		require.Error(t, err)
	})

	t.Run("explicit cache request passes the full admission path", func(t *testing.T) {
		plan := executionProfilePlan(t, "openai.gpt-5.6-sol.api", "1.6.0")
		body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"support-v1","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"messages":[{"role":"system","content":[{"type":"text","text":"stable prefix","prompt_cache_breakpoint":{"mode":"explicit"}}]}]}`)
		_, err := validateChatExecutionProfileRequest(body, plan)
		require.NoError(t, err)
		_, err = estimateChatAdmissionForPlan(body, plan)
		require.NoError(t, err)
		nullBreakpoint := []byte(`{"model":"gpt-5.6-sol","prompt_cache_options":null,"messages":[{"role":"system","content":[{"type":"text","text":"stable prefix","prompt_cache_breakpoint":null}]}]}`)
		_, err = validateChatExecutionProfileRequest(nullBreakpoint, plan)
		require.NoError(t, err)
		_, err = estimateChatAdmissionForPlan(nullBreakpoint, plan)
		require.NoError(t, err)

		invalid := []byte(`{"model":"gpt-5.6-sol","prompt_cache_options":{"mode":"explicit","ttl":"1h"},"messages":[]}`)
		_, err = validateChatExecutionProfileRequest(invalid, plan)
		require.Error(t, err)
	})

	t.Run("unpriced service tiers remain documented-only", func(t *testing.T) {
		plan := executionProfilePlan(t, "openai.gpt-5.6-sol.chat", "1.0.0")
		require.Equal(t, "openai.gpt-5.6-sol.chat", plan.ExecutionProfileID)
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"gpt-5.6-sol","messages":[],"service_tier":"priority"}`,
		), plan)
		require.ErrorContains(t, err, "service_tier is not supported")
	})
}

func TestGLM53JSONObjectAcceptsOnlyOfficialResponseFormats(t *testing.T) {
	plan := executionProfilePlan(t, "glm.5.3.api", "1.6.0")
	for _, body := range []string{
		`{"model":"glm-5.3","messages":[]}`,
		`{"model":"glm-5.3","messages":[],"response_format":{"type":"text"}}`,
		`{"model":"glm-5.3","messages":[],"response_format":{"type":"json_object"}}`,
	} {
		evidence, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.NoError(t, err)
		require.Equal(t, "protocol.glm-openai-chat.v5", evidence.ModelProtocolProfile)
	}
	for _, body := range []string{
		`{"model":"glm-5.3","messages":[],"response_format":null}`,
		`{"model":"glm-5.3","messages":[],"response_format":"json_object"}`,
		`{"model":"glm-5.3","messages":[],"response_format":{}}`,
		`{"model":"glm-5.3","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}}}`,
		`{"model":"glm-5.3","messages":[],"response_format":{"type":"json_object","schema":{"type":"object"}}}`,
		`{"model":"glm-5.3","messages":[],"guided_json":{"type":"object"}}`,
	} {
		_, err := validateChatExecutionProfileRequest([]byte(body), plan)
		require.Error(t, err, body)
	}
}

func TestKimiK26ThinkingProfileAppliesCombinationRules(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.6.api", "1.0.0")
	tests := []struct {
		name    string
		body    string
		request string
		wantErr bool
	}{
		{name: "provider default enabled rejects required", body: `{"model":"kimi-k2.6","messages":[],"tool_choice":"required"}`, wantErr: true},
		{name: "explicit enabled rejects required", body: `{"model":"kimi-k2.6","messages":[],"thinking":{"type":"enabled"},"tool_choice":"required"}`, wantErr: true},
		{name: "disabled permits required", body: `{"model":"kimi-k2.6","messages":[],"thinking":{"type":"disabled"},"tool_choice":"required"}`, request: "disabled"},
		{name: "enabled auto", body: `{"model":"kimi-k2.6","messages":[],"thinking":{"type":"enabled"},"tool_choice":"auto"}`, request: "enabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "tool_choice=required") {
					t.Fatalf("expected combination error, got %v", err)
				}
				return
			}
			if err != nil || evidence.ReasoningRequest != test.request {
				t.Fatalf("evidence=%+v err=%v", evidence, err)
			}
		})
	}
}

func TestKimiK26CurrentProfileValidatesKeepToolChoicesAndFixedParameters(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.6.api", "1.7.0")

	evidence, err := validateChatExecutionProfileRequest([]byte(`{
		"model":"kimi-k2.6","messages":[],
		"thinking":{"type":"enabled","keep":"all"},
		"tools":[{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}],
		"tool_choice":"auto","temperature":1,"top_p":0.950,"n":1,
		"presence_penalty":0.0,"frequency_penalty":0
	}`), plan)
	require.NoError(t, err)
	require.Equal(t, "enabled", evidence.ReasoningToggleRequest)
	require.Equal(t, "all", evidence.ReasoningThinkingKeepRequest)
	require.Equal(t, "null", evidence.ReasoningThinkingKeepDefault)

	for _, test := range []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "keep requires enabled thinking",
			body:    `{"model":"kimi-k2.6","messages":[],"thinking":{"type":"disabled","keep":"all"}}`,
			wantErr: "thinking.keep requires thinking.type=enabled",
		},
		{
			name:    "required is unsupported even with thinking disabled",
			body:    `{"model":"kimi-k2.6","messages":[],"thinking":{"type":"disabled"},"tool_choice":"required"}`,
			wantErr: "tool_choice must be one of none, auto",
		},
		{
			name:    "disabled thinking uses non-thinking temperature",
			body:    `{"model":"kimi-k2.6","messages":[],"thinking":{"type":"disabled"},"temperature":1}`,
			wantErr: "temperature must be 0.6",
		},
		{
			name:    "thinking temperature is fixed",
			body:    `{"model":"kimi-k2.6","messages":[],"temperature":0.6}`,
			wantErr: "temperature must be 1",
		},
		{
			name:    "top p is fixed",
			body:    `{"model":"kimi-k2.6","messages":[],"top_p":0.9}`,
			wantErr: "top_p must be 0.95",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, validateErr := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.ErrorContains(t, validateErr, test.wantErr)
		})
	}

	evidence, err = validateChatExecutionProfileRequest([]byte(`{
		"model":"kimi-k2.6","messages":[],"thinking":{"type":"disabled"},
		"tools":[{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}],
		"tool_choice":"auto","temperature":0.60,"top_p":0.95,"n":1,
		"presence_penalty":0,"frequency_penalty":0
	}`), plan)
	require.NoError(t, err)
	require.Equal(t, "disabled", evidence.ReasoningToggleRequest)
}

func TestKimiK27PlatformProfileValidatesRequiredThinkingAndFixedParameters(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.7-code.api", "1.3.0")

	evidence, err := validateChatExecutionProfileRequest([]byte(`{"model":"kimi-k2.7-code","messages":[]}`), plan)
	require.NoError(t, err)
	require.Equal(t, "omitted", evidence.ReasoningToggleRequest)
	require.Equal(t, "omitted", evidence.ReasoningThinkingKeepRequest)
	require.Equal(t, "all", evidence.ReasoningThinkingKeepDefault)

	evidence, err = validateChatExecutionProfileRequest([]byte(`{
		"model":"kimi-k2.7-code","messages":[],
		"thinking":{"type":"enabled","keep":"all"},
		"tools":[{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}],"tool_choice":"auto",
		"temperature":1.0,"top_p":0.95,"n":1,"presence_penalty":0,"frequency_penalty":0
	}`), plan)
	require.NoError(t, err)
	require.Equal(t, "all", evidence.ReasoningThinkingKeepRequest)

	for _, test := range []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "thinking cannot be disabled",
			body:    `{"model":"kimi-k2.7-code","messages":[],"thinking":{"type":"disabled"}}`,
			wantErr: "thinking must be one of enabled",
		},
		{
			name:    "required tool choice is unsupported",
			body:    `{"model":"kimi-k2.7-code","messages":[],"tool_choice":"required"}`,
			wantErr: "tool_choice must be one of none, auto",
		},
		{
			name:    "named tool choice is unsupported",
			body:    `{"model":"kimi-k2.7-code","messages":[],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
			wantErr: "named tool_choice is not supported",
		},
		{
			name:    "temperature is fixed",
			body:    `{"model":"kimi-k2.7-code","messages":[],"temperature":0.6}`,
			wantErr: "temperature must be 1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, validateErr := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.ErrorContains(t, validateErr, test.wantErr)
		})
	}
}

func TestKimiK3CurrentProfileValidatesFixedParameters(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k3.api", "1.6.0")
	evidence, err := validateChatExecutionProfileRequest([]byte(`{
		"model":"kimi-k3","messages":[],"reasoning_effort":"low","tool_choice":"required",
		"temperature":1,"top_p":0.95,"n":1,"presence_penalty":0,"frequency_penalty":0
	}`), plan)
	require.NoError(t, err)
	require.Equal(t, "low", evidence.ReasoningEffortRequest)
	require.Equal(t, "low", evidence.ReasoningEffortEffective)

	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"kimi-k3","messages":[],"n":2}`), plan)
	require.ErrorContains(t, err, "n must be 1")
}

func TestKimiProfileValidationPrecedesSecretDecryption(t *testing.T) {
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.invalid", []byte("0123456789abcdef0123456789abcdef"), nil)
	setExecutionTestProfile(t, &plan, "kimi.k3.api", "1.1.0")
	plan.Routes[0].EncryptedSecret = "not-ciphertext"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, []byte("0123456789abcdef0123456789abcdef"))
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-k3-profile-pre-secret",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"public-model","messages":[],"reasoning_effort":"medium","max_completion_tokens":128}`), time.Now(),
	)
	if batch != nil || !errors.Is(err, errModelExecutionProfileRequestInvalid) {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
}

func TestKimiK3ProviderDefaultIsNotInjectedIntoUpstreamRequest(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.test", encKey, nil)
	setExecutionTestProfile(t, &plan, "kimi.k3.api", "1.1.0")
	plan.Routes[0].Provider = "moonshot"
	plan.Routes[0].UpstreamModel = "kimi-k3"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-k3-default",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":128}`), time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(actual, &payload); err != nil {
		t.Fatal(err)
	}
	if _, injected := payload["reasoning_effort"]; injected {
		t.Fatalf("provider default was injected: %s", actual)
	}
	if batch.profileEvidence.ReasoningRequest != "omitted" || batch.profileEvidence.ReasoningProviderDefault != "max" {
		t.Fatalf("unexpected profile evidence: %+v", batch.profileEvidence)
	}
}

func TestKimiAdvancedProtocolIsPreservedIntoProviderRequestAndFreezeEvidence(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	tests := []struct {
		name           string
		profileID      string
		profileVersion string
		body           string
		required       []string
		wantParallel   bool
		wantReserved   []metering.MeterQuantity
	}{
		{
			name: "K2.6 builtin search with thinking", profileID: "kimi.k2.6.api", profileVersion: "1.7.0",
			body:     `{"model":"public-model","messages":[{"role":"user","content":"latest"}],"thinking":{"type":"disabled"},"tools":[{"type":"builtin_function","function":{"name":"$web_search"}}],"max_completion_tokens":128}`,
			required: []string{modelCapabilityBuiltinWebSearch, modelCapabilityReasoning, modelCapabilityTools}, wantParallel: true,
			wantReserved: []metering.MeterQuantity{{Meter: "builtin_web_search_calls", Quantity: 1}},
		},
		{
			name: "K2.6 preserved exact non-stream tool history", profileID: "kimi.k2.6.api", profileVersion: "1.7.0",
			body:     `{"model":"public-model","messages":[{"role":"user","content":"weather"},{"role":"assistant","content":null,"reasoning_content":"need weather","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Shanghai\"}"}}]},{"role":"tool","tool_call_id":"call_1","name":"lookup","content":"{\"temperature\":20}"}],"thinking":{"type":"enabled"},"tool_choice":"auto","tools":[{"type":"function","function":{"name":"lookup","description":"Look up weather","parameters":{"type":"object"}}}],"max_completion_tokens":128}`,
			required: []string{modelprofile.CapabilityFunctionTools, modelCapabilityReasoning, modelprofile.CapabilityToolChoiceAuto, modelCapabilityTools},
		},
		{
			name: "K2.6 inline image and partial", profileID: "kimi.k2.6.api", profileVersion: "1.7.0",
			body:     `{"model":"public-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aA=="}}]},{"role":"assistant","content":"prefix","partial":true}],"max_completion_tokens":128}`,
			required: []string{modelCapabilityPartialMode, modelCapabilityReasoning, modelCapabilityVision},
		},
		{
			name: "K3 dynamic system tools", profileID: "kimi.k3.api", profileVersion: "1.6.0",
			body:     `{"model":"public-model","messages":[{"role":"system","tools":[{"type":"function","function":{"name":"lookup","description":"Look up a value","parameters":{"type":"object"}}}]},{"role":"user","content":"hello"}],"max_completion_tokens":128}`,
			required: []string{modelCapabilityDynamicTools, modelCapabilityReasoning, modelCapabilityTools},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := executionTestPlan(t, modelrouting.ModeLegacy, modeloperation.IDChatCompletions, "https://provider.test", encKey, nil)
			setExecutionTestProfile(t, &plan, test.profileID, test.profileVersion)
			plan.Routes[0].Provider = "moonshot"
			plan.Routes[0].UpstreamModel = "upstream-kimi"
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeLegacy, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
				t.Fatal(err)
			}
			explicitOutput := 128
			batch, err := relay.prepareModelExecutionRoutes(
				context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
				"public-model", "public-model", "req-kimi-advanced",
				modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
				[]byte(test.body), time.Now(),
			)
			if err != nil {
				t.Fatal(err)
			}
			require.ElementsMatch(t, test.wantReserved, batch.profileEvidence.ReservedMeterUsage)
			require.Equal(t, test.required, batch.plan.RequiredCapabilities)
			actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
			require.NoError(t, err)
			var payload map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(actual, &payload))
			if test.wantParallel {
				require.JSONEq(t, `false`, string(payload["parallel_tool_calls"]))
			}
			if strings.Contains(test.body, `builtin_function`) {
				require.Contains(t, string(payload["tools"]), `builtin_function`)
			}
			if strings.Contains(test.body, `image_url`) {
				require.Contains(t, string(payload["messages"]), `data:image/png;base64,aA==`)
				require.Contains(t, string(payload["messages"]), `"partial":true`)
			}
			if strings.Contains(test.body, `"role":"system","tools":[{"type":"function"`) {
				require.Contains(t, string(payload["messages"]), `"tools"`)
			}
		})
	}
}

func TestKimiAdvancedShapesRemainConservativelyMeasurableBeforeProfileSelection(t *testing.T) {
	body := []byte(`{"model":"public-model","messages":[{"role":"system","tools":[{"type":"function","function":{"name":"lookup","description":"Look up a value","parameters":{"type":"object"}}}]},{"role":"user","content":[{"type":"image_url","image_url":"data:image/png;base64,aA=="}]},{"role":"assistant","content":"prefix","partial":true}],"tools":[{"type":"builtin_function","function":{"name":"$web_search"}}]}`)
	estimate, err := estimateChatPreProfileAdmission(body)
	require.NoError(t, err)
	require.Greater(t, estimate.InputTokens, len(body))
	require.Equal(t, chatAdmissionEstimateVersion, estimate.Version)
}

func TestTypedProviderExtensionsRemainConservativelyMeasurableBeforeProfileSelection(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","messages":[],"tool_stream":null,"thinking_budget":null,"preserve_thinking":null,"enable_search":null,"search_options":null,"web_search_options":null,"prompt_cache_options":null}`,
		`{"model":"gpt-5.6-sol","messages":[{"role":"system","content":[{"type":"text","text":"prefix","prompt_cache_breakpoint":null}]}]}`,
		`{"model":"qwen3.8-max","messages":[],"stream":true,"tools":[{"type":"function","function":{"name":"lookup","description":"Lookup"}}],"tool_stream":true,"enable_thinking":true,"thinking_budget":4096,"preserve_thinking":true}`,
		`{"model":"qwen3.8-max","messages":[],"enable_search":true,"search_options":{"forced_search":true,"search_strategy":"turbo"}}`,
		`{"model":"hy3","messages":[],"web_search_options":{"enable":true,"search_source":"lite"}}`,
		`{"model":"qwen3.8-max","messages":[{"role":"user","content":[{"type":"image_url","image_url":"https://example.test/image.webp"},{"type":"video_url","video_url":"https://example.test/video.mp4"}]}]}`,
		`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"https://example.test/audio.wav"}},{"type":"video_url","video_url":"https://example.test/video.mp4","fps":1,"media_resolution":"default"}]}]}`,
		`{"model":"gpt-5.6-sol","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"messages":[{"role":"system","content":[{"type":"text","text":"prefix","prompt_cache_breakpoint":{"mode":"explicit"}}]}]}`,
	} {
		estimate, err := estimateChatPreProfileAdmission([]byte(body))
		require.NoError(t, err, body)
		require.Greater(t, estimate.InputTokens, len(body), body)
		require.Equal(t, chatAdmissionEstimateVersion, estimate.Version)
	}

	for _, body := range []string{
		`{"model":"qwen3.8-max","messages":[],"thinking_budget":"4096"}`,
		`{"model":"qwen3.8-max","messages":[],"preserve_thinking":"true"}`,
		`{"model":"qwen3.8-max","messages":[],"tool_stream":"true"}`,
		`{"model":"qwen3.8-max","messages":[],"search_options":"turbo"}`,
		`{"model":"hy3","messages":[],"web_search_options":"lite"}`,
		`{"model":"gpt-5.6-sol","messages":[],"prompt_cache_options":"explicit"}`,
		`{"model":"gpt-5.6-sol","messages":[],"prompt_cache_retention":"24h"}`,
		`{"model":"m","messages":[],"unknown_provider_extension":true}`,
	} {
		_, err := estimateChatPreProfileAdmission([]byte(body))
		require.Error(t, err, body)
	}
}

func TestQwenAndHy3SearchExtensionsPassPostProfileAdmission(t *testing.T) {
	for _, test := range []struct {
		profileID string
		version   string
		body      string
	}{
		{profileID: "qwen.3.8-max.api", version: "1.7.0", body: `{"model":"qwen3.8-max","messages":[],"enable_search":true,"search_options":{"forced_search":true,"search_strategy":"turbo"}}`},
		{profileID: "hy3.api", version: "1.6.0", body: `{"model":"hy3","messages":[],"web_search_options":{"enable":true,"search_source":"lite"}}`},
	} {
		plan := executionProfilePlan(t, test.profileID, test.version)
		_, err := estimateChatAdmissionForPlan([]byte(test.body), plan)
		require.NoError(t, err, test.body)
	}
}

func TestChatAdmissionRejectsInvalidPreservedToolCallIndex(t *testing.T) {
	for _, index := range []string{`"0"`, `0.5`, `-1`, `1000001`} {
		body := []byte(`{"model":"public-model","messages":[{"role":"assistant","content":null,"tool_calls":[{"index":` + index + `,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}]}`)
		_, err := estimateChatPreProfileAdmission(body)
		require.ErrorContains(t, err, "message tool_call index must be a non-negative integer")
	}
}

func TestDoubaoSeed21ProCandidateValidatesStrictNonStreamCoreAndReasoningDefault(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-pro.api", "1.1.0")
	tests := []struct {
		name       string
		body       string
		wantToggle string
		wantErr    string
	}{
		{
			name:       "provider default reasoning",
			body:       `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"hello"}]}`,
			wantToggle: "omitted",
		},
		{
			name:       "explicit reasoning disabled",
			body:       `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled"}}`,
			wantToggle: "disabled",
		},
		{
			name:       "provider maximum through max tokens",
			body:       `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"hello"}],"max_tokens":262144}`,
			wantToggle: "omitted",
		},
		{
			name:       "compatibility maximum",
			body:       `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":65536}`,
			wantToggle: "omitted",
		},
		{
			name:    "max tokens above provider model maximum",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"max_tokens":262145}`,
			wantErr: "max_tokens must be between 1 and 262144",
		},
		{
			name:    "completion alias above documented field maximum",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"max_completion_tokens":65537}`,
			wantErr: "max_completion_tokens must be between 1 and 65536",
		},
		{
			name:    "output aliases are mutually exclusive",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"max_tokens":4096,"max_completion_tokens":4096}`,
			wantErr: "cannot be set together",
		},
		{
			name:    "streaming deferred",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"stream":true}`,
			wantErr: "streaming is not supported",
		},
		{
			name:    "effort deferred",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"reasoning_effort":"high"}`,
			wantErr: "does not accept reasoning_effort",
		},
		{
			name:    "tools deferred",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}]}`,
			wantErr: "function tools are not supported",
		},
		{
			name:    "structured output deferred",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"response_format":{"type":"json_object"}}`,
			wantErr: "response_format.type must be one of text",
		},
		{
			name:    "service tier deferred",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[],"service_tier":"default"}`,
			wantErr: "service_tier is not supported",
		},
		{
			name:    "reasoning content history deferred",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"assistant","content":"answer","reasoning_content":"prior chain"},{"role":"user","content":"continue"}]}`,
			wantErr: "reasoning_content history is not supported",
		},
		{
			name:    "reasoning object history deferred",
			body:    `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"assistant","content":"answer","reasoning":"prior chain"},{"role":"user","content":"continue"}]}`,
			wantErr: "reasoning history is not supported",
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
			require.Equal(t, test.wantToggle, evidence.ReasoningToggleRequest)
			require.Equal(t, "enabled", evidence.ReasoningProviderDefault)
			require.Equal(t, modelprofile.ReasoningDefaultProvider, evidence.ReasoningDefaultAuthority)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestDoubaoSeed21TurboCandidateValidatesStrictNonStreamEffortCore(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-turbo.api", "1.1.0")
	tests := []struct {
		name, body, wantRequest, wantEffective, wantErr string
	}{
		{
			name:        "provider default high",
			body:        `{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"user","content":"hello"}]}`,
			wantRequest: "omitted", wantEffective: "omitted",
		},
		{
			name:        "minimal disables reasoning",
			body:        `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"reasoning_effort":"minimal"}`,
			wantRequest: "minimal", wantEffective: "disabled",
		},
		{
			name:        "explicit low",
			body:        `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"reasoning_effort":"low"}`,
			wantRequest: "low", wantEffective: "low",
		},
		{
			name:        "provider maximum through max tokens",
			body:        `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"max_tokens":262144}`,
			wantRequest: "omitted", wantEffective: "omitted",
		},
		{
			name:        "compatibility maximum",
			body:        `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"max_completion_tokens":65536}`,
			wantRequest: "omitted", wantEffective: "omitted",
		},
		{name: "unsupported effort", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"reasoning_effort":"xhigh"}`, wantErr: "reasoning_effort must be one of"},
		{name: "thinking type not yet modeled", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"thinking":{"type":"enabled"}}`, wantErr: "does not accept thinking"},
		{name: "streaming deferred", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"stream":true}`, wantErr: "streaming is not supported"},
		{name: "tools deferred", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}]}`, wantErr: "function tools are not supported"},
		{name: "structured output deferred", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[],"response_format":{"type":"json_object"}}`, wantErr: "response_format.type must be one of text"},
		{name: "reasoning history deferred", body: `{"model":"doubao-seed-2-1-turbo-260628","messages":[{"role":"assistant","content":"answer","reasoning_content":"prior"}]}`, wantErr: "reasoning_content history is not supported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantRequest, evidence.ReasoningEffortRequest)
			require.Equal(t, test.wantEffective, evidence.ReasoningEffortEffective)
			require.Equal(t, "high", evidence.ReasoningProviderDefault)
			require.Equal(t, modelprofile.ReasoningDefaultProvider, evidence.ReasoningDefaultAuthority)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestDoubaoSeedEvolvingCandidateValidatesStrictNonStreamCore(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.2.0")
	tests := []struct {
		name, body, wantErr string
	}{
		{
			name: "plain text",
			body: `{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "provider maximum through max tokens",
			body: `{"model":"doubao-seed-evolving","messages":[],"max_tokens":262144}`,
		},
		{
			name: "compatibility maximum",
			body: `{"model":"doubao-seed-evolving","messages":[],"max_completion_tokens":65536}`,
		},
		{name: "max tokens above provider model maximum", body: `{"model":"doubao-seed-evolving","messages":[],"max_tokens":262145}`, wantErr: "max_tokens must be between 1 and 262144"},
		{name: "completion alias above documented field maximum", body: `{"model":"doubao-seed-evolving","messages":[],"max_completion_tokens":65537}`, wantErr: "max_completion_tokens must be between 1 and 65536"},
		{name: "output aliases are mutually exclusive", body: `{"model":"doubao-seed-evolving","messages":[],"max_tokens":4096,"max_completion_tokens":4096}`, wantErr: "cannot be set together"},
		{name: "streaming deferred", body: `{"model":"doubao-seed-evolving","messages":[],"stream":true}`, wantErr: "streaming is not supported"},
		{name: "thinking deferred", body: `{"model":"doubao-seed-evolving","messages":[],"thinking":{"type":"enabled"}}`, wantErr: "reasoning control is not supported by profile"},
		{name: "effort deferred", body: `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"high"}`, wantErr: "reasoning control is not supported by profile"},
		{name: "tools deferred", body: `{"model":"doubao-seed-evolving","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}]}`, wantErr: "function tools are not supported"},
		{name: "structured output deferred", body: `{"model":"doubao-seed-evolving","messages":[],"response_format":{"type":"json_object"}}`, wantErr: "response_format.type must be one of text"},
		{name: "service tier deferred", body: `{"model":"doubao-seed-evolving","messages":[],"service_tier":"default"}`, wantErr: "service_tier is not supported"},
		{name: "reasoning content history deferred", body: `{"model":"doubao-seed-evolving","messages":[{"role":"assistant","content":"answer","reasoning_content":"prior"}]}`, wantErr: "reasoning_content history is not supported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v1", evidence.ModelProtocolProfile)
			require.Empty(t, evidence.ReasoningProfile)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestDoubaoSeedEvolvingReasoningCandidateValidatesExactControlsAndHistory(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.3.0")
	tests := []struct {
		name, body, wantToggle, wantEffort, wantEffective, wantErr string
		wantTimeout                                                int
	}{
		{
			name:       "provider defaults enable reasoning",
			body:       `{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"hello"}]}`,
			wantToggle: "omitted", wantEffort: "omitted", wantEffective: "omitted", wantTimeout: 1_800,
		},
		{
			name:       "explicit thinking enabled",
			body:       `{"model":"doubao-seed-evolving","messages":[],"thinking":{"type":"enabled"}}`,
			wantToggle: "enabled", wantEffort: "omitted", wantEffective: "omitted", wantTimeout: 1_800,
		},
		{
			name:       "explicit thinking disabled",
			body:       `{"model":"doubao-seed-evolving","messages":[],"thinking":{"type":"disabled"}}`,
			wantToggle: "disabled", wantEffort: "omitted", wantEffective: "omitted",
		},
		{
			name:       "explicit low effort",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"low"}`,
			wantToggle: "omitted", wantEffort: "low", wantEffective: "low", wantTimeout: 1_800,
		},
		{
			name:       "explicit medium effort",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"medium"}`,
			wantToggle: "omitted", wantEffort: "medium", wantEffective: "medium", wantTimeout: 1_800,
		},
		{
			name:       "explicit high effort",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"high"}`,
			wantToggle: "omitted", wantEffort: "high", wantEffective: "high", wantTimeout: 1_800,
		},
		{
			name:       "xhigh compatibility retains wire value",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"xhigh"}`,
			wantToggle: "omitted", wantEffort: "xhigh", wantEffective: "high", wantTimeout: 1_800,
		},
		{
			name:       "max compatibility retains wire value",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"max"}`,
			wantToggle: "omitted", wantEffort: "max", wantEffective: "high", wantTimeout: 1_800,
		},
		{
			name:       "none disables reasoning directly",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"none"}`,
			wantToggle: "omitted", wantEffort: "none", wantEffective: "disabled",
		},
		{
			name:       "minimal disables reasoning directly",
			body:       `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"minimal"}`,
			wantToggle: "omitted", wantEffort: "minimal", wantEffective: "disabled",
		},
		{
			name:       "ordinary assistant reasoning history",
			body:       `{"model":"doubao-seed-evolving","messages":[{"role":"assistant","content":"answer","reasoning_content":"prior summary"},{"role":"user","content":"continue"}]}`,
			wantToggle: "omitted", wantEffort: "omitted", wantEffective: "omitted", wantTimeout: 1_800,
		},
		{name: "thinking auto is not documented for Evolving", body: `{"model":"doubao-seed-evolving","messages":[],"thinking":{"type":"auto"}}`, wantErr: "thinking must be one of enabled, disabled"},
		{name: "unknown effort", body: `{"model":"doubao-seed-evolving","messages":[],"reasoning_effort":"future"}`, wantErr: "reasoning_effort must be one of"},
		{name: "toggle and effort are mutually exclusive", body: `{"model":"doubao-seed-evolving","messages":[],"thinking":{"type":"enabled"},"reasoning_effort":"high"}`, wantErr: "thinking and reasoning_effort cannot be set together"},
		{name: "consistent disabled controls remain ambiguous", body: `{"model":"doubao-seed-evolving","messages":[],"thinking":{"type":"disabled"},"reasoning_effort":"minimal"}`, wantErr: "thinking and reasoning_effort cannot be set together"},
		{name: "opaque response evidence cannot be replayed", body: `{"model":"doubao-seed-evolving","messages":[{"role":"assistant","content":"answer","encrypted_content":"opaque"}]}`, wantErr: "encrypted_content history is not supported"},
		{name: "streaming remains deferred", body: `{"model":"doubao-seed-evolving","messages":[],"stream":true}`, wantErr: "streaming is not supported"},
		{name: "tools remain deferred", body: `{"model":"doubao-seed-evolving","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}]}`, wantErr: "function tools are not supported"},
		{name: "structured output remains deferred", body: `{"model":"doubao-seed-evolving","messages":[],"response_format":{"type":"json_object"}}`, wantErr: "response_format.type must be one of text"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v2", evidence.ModelProtocolProfile)
			require.Equal(t, "reasoning.doubao-seed-evolving-toggle-effort.v1", evidence.ReasoningProfile)
			require.Equal(t, test.wantToggle, evidence.ReasoningToggleRequest)
			require.Equal(t, test.wantEffort, evidence.ReasoningEffortRequest)
			require.Equal(t, test.wantEffective, evidence.ReasoningEffortEffective)
			require.Equal(t, "thinking=enabled;reasoning_effort=high", evidence.ReasoningProviderDefault)
			require.Equal(t, modelprofile.ReasoningDefaultProvider, evidence.ReasoningDefaultAuthority)
			require.Equal(t, test.wantTimeout, evidence.ReasoningTimeoutSeconds)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestDoubaoSeedEvolvingReasoningReachesProviderPayloadWithExactCapability(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &plan, "doubao.seed-evolving.api", "1.3.0")
	plan.Routes[0].Provider = "doubao"
	plan.Routes[0].UpstreamModel = "doubao-seed-evolving"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	planner := &executionPlannerStub{plan: plan}
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"xhigh","max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-evolving-reasoning",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []string{modelprofile.CapabilityReasoningEffort}, batch.plan.RequiredCapabilities)
	require.Equal(t, []modelrouting.CapabilityRequirement{{
		CapabilityID: modelprofile.CapabilityReasoningEffort,
		Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
	}}, batch.plan.CapabilityRequirements)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v2", batch.profileEvidence.ModelProtocolProfile)
	require.Equal(t, "xhigh", batch.profileEvidence.ReasoningEffortRequest)
	require.Equal(t, "high", batch.profileEvidence.ReasoningEffortEffective)
	require.Equal(t, 1_800, batch.profileEvidence.ReasoningTimeoutSeconds)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"xhigh","max_tokens":128}`, string(actual))
}

func TestDoubaoSeedEvolvingCoreReachesProviderPayloadWithoutCapabilityInflation(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &plan, "doubao.seed-evolving.api", "1.2.0")
	plan.Routes[0].Provider = "doubao"
	plan.Routes[0].UpstreamModel = "doubao-seed-evolving"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	planner := &executionPlannerStub{plan: plan}
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-evolving-core",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Empty(t, batch.plan.CapabilityRequirements)
	require.Empty(t, batch.plan.RequiredCapabilities)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v1", batch.profileEvidence.ModelProtocolProfile)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"hello"}],"max_tokens":128}`, string(actual))
}
