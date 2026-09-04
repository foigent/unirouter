package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func representativeReuseFunctionToolsBinding(contractID string) modelcapabilityqualification.Binding {
	switch contractID {
	case modelcapabilityqualification.MiniMaxChatFunctionToolsProbeContractID:
		return modelcapabilityqualification.Binding{
			OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
			ExecutionProfileID: "minimax.m3.token-plan", ExecutionProfileVersion: "1.7.0",
			ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9", ProtocolProfileVersion: "1.0.0",
			ProbeContractID: contractID, ProbeContractVersion: modelcapabilityqualification.OpenAIChatFunctionToolsProbeContractVersion,
			ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M3", PublicPath: "/v1/chat/completions",
		}
	case modelcapabilityqualification.MiniMaxPAYGM3ChatFunctionToolsProbeContractID:
		return modelcapabilityqualification.Binding{
			OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
			ExecutionProfileID: "minimax.m3.payg", ExecutionProfileVersion: "1.1.0",
			ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9", ProtocolProfileVersion: "1.0.0",
			ProbeContractID: contractID, ProbeContractVersion: modelcapabilityqualification.OpenAIChatFunctionToolsProbeContractVersion,
			ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M3", PublicPath: "/v1/chat/completions",
		}
	case modelcapabilityqualification.MiniMaxPAYGM2ChatFunctionToolsProbeContractID:
		return modelcapabilityqualification.Binding{
			OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
			ExecutionProfileID: "minimax.m2.7.payg", ExecutionProfileVersion: "1.1.0",
			ProtocolProfileID: "protocol.minimax-m2-openai-chat.v7", ProtocolProfileVersion: "1.0.0",
			ProbeContractID: contractID, ProbeContractVersion: modelcapabilityqualification.OpenAIChatFunctionToolsProbeContractVersion,
			ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M2.7", PublicPath: "/v1/chat/completions",
		}
	case modelcapabilityqualification.GLMToggleChatFunctionToolsProbeContractID:
		return modelcapabilityqualification.Binding{
			OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
			ExecutionProfileID: "glm.5.1.api", ExecutionProfileVersion: "1.0.0",
			ProtocolProfileID: "protocol.glm-openai-chat.v6", ProtocolProfileVersion: "1.0.0",
			ProbeContractID: contractID, ProbeContractVersion: modelcapabilityqualification.OpenAIChatFunctionToolsProbeContractVersion,
			ExpectedProvider: "glm", ExpectedUpstreamModel: "glm-5.1", PublicPath: "/v1/chat/completions",
		}
	}
	return modelcapabilityqualification.Binding{
		OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
		ExecutionProfileID: "glm.5.3.api", ExecutionProfileVersion: "1.8.0",
		ProtocolProfileID: "protocol.glm-openai-chat.v5", ProtocolProfileVersion: "1.0.0",
		ProbeContractID: contractID, ProbeContractVersion: modelcapabilityqualification.OpenAIChatFunctionToolsProbeContractVersion,
		ExpectedProvider: "glm", ExpectedUpstreamModel: "glm-5.3", PublicPath: "/v1/chat/completions",
	}
}

func TestGLM5VTextFunctionToolsQualificationCannotAuthorizeMediaByItself(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions,
		"https://open.bigmodel.cn/api/paas/v4", encKey, nil)
	setExecutionTestProfile(t, &plan, "glm.5v-turbo.api", "1.11.0")
	plan.Routes[0].Provider = "glm"
	plan.Routes[0].UpstreamModel = "glm-5v-turbo"
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))

	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/layout.png"}},{"type":"text","text":"Inspect this layout with the tool"}]}],"tools":[{"type":"function","function":{"name":"inspect_layout","description":"Inspect a layout","parameters":{"type":"object","properties":{}}}}],"tool_choice":"auto","max_tokens":128}`)
	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-glm5v-media-tools-boundary",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	want := map[string]bool{
		modelprofile.CapabilityFunctionTools: false,
		modelprofile.CapabilityTools:         false,
		modelprofile.CapabilityVision:        false,
	}
	for _, requirement := range batch.plan.CapabilityRequirements {
		if _, exists := want[requirement.CapabilityID]; exists {
			want[requirement.CapabilityID] = true
		}
	}
	for capability, found := range want {
		require.True(t, found, "missing independent capability requirement %s", capability)
	}
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
}

