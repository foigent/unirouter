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

func meaningfulProbeBindings(t *testing.T) (modelprofile.Definition, []Binding) {
	t.Helper()
	wire, err := os.ReadFile("../../capability-bundles/deepseek-v41-flash-provider-completion.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	bindings := []Binding{}
	for _, row := range bundle.BindingDelta {
		b, err := BindDefinition(Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID,
			ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion,
			ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion,
			ProbeContractID: row.ProbeContractID, ProbeContractVersion: DeepSeekMeaningfulStreamProbeVersion,
			ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}, d)
		require.NoError(t, err)
		bindings = append(bindings, b)
	}
	return d, bindings
}

func TestMeaningfulStreamProbeExactTaskOracleAndRetainedBundle(t *testing.T) {
	d, bindings := meaningfulProbeBindings(t)
	wire, err := os.ReadFile("../../capability-bundles/deepseek-flash-meaningful-stream-probe.json")
	require.NoError(t, err)
	require.NoError(t, ValidateBundleSource(wire))
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	require.Empty(t, bundle.Definitions)
	require.Len(t, bundle.BindingDelta, 6)
	require.Len(t, bundle.SelectorDelta, 6)
	for i, b := range bindings {
		body, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
		require.NoError(t, err)
		require.Len(t, body, 780)
		require.Equal(t, "32c62ada6083f2fd5c46321c51039dafc18d417babe2cd7676dbd937ee2b2a0c", fmt.Sprintf("%x", sha256.Sum256(body)))
		group, err := SharedStreamProbeContract(b)
		require.NoError(t, err)
		require.Equal(t, DeepSeekProviderCompletionProbeContractID+"@2.0.0", group)
		require.Equal(t, d.Digest(), bundle.BindingDelta[i].ProfileDefinitionDigest)
		require.NoError(t, validateBundleBinding(bundle.BindingDelta[i], d), "resolve retained Definition explicitly")
		require.Equal(t, "1.0.0", bundle.SelectorDelta[i].ExpectedPredecessor.ProbeContractVersion)
		require.Equal(t, "2.0.0", bundle.SelectorDelta[i].Final.ProbeContractVersion)
		old := b
		old.ProbeContractVersion = "1.0.0"
		oldBody, err := BuildOpenAIChatFunctionToolsProbeRequest(old)
		require.NoError(t, err)
		require.Len(t, oldBody, 569)
		require.Equal(t, "aaed5a44f59621e5d1510a856b2d9b15263952670a17b6f070ab9f64a7c49e99", fmt.Sprintf("%x", sha256.Sum256(oldBody)))
	}
	// An independent oracle proves the fixed task, rather than assuming the
	// expected literal or claiming a synthetic fixture is Provider evidence.
	answers := []int{}
	for n := 1000; n <= 9999; n++ {
		sum := 0
		for k := n; k > 0; k /= 10 {
			sum += k % 10
		}
		if n%7 == 3 && n%11 == 5 && n%13 == 7 && sum == 21 {
			answers = append(answers, n)
		}
	}
	require.Equal(t, []int{6429}, answers)
}

func TestMeaningfulStreamProbeReusesSecondCompatibleDefinition(t *testing.T) {
	d, bindings := meaningfulProbeBindings(t)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(d.CanonicalBytes(), &raw))
	profile := raw["profile"].(map[string]any)
	profile["id"], profile["version"] = "fixture.compatible-flash.api", "99.0.0"
	changed, err := json.Marshal(raw)
	require.NoError(t, err)
	second, err := modelprofile.DecodeDefinition(changed)
	require.NoError(t, err)
	b := bindings[0]
	b.ExecutionProfileID, b.ExecutionProfileVersion = second.Profile().ID, second.Profile().Version
	b, err = BindDefinition(b, second)
	require.NoError(t, err)
	body, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
	require.NoError(t, err)
	require.Equal(t, "32c62ada6083f2fd5c46321c51039dafc18d417babe2cd7676dbd937ee2b2a0c", fmt.Sprintf("%x", sha256.Sum256(body)))
	for name, mutate := range map[string]func(*Binding){
		"unknown version":        func(b *Binding) { b.ProbeContractVersion = "99.0.0" },
		"other provider":         func(b *Binding) { b.ExpectedProvider = "glm" },
		"wrong protocol":         func(b *Binding) { b.ProtocolProfileID = "protocol.deepseek-openai-chat.v5" },
		"wrong alias":            func(b *Binding) { b.ExpectedUpstreamModel = "deepseek-v4-flash" },
		"unsupported capability": func(b *Binding) { b.CapabilityID = "tool_choice_required" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := b
			mutate(&bad)
			_, err := BuildOpenAIChatFunctionToolsProbeRequest(bad)
			require.Error(t, err)
			_, err = ValidateOpenAIChatStreamFunctionToolsForBinding(bad, "text/event-stream", strings.NewReader(""))
			require.Error(t, err)
		})
	}
}

