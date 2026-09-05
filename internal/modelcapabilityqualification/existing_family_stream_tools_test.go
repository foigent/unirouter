package modelcapabilityqualification

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestExistingFamilyDataProbeCompositions(t *testing.T) {
	for _, tc := range []struct{ id, version, protocol, probe string }{
		{"deepseek.v4-flash.api", "1.5.0", "protocol.deepseek-openai-chat.v5", DeepSeekStreamFunctionToolsProbeContractID},
		{"deepseek.v4-pro.api", "1.5.0", "protocol.deepseek-openai-chat.v5", DeepSeekStreamFunctionToolsProbeContractID},
		{"kimi.k2.6.api", "1.7.0", "protocol.kimi-openai-chat.v6", KimiStreamFunctionToolsProbeContractID},
		{"kimi.k2.7-code.api", "1.3.0", "protocol.kimi-openai-chat.v6", KimiStreamFunctionToolsProbeContractID},
		{"kimi.k3.api", "1.9.0", "protocol.kimi-openai-chat.k3.v6", KimiStreamFunctionToolsProbeContractID},
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v10", MiniMaxM3StreamFunctionToolsProbeContractID},
		{"minimax.m2.7-highspeed.payg", "1.2.0", "protocol.minimax-m2-openai-chat.v8", MiniMaxM27StreamFunctionToolsProbeContractID},
	} {
		t.Run(tc.id, func(t *testing.T) {
			p, ok := modelprofile.Lookup(tc.id, tc.version)
			require.True(t, ok)
			p.Version, p.ProtocolProfile = "99.0.0", tc.protocol
			if tc.id == "minimax.m3.payg" {
				p.ReasoningProfile = "reasoning.minimax-m3-adaptive-embedded.v2"
			}
			d, err := modelprofile.NewDefinition(p, p.Provider+"/"+strings.ToLower(p.UpstreamModelIDs[0]), "")
			require.NoError(t, err)
			b := Binding{OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
				ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: "1.0.0",
				ProbeContractID: tc.probe, ProbeContractVersion: "1.0.0", ExpectedProvider: p.Provider, ExpectedUpstreamModel: p.UpstreamModelIDs[0], PublicPath: "/v1/chat/completions"}
			b, err = BindDefinition(b, d)
			require.NoError(t, err)
			body, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
			require.NoError(t, err)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(body, &payload))
			require.Equal(t, true, payload["stream"])
			require.Equal(t, map[string]interface{}{"include_usage": true}, payload["stream_options"])
			protocol, _ := modelprofile.ResolveChatProtocolContract(p.ProtocolProfile)
			require.Equal(t, float64(1024), payload[protocol.OutputLimitField])
			if p.Provider == "deepseek" {
				require.NotContains(t, payload, "tool_choice")
			} else {
				require.Equal(t, "auto", payload["tool_choice"])
			}
			if tc.id == "minimax.m3.payg" {
				require.Equal(t, map[string]interface{}{"type": "adaptive"}, payload["thinking"])
			}
			bad := b
			bad.ExpectedProvider = "not-the-provider"
			_, err = BuildOpenAIChatFunctionToolsProbeRequest(bad)
			require.Error(t, err)
			if p.Provider == "minimax" {
				for _, cap := range []string{modelprofile.CapabilityPromptCache, modelprofile.CapabilityCacheReadUsage} {
					cache := b
					cache.CapabilityID = cap
					cache.ProbeContractID = MiniMaxPAYGM2ChatFunctionToolsProbeContractID
					cache.ProbeContractVersion = "2.0.0"
					if tc.id == "minimax.m3.payg" {
						cache.ProbeContractID = MiniMaxPAYGM3ChatFunctionToolsProbeContractID
					}
					cache, err = BindDefinition(cache, d)
					require.NoError(t, err)
					body, err = BuildOpenAIChatFunctionToolsProbeRequest(cache)
					require.NoError(t, err)
					require.Contains(t, string(body), `"stream":false`)
					require.Contains(t, string(body), "unirouter-cache-eligible-prefix")
				}
			}
		})
	}
}