func TestRepresentativeReuseProbeRequestsPassExactProfileValidationAndAdmission(t *testing.T) {
	for _, contractID := range []string{
		modelcapabilityqualification.MiniMaxChatFunctionToolsProbeContractID,
		modelcapabilityqualification.MiniMaxPAYGM3ChatFunctionToolsProbeContractID,
		modelcapabilityqualification.MiniMaxPAYGM2ChatFunctionToolsProbeContractID,
		modelcapabilityqualification.GLMChatFunctionToolsProbeContractID,
		modelcapabilityqualification.GLMToggleChatFunctionToolsProbeContractID,
	} {
		binding := representativeReuseFunctionToolsBinding(contractID)
		body, err := modelcapabilityqualification.BuildOpenAIChatFunctionToolsProbeRequest(binding)
		require.NoError(t, err)
		plan := executionProfilePlan(t, binding.ExecutionProfileID, binding.ExecutionProfileVersion)
		evidence, err := validateChatExecutionProfileRequest(body, plan)
		require.NoError(t, err, binding.ExecutionProfileID)
		evidence.bindAdmissionBody(body)
		_, err = estimateChatAdmissionForValidatedPlan(body, plan, evidence)
		require.NoError(t, err, binding.ExecutionProfileID)

		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &payload))
		if contractID == modelcapabilityqualification.MiniMaxChatFunctionToolsProbeContractID ||
			contractID == modelcapabilityqualification.MiniMaxPAYGM3ChatFunctionToolsProbeContractID ||
			contractID == modelcapabilityqualification.MiniMaxPAYGM2ChatFunctionToolsProbeContractID {
			require.NotContains(t, payload, "tool_choice")
			require.NotContains(t, payload, "thinking")
		} else if contractID == modelcapabilityqualification.GLMChatFunctionToolsProbeContractID {
			require.Equal(t, "auto", payload["tool_choice"])
			require.Equal(t, "enabled", payload["thinking"].(map[string]interface{})["type"])
		} else {
			require.Equal(t, "auto", payload["tool_choice"])
			require.NotContains(t, payload, "thinking")
		}
	}
}

