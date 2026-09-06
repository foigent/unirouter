package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestExistingFamilyDataProbeAndAgentHistoryReachExactWire(t *testing.T) {
	for _, tc := range []struct{ id, version, protocol, probe, base string }{
		{"deepseek.v4-flash.api", "1.5.0", "protocol.deepseek-openai-chat.v5", qualification.DeepSeekStreamFunctionToolsProbeContractID, "https://api.deepseek.com"},
		{"deepseek.v4-pro.api", "1.5.0", "protocol.deepseek-openai-chat.v5", qualification.DeepSeekStreamFunctionToolsProbeContractID, "https://api.deepseek.com"},
		{"kimi.k2.6.api", "1.7.0", "protocol.kimi-openai-chat.v6", qualification.KimiStreamFunctionToolsProbeContractID, "https://api.moonshot.cn/v1"},
		{"kimi.k2.7-code.api", "1.3.0", "protocol.kimi-openai-chat.v6", qualification.KimiStreamFunctionToolsProbeContractID, "https://api.moonshot.cn/v1"},
		{"kimi.k3.api", "1.9.0", "protocol.kimi-openai-chat.k3.v6", qualification.KimiStreamFunctionToolsProbeContractID, "https://api.moonshot.cn/v1"},
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v10", qualification.MiniMaxM3StreamFunctionToolsProbeContractID, "https://api.minimax.cn/v1"},
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v11", qualification.MiniMaxM3StreamFunctionToolsProbeContractID, "https://api.minimax.cn/v1"},
		{"minimax.m2.7-highspeed.payg", "1.2.0", "protocol.minimax-m2-openai-chat.v8", qualification.MiniMaxM27StreamFunctionToolsProbeContractID, "https://api.minimax.cn/v1"},
	} {
		p, ok := modelprofile.Lookup(tc.id, tc.version)
		require.True(t, ok)
		p.Version, p.ProtocolProfile = "99.0.0", tc.protocol
		if tc.id == "minimax.m3.payg" {
			p.ReasoningProfile = "reasoning.minimax-m3-adaptive-embedded.v2"
		}
		d, err := modelprofile.NewDefinition(p, p.Provider+"/"+strings.ToLower(p.UpstreamModelIDs[0]), "")
		require.NoError(t, err)
		b, err := qualification.BindDefinition(qualification.Binding{OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: "1.0.0", ProbeContractID: tc.probe, ProbeContractVersion: "1.0.0", ExpectedProvider: p.Provider, ExpectedUpstreamModel: p.UpstreamModelIDs[0], PublicPath: "/v1/chat/completions"}, d)
		require.NoError(t, err)
		for _, scenario := range []string{"probe", "default", "history", "disabled"} {
			if scenario == "disabled" && tc.id != "minimax.m3.payg" && tc.id != "kimi.k2.6.api" {
				continue
			}
			t.Run(tc.id+"/"+scenario, func(t *testing.T) {
				body, err := qualification.BuildOpenAIChatFunctionToolsProbeRequest(b)
				require.NoError(t, err)
				var request map[string]interface{}
				require.NoError(t, json.Unmarshal(body, &request))
				contract, _ := modelprofile.ResolveChatProtocolContract(p.ProtocolProfile)
				if scenario != "probe" {
					delete(request, "thinking")
					request["tool_choice"] = "auto"
					request[contract.OutputLimitField] = 8192
				}
				if scenario == "history" {
					assistant := map[string]interface{}{"role": "assistant", "content": "checked", "tool_calls": []interface{}{map[string]interface{}{"id": "call_echo", "type": "function", "function": map[string]interface{}{"name": "unirouter_probe_echo", "arguments": `{"value":"UNIROUTER_TOOL_PROBE_OK"}`}}}}
					if p.Provider != "minimax" {
						assistant["reasoning_content"] = "Checked the tool."
					} else {
						assistant["content"] = "<think>Checked the tool.</think>"
					}
					request["messages"] = []interface{}{map[string]interface{}{"role": "user", "content": "Read the public fixture."}, assistant, map[string]interface{}{"role": "tool", "tool_call_id": "call_echo", "content": "UNIROUTER_TOOL_PROBE_OK"}}
				}
				if scenario == "disabled" {
					request["thinking"] = map[string]string{"type": "disabled"}
				}
				body, err = json.Marshal(request)
				require.NoError(t, err)
				key := []byte("0123456789abcdef0123456789abcdef")
				plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, tc.base, key, nil)
				setExecutionTestProfile(t, &plan, tc.id, tc.version)
				plan.Definition = d
				plan.ExecutionProfileVersion = p.Version
				plan.ModelProtocolProfile = p.ProtocolProfile
				plan.ReasoningProfile = p.ReasoningProfile
				plan.Routes[0].Provider = p.Provider
				plan.Routes[0].UpstreamModel = b.ExpectedUpstreamModel
				plan.PublicOutputLimit = p.ProviderMaxOutput
				plan.Routes[0].DefaultMaxOutput = 8192
				evidence, err := validateChatExecutionProfileRequest(body, plan)
				require.NoError(t, err)
				evidence.bindAdmissionBody(body)
				_, err = estimateChatAdmissionForValidatedPlan(body, plan, evidence)
				require.NoError(t, err)
				relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
				require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
				n := 8192
				if scenario == "probe" {
					n = 1024
				}
				batch, err := relay.prepareModelExecutionRoutes(context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions, "public-model", "public-model", "existing-family-wire", modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &n, EstimateVersion: chatAdmissionEstimateVersion}, body, time.Now())
				require.NoError(t, err)
				wire, err := providerRequestBody(batch.routes[0].upstreamRequest)
				require.NoError(t, err)
				var sent, input map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(wire, &sent))
				require.NoError(t, json.Unmarshal(body, &input))
				for _, field := range []string{"messages", "tools", "stream", "stream_options", contract.OutputLimitField} {
					require.JSONEq(t, string(input[field]), string(sent[field]), field)
				}
				if p.Provider == "deepseek" {
					require.NotContains(t, sent, "tool_choice")
				} else {
					require.JSONEq(t, string(input["tool_choice"]), string(sent["tool_choice"]))
				}
			})
		}
	}
}
