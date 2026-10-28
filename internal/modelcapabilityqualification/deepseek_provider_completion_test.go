package modelcapabilityqualification

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"os"
	"strings"
	"testing"
)

func TestDeepSeekProviderCompletionBundleBindingsAndStrictProbe(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/deepseek-v41-flash-provider-completion.json")
	require.NoError(t, err)
	require.NoError(t, ValidateBundleSource(wire))
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	require.Len(t, bundle.Definitions, 1)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	_, old := currentFlashBinding(t)
	require.Equal(t, old.Digest(), d.PredecessorDigest())
	require.Equal(t, old.ProductLineageID(), d.ProductLineageID())
	require.Equal(t, "1.1.0", d.Profile().Version)
	require.Equal(t, "2751c693d24905f1845ebb64253322e4cdaed5b327182cfd55890f5c72575a99", d.Digest())
	require.Len(t, bundle.BindingDelta, 6)
	require.Len(t, bundle.SelectorDelta, 6)
	fixture, err := os.ReadFile("../adapters/testdata/deepseek_provider_completion_v6.sse")
	require.NoError(t, err)
	// Synthetic evidence: bytes exceed B, while the fixture's provider vector is 20.
	first := strings.Index(string(fixture), "\n\n") + 2
	large := "data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"deepseek-flash","choices":[{"index":0,"delta":{"reasoning_content":"` + strings.Repeat("A", 12288) + `"},"finish_reason":null}]}` + "\n\n"
	stream := string(fixture[:first]) + large + string(fixture[first:])
	for i, row := range bundle.BindingDelta {
		require.Equal(t, d.Digest(), row.ProfileDefinitionDigest)
		selector := bundle.SelectorDelta[i]
		require.Nil(t, selector.ExpectedPredecessor)
		require.Equal(t, row.CapabilityID, selector.CapabilityID)
		require.Equal(t, row.ProbeContractID, selector.Final.ProbeContractID)
		require.Equal(t, row.ProbeContractVersion, selector.Final.ProbeContractVersion)
		b, e := BindDefinition(Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID, ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion, ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion, ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}, d)
		require.NoError(t, e)
		group, e := SharedStreamProbeContract(b)
		require.NoError(t, e)
		require.Equal(t, DeepSeekProviderCompletionProbeContractID+"@1.0.0", group)
		request, e := BuildOpenAIChatFunctionToolsProbeRequest(b)
		require.NoError(t, e)
		var body map[string]any
		require.NoError(t, json.Unmarshal(request, &body))
		require.EqualValues(t, 2048, body["max_tokens"])
		require.NotContains(t, body, "max_completion_tokens")
		require.NotContains(t, body, "tool_choice")
		require.Equal(t, map[string]any{"type": "enabled"}, body["thinking"])
		require.Equal(t, "high", body["reasoning_effort"])
		observation, e := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(stream))
		require.NoError(t, e)
		require.EqualValues(t, 20, observation.CompletionTokens)
		for name, bad := range map[string]string{
			"missing done":        strings.ReplaceAll(stream, "data: [DONE]\n\n", ""),
			"missing finish":      strings.Replace(stream, `"finish_reason":"tool_calls"`, `"finish_reason":null`, 1),
			"truncated arguments": strings.Replace(stream, `OK\"}`, `OK\"`, 1),
			"inconsistent usage":  strings.Replace(stream, `"total_tokens":120`, `"total_tokens":119`, 1),
			"fresh terminal text": strings.Replace(stream, `"delta":{},"finish_reason":"tool_calls"`, `"delta":{"reasoning_content":"late"},"finish_reason":"tool_calls"`, 1),
			"usage-only":          strings.Replace(stream, `"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]`, `"choices":[]`, 1),
			"overrun":             strings.ReplaceAll(strings.ReplaceAll(stream, `"completion_tokens":20`, `"completion_tokens":2049`), `"total_tokens":120`, `"total_tokens":2149`),
			"duplicate usage":     strings.Replace(stream, `"completion_tokens":20`, `"completion_tokens":20,"completion_tokens":20`, 1),
			"wrong model":         strings.ReplaceAll(stream, "deepseek-flash", "deepseek-v4-flash"),
		} {
			t.Run(row.CapabilityID+"/"+name, func(t *testing.T) {
				_, e := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(bad))
				require.Error(t, e)
			})
		}
		for _, mutate := range []func(*Binding){
			func(b *Binding) { b.ProbeContractID = DeepSeekStreamFunctionToolsProbeContractID },
			func(b *Binding) { b.ProbeContractVersion = "99.0.0" },
			func(b *Binding) { b.ExpectedUpstreamModel = "deepseek-v4-flash" },
			func(b *Binding) { b.ProtocolProfileID = "protocol.deepseek-openai-chat.v5" },
		} {
			bad := b
			mutate(&bad)
			_, e = BuildOpenAIChatFunctionToolsProbeRequest(bad)
			require.Error(t, e)
		}
	}
}
