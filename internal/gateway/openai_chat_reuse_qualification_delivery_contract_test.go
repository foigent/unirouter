package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
)

func representativeReuseFunctionToolsBinding(contractID string) modelcapabilityqualification.Binding {
	if contractID == modelcapabilityqualification.MiniMaxChatFunctionToolsProbeContractID {
		return modelcapabilityqualification.Binding{
			OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
			ExecutionProfileID: "minimax.m3.token-plan", ExecutionProfileVersion: "1.7.0",
			ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9", ProtocolProfileVersion: "1.0.0",
			ProbeContractID: contractID, ProbeContractVersion: modelcapabilityqualification.OpenAIChatFunctionToolsProbeContractVersion,
			ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M3", PublicPath: "/v1/chat/completions",
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

func TestRepresentativeReuseProbeRequestsPassExactProfileValidationAndAdmission(t *testing.T) {
	for _, contractID := range []string{
		modelcapabilityqualification.MiniMaxChatFunctionToolsProbeContractID,
		modelcapabilityqualification.GLMChatFunctionToolsProbeContractID,
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
		if contractID == modelcapabilityqualification.MiniMaxChatFunctionToolsProbeContractID {
			require.NotContains(t, payload, "tool_choice")
			require.NotContains(t, payload, "thinking")
		} else {
			require.Equal(t, "auto", payload["tool_choice"])
			require.Equal(t, "enabled", payload["thinking"].(map[string]interface{})["type"])
		}
	}
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