func TestMeaningfulStreamProbeStrictOracleAndLegacyIsolation(t *testing.T) {
	_, bindings := meaningfulProbeBindings(t)
	b := bindings[0]
	fixture, err := os.ReadFile("../adapters/testdata/deepseek_provider_completion_v6.sse")
	require.NoError(t, err)
	valid := strings.ReplaceAll(string(fixture), "UNIROUTER_TOOL_PROBE_OK", "6429")
	observation, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(valid))
	require.NoError(t, err)
	require.True(t, observation.ToolCallObserved)
	require.True(t, observation.ReasoningObserved)
	old := b
	old.ProbeContractVersion = "1.0.0"
	_, err = ValidateOpenAIChatStreamFunctionToolsForBinding(old, "text/event-stream", strings.NewReader(string(fixture)))
	require.NoError(t, err)
	_, err = ValidateOpenAIChatStreamFunctionToolsForBinding(old, "text/event-stream", strings.NewReader(valid))
	require.Error(t, err)
	for name, bad := range map[string]string{
		"old oracle":           string(fixture),
		"wrong value":          strings.ReplaceAll(valid, "6429", "0000"),
		"numeric value":        strings.ReplaceAll(valid, `\"6429\"}`, `6429}`),
		"extra argument":       strings.ReplaceAll(valid, `\"6429\"}`, `\"6429\",\"extra\":true}`),
		"duplicate argument":   strings.ReplaceAll(valid, `\"6429\"}`, `\"6429\",\"value\":\"6429\"}`),
		"missing argument":     strings.ReplaceAll(strings.ReplaceAll(valid, `{\"value\":`, `{`), `\"6429\"}`, `}`),
		"wrong function":       strings.ReplaceAll(valid, "unirouter_probe_echo", "different_function"),
		"two calls":            strings.Replace(valid, `"tool_calls":[{`, `"tool_calls":[{"index":1,"id":"call_extra","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"6429\"}"}},{`, 1),
		"missing DONE":         strings.ReplaceAll(valid, "data: [DONE]\n\n", ""),
		"wrong terminal":       strings.ReplaceAll(valid, `"finish_reason":"tool_calls"`, `"finish_reason":"stop"`),
		"length terminal":      strings.ReplaceAll(valid, `"finish_reason":"tool_calls"`, `"finish_reason":"length"`),
		"missing usage":        strings.ReplaceAll(valid, `,"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":10,"prompt_cache_miss_tokens":90}`, ""),
		"usage sum":            strings.ReplaceAll(valid, `"total_tokens":120`, `"total_tokens":119`),
		"cache exceeds input":  strings.ReplaceAll(valid, `"prompt_cache_hit_tokens":10`, `"prompt_cache_hit_tokens":101`),
		"response overflow":    strings.Repeat(": padding\n\n", OpenAIChatFunctionToolsMaxResponseBytes/8) + valid,
		"completion overrun":   strings.ReplaceAll(strings.ReplaceAll(valid, `"completion_tokens":20`, `"completion_tokens":2049`), `"total_tokens":120`, `"total_tokens":2149`),
		"wrong model":          strings.ReplaceAll(valid, "deepseek-flash", "different-model"),
		"no reasoning":         strings.ReplaceAll(valid, `"reasoning_content":"brief"`, `"reasoning_content":""`),
		"whitespace reasoning": strings.ReplaceAll(valid, `"reasoning_content":"brief"`, `"reasoning_content":"   "`),
		"null reasoning":       strings.ReplaceAll(valid, `"reasoning_content":"brief"`, `"reasoning_content":null`),
		"missing reasoning":    strings.ReplaceAll(valid, `"reasoning_content":"brief",`, ""),
		"truncated":            valid[:len(valid)/2],
	} {
		t.Run(name, func(t *testing.T) {
			require.NotEqual(t, valid, bad, "negative must actually mutate the wire")
			_, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(bad))
			require.Error(t, err)
		})
	}
}
