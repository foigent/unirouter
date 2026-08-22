package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
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
			wantErr: "low, high, or max",
		},
		{
			name:    "named tool choice",
			body:    `{"model":"kimi-k3","messages":[{"role":"user","content":"hello"}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
			wantErr: "named tool_choice is incompatible",
		},
		{
			name:    "K2 thinking field",
			body:    `{"model":"kimi-k3","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled"}}`,
			wantErr: "K3 accepts reasoning_effort",
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
