package modelcapabilityqualification

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func currentFlashBinding(t *testing.T) (Binding, modelprofile.Definition) {
	t.Helper()
	wire, err := os.ReadFile("../../capability-bundles/deepseek-v41-flash.json")
	require.NoError(t, err)
	require.NoError(t, ValidateBundleSource(wire))
	bundle, err := publication.VerifyBundleV2(wire, fmt.Sprintf("%x", sha256.Sum256(wire)))
	require.NoError(t, err)
	require.Len(t, bundle.Definitions, 1)
	require.Len(t, bundle.BindingDelta, 6)
	require.Len(t, bundle.SelectorDelta, 6)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	require.Equal(t, "6c6c7ae34b6a6dcf69b0a3a6b382a758ec04602b50067bb2bae927c76bf4a33f", d.Digest())
	require.Empty(t, d.PredecessorDigest())
	p := d.Profile()
	require.Equal(t, "deepseek.v4.1-flash.api", p.ID)
	require.Equal(t, "1.0.0", p.Version)
	require.Equal(t, []string{"deepseek-flash"}, p.UpstreamModelIDs)
	require.Equal(t, "deepseek-flash", p.UpstreamModelFamily)
	require.Equal(t, "floating", p.UpstreamVersionPolicy)
	_, compiled := modelprofile.Lookup(p.ID, p.Version)
	require.False(t, compiled)
	var effort Binding
	for i, row := range bundle.BindingDelta {
		require.Equal(t, d.Digest(), row.ProfileDefinitionDigest)
		selector := bundle.SelectorDelta[i]
		require.Equal(t, row.CapabilityID, selector.CapabilityID)
		require.Nil(t, selector.ExpectedPredecessor)
		require.Equal(t, row.ProbeContractVersion, selector.Final.ProbeContractVersion)
		b, e := BindDefinition(Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID, ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion, ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion, ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}, d)
		require.NoError(t, e)
		key, e := SharedStreamProbeContract(b)
		require.NoError(t, e)
		require.Equal(t, b.ProbeContractID+"@"+b.ProbeContractVersion, key)
		if b.CapabilityID == modelprofile.CapabilityReasoningEffort {
			effort = b
		}
	}
	require.Equal(t, DeepSeekCurrentFlashDefaultEffortProbeVersion, effort.ProbeContractVersion)
	return effort, d
}

func TestCurrentDeepSeekFlashDefinitionBundleAndProbeV3(t *testing.T) {
	b, d := currentFlashBinding(t)
	request, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(request, &body))
	require.Equal(t, "deepseek-flash", body["model"])
	require.Equal(t, "high", body["reasoning_effort"])
	require.Equal(t, map[string]any{"type": "enabled"}, body["thinking"])
	require.Equal(t, true, body["stream"])
	require.Equal(t, map[string]any{"include_usage": true}, body["stream_options"])
	require.NotContains(t, body, "tool_choice")
	for _, mutate := range []func(*Binding){
		func(x *Binding) { x.ExpectedUpstreamModel = "deepseek-v4-flash" },
		func(x *Binding) { x.ExecutionProfileID = "deepseek.v4-flash.api" },
		func(x *Binding) { x.ExecutionProfileVersion = "1.0.1" },
		func(x *Binding) { x.ProbeContractVersion = "2.0.0" },
		func(x *Binding) { x.CapabilityID = modelprofile.CapabilityTools },
		func(x *Binding) { x.ExpectedProvider = "other" },
		func(x *Binding) { x.ProtocolProfileID = "protocol.deepseek-openai-chat.v4" },
	} {
		bad := b
		mutate(&bad)
		_, err = BuildOpenAIChatFunctionToolsProbeRequest(bad)
		require.Error(t, err)
		_, err = SharedStreamProbeContract(bad)
		require.Error(t, err)
	}
	// General composition support is independent of a single Profile address.
	p := d.Profile()
	p.ID = "fixture.current-flash"
	p.Version = "99.0.0"
	revised, err := modelprofile.NewDefinition(p, d.ProductLineageID(), "")
	require.NoError(t, err)
	changed := b
	changed.ExecutionProfileID = p.ID
	changed.ExecutionProfileVersion = p.Version
	_, err = BindDefinition(changed, revised)
	require.NoError(t, err)
	for _, mutate := range []func(*modelprofile.Profile){
		func(p *modelprofile.Profile) { p.ReasoningProfile = "reasoning.deepseek-v4-pro-thinking-toggle.v5" },
		func(p *modelprofile.Profile) { p.UpstreamModelFamily = "other" },
		func(p *modelprofile.Profile) { p.UpstreamVersionPolicy = "pinned" },
		func(p *modelprofile.Profile) { p.UpstreamModelIDs = []string{"deepseek-flash", "other"} },
	} {
		wrong := d.Profile()
		mutate(&wrong)
		definition, e := modelprofile.NewDefinition(wrong, d.ProductLineageID(), "")
		if e == nil {
			_, e = BindDefinition(b, definition)
		}
		require.Error(t, e)
	}
}

func TestCurrentDeepSeekFlashProbeV3StrictResponseContract(t *testing.T) {
	b, _ := currentFlashBinding(t)
	fixture := "data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"deepseek-flash","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"brief","tool_calls":[{"index":0,"id":"call_echo","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}]}` + "\n\n" +
		"data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"deepseek-flash","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":10,"prompt_cache_miss_tokens":90}}` + "\n\ndata: [DONE]\n\n"
	observed, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(fixture))
	require.NoError(t, err)
	require.True(t, observed.ReasoningObserved)
	require.True(t, observed.ToolCallObserved)
	require.EqualValues(t, 120, observed.TotalTokens)
	for name, bad := range map[string]string{
		"missing identity":    strings.Replace(fixture, `"model":"deepseek-flash",`, "", 1),
		"wrong identity type": strings.Replace(fixture, `"model":"deepseek-flash"`, `"model":42`, 1),
		"retired model":       strings.Replace(fixture, `"model":"deepseek-flash"`, `"model":"deepseek-v4-flash"`, 1),
		"arbitrary model":     strings.ReplaceAll(fixture, `"model":"deepseek-flash"`, `"model":"other"`),
		"duplicate model":     strings.Replace(fixture, `"model":"deepseek-flash"`, `"model":"deepseek-flash","model":"deepseek-flash"`, 1),
		"bad json":            "data: {private_marker}\n\n",
		"missing usage":       strings.Replace(fixture, `,"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":10,"prompt_cache_miss_tokens":90}`, "", 1),
		"usage type":          strings.Replace(fixture, `"total_tokens":120`, `"total_tokens":"private_marker"`, 1),
		"usage conflict":      strings.Replace(fixture, `"prompt_cache_miss_tokens":90`, `"prompt_cache_miss_tokens":99`, 1),
		"missing done":        strings.Replace(fixture, "data: [DONE]\n\n", "", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, e := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(bad))
			require.Error(t, e)
			diagnostic, _ := json.Marshal(DescribeOpenAIChatFunctionToolsDiagnostic(e))
			require.NotContains(t, string(diagnostic), "private_marker")
		})
	}
}