// Use compiled current bindings, not a parallel model/normalization registry.
// Response boundaries (including intentionally different Probe acceptance) are
// covered by glm_request_id_delivery_test and minimax_highspeed_tool_index_delivery_test.
func TestCurrentReuseProbeAndConsoleRequestsReachExactProviderWire(t *testing.T) {
	bindings := append(modelcapabilityqualification.MiniMaxPAYGCurrentBindings(), modelcapabilityqualification.GLM53CurrentBindings()...)
	bindings = append(bindings, modelcapabilityqualification.GLMToggleCurrentBindings()...)
	modelsTested := 0
	for _, binding := range bindings {
		if binding.CapabilityID != modelprofile.CapabilityFunctionTools {
			continue
		}
		modelsTested++
		t.Run(binding.ExpectedUpstreamModel, func(t *testing.T) {
			probe, err := modelcapabilityqualification.BuildOpenAIChatFunctionToolsProbeRequest(binding)
			require.NoError(t, err)
			// The Console's fixed pure-text Function Tool request: provider default
			// is omission, not a model-name-based rewrite of explicit auto.
			console := map[string]interface{}{
				"model": "public-model", "stream": false, "max_tokens": 1024,
				"messages": []interface{}{map[string]interface{}{"role": "user", "content": "Call unirouter_ui_echo with value ui-9-model-check."}},
				"tools": []interface{}{map[string]interface{}{"type": "function", "function": map[string]interface{}{
					"name": "unirouter_ui_echo", "description": "Echo a short validation value.",
					"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false},
				}}},
			}
			if binding.ExpectedProvider == "glm" {
				console["tool_choice"] = "auto"
			}
			consoleBody, err := json.Marshal(console)
			require.NoError(t, err)
			for name, body := range map[string][]byte{"probe": probe, "console": consoleBody} {
				t.Run(name, func(t *testing.T) {
					encKey := []byte("0123456789abcdef0123456789abcdef")
					endpoint := "https://open.bigmodel.cn/api/paas/v4"
					if binding.ExpectedProvider == "minimax" {
						endpoint = "https://api.minimax.cn/v1"
					}
					plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, endpoint, encKey, nil)
					setExecutionTestProfile(t, &plan, binding.ExecutionProfileID, binding.ExecutionProfileVersion)
					profile, ok := modelprofile.Lookup(binding.ExecutionProfileID, binding.ExecutionProfileVersion)
					require.True(t, ok)
					// The generic fixture's 128k ceiling cannot represent M3's
					// modeled omitted-output default; use this exact Profile's limits.
					plan.PublicContextLimit = profile.DefaultContextWindow
					plan.Routes[0].MaxContext = profile.DefaultContextWindow
					plan.PublicOutputLimit = profile.DefaultOutputLimit
					plan.Routes[0].DefaultMaxOutput = profile.DefaultOutputLimit
					plan.Routes[0].Provider = binding.ExpectedProvider
					plan.Routes[0].UpstreamModel = binding.ExpectedUpstreamModel
					relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
					require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
					var input map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(body, &input))
					var outputLimit *int
					for _, limitField := range []string{"max_tokens", "max_completion_tokens"} {
						if value, exists := input[limitField]; exists {
							require.NoError(t, json.Unmarshal(value, &outputLimit))
						}
					}
					batch, err := relay.prepareModelExecutionRoutes(context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
						"public-model", "public-model", "req-reuse-contract",
						modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: outputLimit, EstimateVersion: chatAdmissionEstimateVersion}, body, time.Now())
					require.NoError(t, err)
					require.Len(t, batch.routes, 1)
					wire, err := providerRequestBody(batch.routes[0].upstreamRequest)
					require.NoError(t, err)
					var outbound map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(wire, &outbound))
					require.JSONEq(t, `"`+binding.ExpectedUpstreamModel+`"`, string(outbound["model"]))
					for _, field := range []string{"tools", "tool_choice", "thinking", "max_tokens", "max_completion_tokens", "stream", "stream_options"} {
						if value, exists := input[field]; exists {
							require.JSONEq(t, string(value), string(outbound[field]), field)
						} else {
							require.NotContains(t, outbound, field, "must not invent omitted defaults")
						}
					}
				})
			}
			if binding.ExpectedProvider == "minimax" {
				console["tool_choice"] = "auto"
				explicitAuto, err := json.Marshal(console)
				require.NoError(t, err)
				_, err = validateChatExecutionProfileRequest(explicitAuto, executionProfilePlan(t, binding.ExecutionProfileID, binding.ExecutionProfileVersion))
				if binding.ProtocolProfileID == "protocol.minimax-m27-openai-chat.v1" {
					require.NoError(t, err, "new exact M2.7 contract supports explicit auto")
				} else {
					require.Error(t, err, "historical explicit auto must not silently become provider-default omission")
				}
			}
		})
	}
	require.Equal(t, 9, modelsTested)
}

func TestMiniMaxPAYGHighspeedSuccessorProbePassesExactProfileValidationAndAdmission(t *testing.T) {
	binding := modelcapabilityqualification.Binding{
		OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
		ExecutionProfileID: "minimax.m2.7-highspeed.payg", ExecutionProfileVersion: "1.1.0",
		ProtocolProfileID: "protocol.minimax-m2-openai-chat.v7", ProtocolProfileVersion: "1.0.0",
		ProbeContractID:      modelcapabilityqualification.MiniMaxPAYGM2ChatFunctionToolsProbeContractID,
		ProbeContractVersion: modelcapabilityqualification.OpenAIChatFunctionToolsProbeContractVersion,
		ExpectedProvider:     "minimax", ExpectedUpstreamModel: "MiniMax-M2.7-highspeed", PublicPath: "/v1/chat/completions",
	}
	body, err := modelcapabilityqualification.BuildOpenAIChatFunctionToolsProbeRequest(binding)
	require.NoError(t, err)
	plan := executionProfilePlan(t, binding.ExecutionProfileID, binding.ExecutionProfileVersion)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	evidence.bindAdmissionBody(body)
	_, err = estimateChatAdmissionForValidatedPlan(body, plan, evidence)
	require.NoError(t, err)
}

