package gateway

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	pub "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	q "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"os"
	"testing"
	"time"
)

// Constructed minimal SDK-shape regression, not a claimed Provider capture.
func TestMiniMaxDataToolContinuationReachesProviderWithoutInventedContent(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/minimax-tool-history-continuation.json")
	require.NoError(t, err)
	bundle, err := pub.DecodeBundleV2(wire)
	require.NoError(t, err)
	for _, raw := range bundle.Definitions {
		d, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		p := d.Profile()
		if p.ID == "minimax.m3.payg" && p.Version == "1.4.0" {
			continue
		}
		var binding q.Binding
		for _, r := range bundle.BindingDelta {
			if r.ProfileDefinitionDigest == d.Digest() && r.CapabilityID == modelprofile.CapabilityFunctionTools {
				binding, err = q.BindDefinition(q.Binding{OperationID: r.OperationID, CapabilityID: r.CapabilityID, ExecutionProfileID: r.ExecutionProfileID, ExecutionProfileVersion: r.ExecutionProfileVersion, ProtocolProfileID: r.ProtocolProfileID, ProtocolProfileVersion: r.ProtocolProfileVersion, ProbeContractID: r.ProbeContractID, ProbeContractVersion: r.ProbeContractVersion, ExpectedProvider: r.ExpectedProvider, ExpectedUpstreamModel: r.ExpectedUpstreamModel, PublicPath: r.PublicPath}, d)
				require.NoError(t, err)
			}
		}
		oldReason := "reasoning.minimax-m2-required-embedded.v1"
		if p.ID == "minimax.m3.payg" {
			oldReason = "reasoning.minimax-m3-adaptive-embedded.v2"
		}
		for _, scenario := range []string{"null", "absent", "preserved", "wrong_result_id", "missing_result", "duplicate_call", "split_reasoning", "bad_thinking", "above_limit"} {
			t.Run(p.ID+"/"+scenario, func(t *testing.T) {
				body, err := q.BuildOpenAIChatFunctionToolsProbeRequest(binding)
				require.NoError(t, err)
				var req map[string]interface{}
				require.NoError(t, json.Unmarshal(body, &req))
				delete(req, "thinking")
				call := map[string]interface{}{"id": "call_echo", "type": "function", "function": map[string]interface{}{"name": "unirouter_probe_echo", "arguments": `{"value":"UNIROUTER_TOOL_PROBE_OK"}`}}
				assistant := map[string]interface{}{"role": "assistant", "content": nil, "tool_calls": []interface{}{call}}
				result := map[string]interface{}{"role": "tool", "tool_call_id": "call_echo", "content": "UNIROUTER_TOOL_PROBE_OK"}
				req["messages"] = []interface{}{map[string]interface{}{"role": "user", "content": "Read the public fixture."}, assistant, result}
				switch scenario {
				case "absent":
					delete(assistant, "content")
				case "preserved":
					assistant["content"] = "<think>Checked the fixture.</think>"
				case "wrong_result_id":
					result["tool_call_id"] = "different"
				case "missing_result":
					req["messages"] = req["messages"].([]interface{})[:2]
				case "duplicate_call":
					assistant["tool_calls"] = []interface{}{call, call}
				case "split_reasoning":
					req["reasoning_split"] = true
				case "bad_thinking":
					req["thinking"] = map[string]string{"type": "enabled"}
				case "above_limit":
					req["max_completion_tokens"] = p.ProviderMaxOutput + 1
				}
				body, err = json.Marshal(req)
				require.NoError(t, err)
				key := []byte("0123456789abcdef0123456789abcdef")
				plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimax.cn/v1", key, nil)
				setExecutionTestProfile(t, &plan, p.ID, "1.2.0")
				plan.Definition = d
				plan.ExecutionProfileVersion = p.Version
				plan.ModelProtocolProfile = p.ProtocolProfile
				plan.ReasoningProfile = p.ReasoningProfile
				plan.CacheUsageProfile = p.CacheUsageProfile
				plan.PublicOutputLimit = p.ProviderMaxOutput
				plan.Routes[0].Provider = p.Provider
				plan.Routes[0].UpstreamModel = p.UpstreamModelIDs[0]
				plan.Routes[0].DefaultMaxOutput = 1024
				evidence, err := validateChatExecutionProfileRequest(body, plan)
				if scenario != "null" && scenario != "absent" && scenario != "preserved" {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				old := p
				old.ReasoningProfile = oldReason
				oldD, err := modelprofile.NewDefinition(old, d.ProductLineageID(), d.PredecessorDigest())
				require.NoError(t, err)
				oldPlan := plan
				oldPlan.Definition = oldD
				oldPlan.ReasoningProfile = oldReason
				_, oldErr := validateChatExecutionProfileRequest(body, oldPlan)
				if scenario == "preserved" {
					require.NoError(t, oldErr)
				} else {
					require.ErrorContains(t, oldErr, "must preserve content")
				}
				evidence.bindAdmissionBody(body)
				_, err = estimateChatAdmissionForValidatedPlan(body, plan, evidence)
				require.NoError(t, err)
				relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
				require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
				n := 1024
				batch, err := relay.prepareModelExecutionRoutes(context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions, "public-model", "public-model", "minimax-continuation", modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &n, EstimateVersion: chatAdmissionEstimateVersion}, body, time.Now())
				require.NoError(t, err)
				sent, err := providerRequestBody(batch.routes[0].upstreamRequest)
				require.NoError(t, err)
				var before, after map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(body, &before))
				require.NoError(t, json.Unmarshal(sent, &after))
				for _, field := range []string{"messages", "tools", "tool_choice", "stream", "stream_options", "max_completion_tokens"} {
					require.JSONEq(t, string(before[field]), string(after[field]), field)
				}
				require.NotContains(t, after, "reasoning_split")
				require.NotContains(t, after, "thinking")
			})
		}
	}
}
