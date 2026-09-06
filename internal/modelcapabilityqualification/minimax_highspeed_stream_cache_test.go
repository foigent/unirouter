package modelcapabilityqualification

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	pub "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
)

func highspeedCacheBundle(t *testing.T) (modelprofile.Definition, []Binding) {
	t.Helper()
	wire, err := os.ReadFile("../../capability-bundles/minimax-highspeed-stream-cache.json")
	require.NoError(t, err)
	require.NoError(t, ValidateBundleSource(wire))
	bundle, err := pub.DecodeBundleV2(wire)
	require.NoError(t, err)
	require.Len(t, bundle.Definitions, 1)
	require.Len(t, bundle.BindingDelta, 7)
	require.Len(t, bundle.SelectorDelta, 7)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	var bindings []Binding
	for _, r := range bundle.BindingDelta {
		require.Equal(t, d.Digest(), r.ProfileDefinitionDigest)
		b, err := BindDefinition(Binding{OperationID: r.OperationID, CapabilityID: r.CapabilityID, ExecutionProfileID: r.ExecutionProfileID, ExecutionProfileVersion: r.ExecutionProfileVersion, ProtocolProfileID: r.ProtocolProfileID, ProtocolProfileVersion: r.ProtocolProfileVersion, ProbeContractID: r.ProbeContractID, ProbeContractVersion: r.ProbeContractVersion, ExpectedProvider: r.ExpectedProvider, ExpectedUpstreamModel: r.ExpectedUpstreamModel, PublicPath: r.PublicPath}, d)
		require.NoError(t, err)
		bindings = append(bindings, b)
	}
	for _, s := range bundle.SelectorDelta {
		require.Nil(t, s.ExpectedPredecessor)
	}
	return d, bindings
}

func TestHighspeedOptionalStreamCacheExactComposition(t *testing.T) {
	d, bindings := highspeedCacheBundle(t)
	p := d.Profile()
	_, compiled := modelprofile.Lookup(p.ID, p.Version)
	require.False(t, compiled)
	oldWire, err := os.ReadFile("../../capability-bundles/minimax-tool-history-continuation.json")
	require.NoError(t, err)
	oldBundle, err := pub.DecodeBundleV2(oldWire)
	require.NoError(t, err)
	var old modelprofile.Definition
	for _, raw := range oldBundle.Definitions {
		candidate, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		if candidate.Profile().ID == p.ID {
			old = candidate
		}
	}
	require.Equal(t, old.Digest(), d.PredecessorDigest())
	unchanged := p
	unchanged.Version = old.Profile().Version
	unchanged.CacheUsageProfile = old.Profile().CacheUsageProfile
	require.Equal(t, old.Profile(), unchanged)
	for _, b := range bindings {
		t.Run(b.CapabilityID, func(t *testing.T) {
			request, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
			require.NoError(t, err)
			cache := b.CapabilityID == modelprofile.CapabilityPromptCache || b.CapabilityID == modelprofile.CapabilityCacheReadUsage
			var payload map[string]any
			require.NoError(t, json.Unmarshal(request, &payload))
			require.Equal(t, !cache, payload["stream"])
			if cache {
				require.Contains(t, string(request), "unirouter-cache-eligible-prefix")
			}
			legacy := b
			legacy.ProbeContractVersion = "1.0.0"
			if cache {
				legacy.ProbeContractVersion = "2.0.0"
			}
			_, err = BuildOpenAIChatFunctionToolsProbeRequest(legacy)
			require.Error(t, err, "old Probe cannot acquire the new cache composition")
			legacy.ExecutionProfileVersion = old.Profile().Version
			legacy, err = BindDefinition(legacy, old)
			require.NoError(t, err)
			_, err = BuildOpenAIChatFunctionToolsProbeRequest(legacy)
			require.NoError(t, err)
			legacy.ProbeContractVersion = b.ProbeContractVersion
			_, err = BuildOpenAIChatFunctionToolsProbeRequest(legacy)
			require.Error(t, err, "new Probe cannot reinterpret old Profile")
			missing := b
			missing.definition = nil
			_, err = BuildOpenAIChatFunctionToolsProbeRequest(missing)
			require.Error(t, err)
			for _, kind := range []string{"cache", "reasoning", "protocol", "provider"} {
				badP := p
				badB := b
				switch kind {
				case "cache":
					badP.CacheUsageProfile = "cache-usage.minimax-openai.v2"
				case "reasoning":
					badP.ReasoningProfile = "reasoning.minimax-m2-required-embedded.v1"
				case "protocol":
					badP.ProtocolProfile = "protocol.minimax-m2-openai-chat.v7"
					badB.ProtocolProfileID = badP.ProtocolProfile
				case "provider":
					badP.Provider = "glm"
					badB.ExpectedProvider = "glm"
				}
				badD, err := modelprofile.NewDefinition(badP, d.ProductLineageID(), d.PredecessorDigest())
				if err == nil {
					_, err = BindDefinition(badB, badD)
				}
				require.Error(t, err, kind)
			}
		})
	}
}

