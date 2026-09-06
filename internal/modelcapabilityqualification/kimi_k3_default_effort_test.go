package modelcapabilityqualification

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestKimiK3DefaultEffortBundleKeepsExistingDefinitionAndBindings(t *testing.T) {
	oldWire, err := os.ReadFile("../../capability-bundles/kimi-streamed-tools.json")
	require.NoError(t, err)
	old, err := publication.DecodeBundleV2(oldWire)
	require.NoError(t, err)
	wire, err := os.ReadFile("../../capability-bundles/kimi-k3-default-effort.json")
	require.NoError(t, err)
	require.NoError(t, ValidateBundleSource(wire))
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	require.Empty(t, bundle.Definitions, "resolve the retained definition; do not republish it")
	require.Len(t, bundle.BindingDelta, 1)
	require.Len(t, bundle.SelectorDelta, 1)
	require.Nil(t, bundle.SelectorDelta[0].ExpectedPredecessor, "only the missing selector is added")
	var d modelprofile.Definition
	for _, raw := range old.Definitions {
		candidate, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		if candidate.Profile().ID == "kimi.k3.api" {
			d = candidate
		}
	}
	require.Equal(t, "c052feeb45bc9155796f71a3ee3e99a13790b0c9e87c35e921401ac5603682f4", d.Digest())
	row := bundle.BindingDelta[0]
	require.Equal(t, d.Digest(), row.ProfileDefinitionDigest)
	require.Equal(t, "reasoning_effort", row.CapabilityID)
	require.Equal(t, "2.0.0", row.ProbeContractVersion)
	b := Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID, ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion, ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion, ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}
	b, err = BindDefinition(b, d)
	require.NoError(t, err)
	request, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(request, &payload))
	require.Equal(t, "max", payload["reasoning_effort"])
	require.Equal(t, true, payload["stream"])
	require.Equal(t, "auto", payload["tool_choice"])
	require.Equal(t, float64(1024), payload["max_completion_tokens"])
	require.Equal(t, map[string]any{"include_usage": true}, payload["stream_options"])
	oldBinding := b
	oldBinding.ProbeContractVersion, oldBinding.CapabilityID = "1.0.0", "function_tools"
	oldRequest, err := BuildOpenAIChatFunctionToolsProbeRequest(oldBinding)
	require.NoError(t, err)
	delete(payload, "reasoning_effort")
	oldExpected, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Equal(t, string(oldRequest), string(oldExpected), "v1 request semantics remain unchanged")
	for _, mutate := range []func(*Binding){
		func(b *Binding) { b.ProbeContractVersion = "1.0.0" },
		func(b *Binding) { b.ProbeContractVersion = "3.0.0" },
		func(b *Binding) { b.CapabilityID = "tools" },
		func(b *Binding) { b.ExpectedProvider = "deepseek" },
		func(b *Binding) { b.ExpectedUpstreamModel = "other" },
		func(b *Binding) { b.ProtocolProfileID = "protocol.kimi-openai-chat.v6" },
	} {
		bad := b
		mutate(&bad)
		_, err := BuildOpenAIChatFunctionToolsProbeRequest(bad)
		require.Error(t, err)
	}
	for _, change := range []func(*modelprofile.Profile){
		func(p *modelprofile.Profile) { p.ReasoningProfile = "reasoning.kimi-k3-effort.v1" },
		func(p *modelprofile.Profile) { p.CacheUsageProfile = "cache-usage.none.v1" },
	} {
		p := d.Profile()
		change(&p)
		other, err := modelprofile.NewDefinition(p, "kimi/kimi-k3", "")
		if err == nil {
			_, err = BindDefinition(b, other)
		}
		require.Error(t, err, "a different component cannot inherit effort qualification")
	}
	// Constructed transport fixture, not a paid Provider observation.
	fixture := "data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"kimi-k3","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"brief","tool_calls":[{"index":0,"id":"call_echo","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}]}` + "\n\n" +
		"data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"kimi-k3","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls","usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"cached_tokens":0}}]}` + "\n\ndata: [DONE]\n\n"
	observation, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(fixture))
	require.NoError(t, err)
	require.True(t, observation.ReasoningObserved)
	require.True(t, observation.ToolCallObserved)
	require.EqualValues(t, 120, observation.TotalTokens)
	for name, invalid := range map[string]string{
		"reasoning": strings.Replace(fixture, `"reasoning_content":"brief",`, "", 1),
		"arguments": strings.Replace(fixture, "UNIROUTER_TOOL_PROBE_OK", "wrong", 1),
		"usage":     strings.Replace(fixture, `"completion_tokens":20`, `"completion_tokens":-1`, 1),
		"terminal":  strings.Replace(fixture, "data: [DONE]\n\n", "", 1),
	} {
		_, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(invalid))
		require.Error(t, err, name)
	}
}
