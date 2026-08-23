package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

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
	plan := executionProfilePlan(t, "deepseek.v4-pro.api", "1.1.0")
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
		"public-model", "public-model", "req-deepseek-auto-tool-choice", []string{modelCapabilityReasoning, modelCapabilityTools},
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
	plan := executionProfilePlan(t, "kimi.k2.6.api", "1.4.0")

	evidence, err := validateChatExecutionProfileRequest([]byte(`{
		"model":"kimi-k2.6","messages":[],
		"thinking":{"type":"enabled","keep":"all"},
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
			wantErr: "tool_choice=required is not supported",
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
		"tool_choice":"auto","temperature":0.60,"top_p":0.95,"n":1,
		"presence_penalty":0,"frequency_penalty":0
	}`), plan)
	require.NoError(t, err)
	require.Equal(t, "disabled", evidence.ReasoningToggleRequest)
}

func TestKimiK27PlatformProfileValidatesRequiredThinkingAndFixedParameters(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.7-code.api", "1.1.0")

	evidence, err := validateChatExecutionProfileRequest([]byte(`{"model":"kimi-k2.7-code","messages":[]}`), plan)
	require.NoError(t, err)
	require.Equal(t, "omitted", evidence.ReasoningToggleRequest)
	require.Equal(t, "omitted", evidence.ReasoningThinkingKeepRequest)
	require.Equal(t, "all", evidence.ReasoningThinkingKeepDefault)

	evidence, err = validateChatExecutionProfileRequest([]byte(`{
		"model":"kimi-k2.7-code","messages":[],
		"thinking":{"type":"enabled","keep":"all"},"tool_choice":"auto",
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
			wantErr: "tool_choice=required is not supported",
		},
		{
			name:    "named tool choice is unsupported",
			body:    `{"model":"kimi-k2.7-code","messages":[],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
			wantErr: "tool_choice=named is not supported",
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
	plan := executionProfilePlan(t, "kimi.k3.api", "1.4.0")
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
		"public-model", "public-model", "req-k3-profile-pre-secret", []string{modelCapabilityReasoning},
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
		"public-model", "public-model", "req-k3-default", nil,
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
			name: "K2.6 builtin search", profileID: "kimi.k2.6.api", profileVersion: "1.1.0",
			body:     `{"model":"public-model","messages":[{"role":"user","content":"latest"}],"thinking":{"type":"disabled"},"tools":[{"type":"builtin_function","function":{"name":"$web_search"}}],"max_tokens":128}`,
			required: []string{modelCapabilityBuiltinWebSearch, modelCapabilityTools}, wantParallel: true,
			wantReserved: []metering.MeterQuantity{{Meter: "builtin_web_search_calls", Quantity: 1}},
		},
		{
			name: "K2.6 inline image and partial", profileID: "kimi.k2.6.api", profileVersion: "1.1.0",
			body:     `{"model":"public-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aA=="}}]},{"role":"assistant","content":"prefix","partial":true}],"max_tokens":128}`,
			required: []string{modelCapabilityPartialMode, modelCapabilityVision},
		},
		{
			name: "K3 dynamic system tools", profileID: "kimi.k3.api", profileVersion: "1.2.0",
			body:     `{"model":"public-model","messages":[{"role":"system","tools":[{"type":"function","function":{"name":"lookup","description":"Look up a value","parameters":{"type":"object"}}}]},{"role":"user","content":"hello"}],"max_tokens":128}`,
			required: []string{modelCapabilityDynamicTools, modelCapabilityTools},
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
				"public-model", "public-model", "req-kimi-advanced", test.required,
				modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
				[]byte(test.body), time.Now(),
			)
			if err != nil {
				t.Fatal(err)
			}
			require.ElementsMatch(t, test.wantReserved, batch.profileEvidence.ReservedMeterUsage)
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
			if strings.Contains(test.body, `"tools":[{"type":"function"`) {
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