func TestHighspeedCacheProbeBuildNormalizeValidate(t *testing.T) {
	d, bindings := highspeedCacheBundle(t)
	p := d.Profile()
	fixture, err := os.ReadFile("../adapters/testdata/minimax_m27_stream_tools.sse")
	require.NoError(t, err)
	stream := strings.ReplaceAll(string(fixture), "MiniMax-M2.7", p.UpstreamModelIDs[0])
	for _, b := range bindings {
		t.Run(b.CapabilityID, func(t *testing.T) {
			_, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
			require.NoError(t, err)
			if b.ProbeContractID == MiniMaxM27StreamFunctionToolsProbeContractID {
				obs, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(stream))
				require.NoError(t, err)
				require.True(t, obs.ToolCallObserved)
				require.True(t, obs.ReasoningObserved)
				require.EqualValues(t, 5585, obs.PromptTokens)
				require.False(t, obs.CacheReadCounterObserved)
				return
			}
			for _, tc := range []struct {
				name, counter, content string
				prompt                 int
				pass                   bool
			}{
				{"hit", "100", "<think>Check.</think>", 5554, true},
				{"zero", "0", "<think>Check.</think>", 5554, b.CapabilityID == modelprofile.CapabilityCacheReadUsage},
				{"absent", "", "<think>Check.</think>", 5554, false},
				{"null", "null", "<think>Check.</think>", 5554, false},
				{"negative", "-1", "<think>Check.</think>", 5554, false},
				{"excess", "6000", "<think>Check.</think>", 5554, false},
				{"ineligible", "100", "<think>Check.</think>", 511, false},
				{"missing reasoning", "100", "No thinking", 5554, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					member := ""
					if tc.counter != "" {
						member = `,"prompt_tokens_details":{"cached_tokens":` + tc.counter + `}`
					}
					wire := []byte(fmt.Sprintf(`{"id":"fixture","object":"chat.completion","created":1,"model":"%s","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":%q,"tool_calls":[{"index":0,"id":"call_echo","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":%d,"completion_tokens":199,"total_tokens":%d%s},"base_resp":{"status_code":0,"status_msg":""}}`, p.UpstreamModelIDs[0], tc.content, tc.prompt, tc.prompt+199, member))
					out, err := adapters.NewOpenAICompatible().ParseResponse(context.Background(), adapters.ResponseInput{ProfileDefinition: d, Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: wire, Channel: models.Channel{Provider: p.Provider}, RequestedModel: p.UpstreamModelIDs[0], UpstreamModel: p.UpstreamModelIDs[0], ModelOperationID: b.OperationID, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, CacheUsageProfile: p.CacheUsageProfile, ProtocolProfile: p.ProtocolProfile, OperationProtocolProfileID: b.ProtocolProfileID, OperationProtocolProfileVersion: b.ProtocolProfileVersion})
					if err == nil {
						var envelope map[string]any
						require.NoError(t, json.Unmarshal(out.Body, &envelope))
						call := envelope["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
						require.NotContains(t, call, "index")
						_, err = ValidateNormalizedOpenAIChatFunctionToolsForBinding(b, "application/json", strings.NewReader(string(out.Body)))
					}
					if tc.pass {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
				})
			}
		})
	}
}