func TestRepresentativeReuseSuccessfulProbeFixturesTraverseCanonicalRuntimeDelivery(t *testing.T) {
	for _, test := range []struct {
		name, contractID, provider, body string
	}{
		{
			name: "minimax normalized exact response", contractID: modelcapabilityqualification.MiniMaxChatFunctionToolsProbeContractID,
			provider: "minimax",
			body:     `{"id":"chatcmpl-minimax","object":"chat.completion","model":"MiniMax-M3","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","name":"MiniMax AI","audio_content":"","content":"<think>probe</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"prompt_tokens_details":{"cached_tokens":3}},"input_sensitive":false,"output_sensitive":false,"input_sensitive_type":0,"output_sensitive_type":0,"output_sensitive_int":0,"base_resp":{"status_code":0,"status_msg":"success"}}`,
		},
		{
			name: "glm generic runtime response", contractID: modelcapabilityqualification.GLMChatFunctionToolsProbeContractID,
			provider: "glm",
			body:     `{"id":"chatcmpl-glm","object":"chat.completion","model":"glm-5.3","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`,
		},
		{
			name: "glm toggle normalized response with reasoning", contractID: modelcapabilityqualification.GLMToggleChatFunctionToolsProbeContractID,
			provider: "glm",
			body:     `{"id":"chatcmpl-glm","object":"chat.completion","model":"glm-5.1","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"reasoning_content":"exact reasoning trace","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			binding := representativeReuseFunctionToolsBinding(test.contractID)
			profile, ok := modelprofile.Lookup(binding.ExecutionProfileID, binding.ExecutionProfileVersion)
			require.True(t, ok)
			normalized, err := adapters.NewOpenAICompatible().ParseResponse(context.Background(), adapters.ResponseInput{
				Endpoint: adapters.EndpointChatCompletions, StatusCode: 200,
				Header: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(test.body),
				Channel:        models.Channel{Provider: test.provider, AdapterType: adapters.TypeOpenAICompatible},
				RequestedModel: binding.ExpectedUpstreamModel, UpstreamModel: binding.ExpectedUpstreamModel,
				ModelOperationID:   binding.OperationID,
				ExecutionProfileID: binding.ExecutionProfileID, ExecutionProfileVersion: binding.ExecutionProfileVersion,
				CacheUsageProfile: profile.CacheUsageProfile, ProtocolProfile: binding.ProtocolProfileID,
				OperationProtocolProfileID: binding.ProtocolProfileID, OperationProtocolProfileVersion: binding.ProtocolProfileVersion,
			})
			require.NoError(t, err)
			observation, err := modelcapabilityqualification.ValidateNormalizedOpenAIChatFunctionToolsForBinding(
				binding, normalized.Header.Get("Content-Type"), bytes.NewReader(normalized.Body),
			)
			require.NoError(t, err)
			require.True(t, observation.ToolCallObserved)

			var response map[string]interface{}
			require.NoError(t, decodeJSONUseNumber(normalized.Body, &response))
			route := &RouteResult{
				Channel: models.Channel{Provider: test.provider}, MappedModel: binding.ExpectedUpstreamModel,
				ExecutionProfileID: binding.ExecutionProfileID, ExecutionProfileVersion: binding.ExecutionProfileVersion,
				ModelProtocolProfile: binding.ProtocolProfileID, ReasoningProfile: profile.ReasoningProfile,
				CacheUsageProfile: profile.CacheUsageProfile,
				PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
					CapabilityID: modelprofile.CapabilityToolChoiceAuto,
					Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
					FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{
						DeclaredFunctionNames: []string{"unirouter_probe_echo"},
					},
				}},
			}
			inspection := inspectNonStreamOutputTextsForRoute(response, route)
			require.True(t, inspection.complete(), "incomplete paths: %v", inspection.incompletePaths)
		})
	}
}
