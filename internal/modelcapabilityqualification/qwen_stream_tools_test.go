package modelcapabilityqualification

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func qwenDataBinding(t *testing.T, capability string) Binding {
	return qwenDataContractBinding(t, capability, false)
}

func qwenDataContractBinding(t *testing.T, capability string, providerCompletion bool) Binding {
	p, ok := modelprofile.Lookup("qwen.3.7-flash.api", "1.7.0")
	require.True(t, ok)
	p.Version = "1.8.0"
	p.ProtocolProfile = "protocol.qwen-openai-chat.flash.v7"
	probe := QwenStreamFunctionToolsProbeContractID
	if providerCompletion {
		p.Version, p.ProtocolProfile = "1.9.0", "protocol.qwen-openai-chat.flash.v8"
		probe = QwenProviderCompletionProbeContractID
	}
	p.Capabilities = []string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningBudget, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto}
	p.EvidenceRefs = []string{"https://help.aliyun.com/zh/model-studio/qwen3-7-flash", "https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions", "https://help.aliyun.com/zh/model-studio/qwen-function-calling"}
	d, err := modelprofile.NewDefinition(p, "dashscope/qwen3.7-flash", "")
	require.NoError(t, err)
	b, err := BindDefinition(Binding{OperationID: modelprofile.OperationOpenAIChat, CapabilityID: capability, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: "1.0.0", ProbeContractID: probe, ProbeContractVersion: "1.0.0", ExpectedProvider: "dashscope", ExpectedUpstreamModel: "qwen3.7-flash", PublicPath: "/v1/chat/completions"}, d)
	require.NoError(t, err)
	return b
}
func TestQwenDataProbeRequiresExactInstalledComposition(t *testing.T) {
	for _, cap := range []string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningBudget, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto} {
		b := qwenDataBinding(t, cap)
		body, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
		require.NoError(t, err)
		var request map[string]any
		require.NoError(t, json.Unmarshal(body, &request))
		require.Equal(t, float64(2048), request["max_completion_tokens"])
		require.Equal(t, float64(1024), request["thinking_budget"])
		require.Equal(t, true, request["stream"])
		require.Equal(t, true, request["enable_thinking"])
		require.NotContains(t, request, "max_tokens")
		for _, bad := range []Binding{func() Binding { x := b; x.ExpectedProvider = "qwen"; return x }(), func() Binding { x := b; x.ProtocolProfileID = "protocol.qwen-openai-chat.flash.v6"; return x }(), func() Binding { x := b; x.ProbeContractVersion = "2.0.0"; return x }()} {
			_, err := BuildQwenStreamFunctionToolsProbeRequest(bad)
			require.Error(t, err)
		}
	}
}
func TestQwenProbeAssemblesDocumentedWireAndRejectsIncompleteFacts(t *testing.T) {
	b := qwenDataBinding(t, modelprofile.CapabilityFunctionTools)
	wire := qwenProbeWire()
	observation, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(wire))
	require.NoError(t, err)
	require.True(t, observation.ToolCallObserved)
	require.True(t, observation.ReasoningObserved)
	require.Equal(t, int64(12), observation.TotalTokens)
	require.False(t, observation.CacheReadCounterObserved)
	for _, bad := range []string{strings.ReplaceAll(wire, "data: [DONE]\n\n", ""), strings.ReplaceAll(wire, `"model":"qwen3.7-flash"`, `"model":"other"`), strings.Replace(wire, `"reasoning_content":"think"`, `"reasoning_content":""`, 1), strings.Replace(wire, `"id":"call-a"`, `"id":""`, 1), strings.Replace(wire, `"finish_reason":"tool_calls"`, `"finish_reason":"length"`, 1), strings.Replace(wire, `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}`, `null`, 1)} {
		_, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(bad))
		require.Error(t, err)
	}
}

func qwenProbeWire() string {
	frame := func(choices, usage string) string {
		return `data: {"id":"chat-a","object":"chat.completion.chunk","created":1,"model":"qwen3.7-flash","system_fingerprint":null,"service_tier":null,"choices":` + choices + `,"usage":` + usage + "}\n\n"
	}
	return frame(`[{"index":0,"logprobs":null,"delta":{"role":"assistant","reasoning_content":"think","tool_calls":[{"index":0,"id":"call-a","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":"}}]},"finish_reason":null}]`, `null`) +
		frame(`[{"index":0,"delta":{"role":null,"tool_calls":[{"index":0,"id":"","type":"function","function":{"arguments":"\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"finish_reason":"tool_calls"}]`, `null`) +
		frame(`[]`, `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}`) + "data: [DONE]\n\n"
}

func TestQwenProviderCompletionProbePinsAllowanceAndFinalUsage(t *testing.T) {
	b := qwenDataContractBinding(t, modelprofile.CapabilityFunctionTools, true)
	body, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, json.Unmarshal(body, &request))
	require.Equal(t, float64(2038), request["max_completion_tokens"])
	require.NotContains(t, request, "max_tokens")
	for _, wrongProbe := range []string{QwenStreamFunctionToolsProbeContractID, "unknown"} {
		bad := b
		bad.ProbeContractID = wrongProbe
		_, err = BuildOpenAIChatFunctionToolsProbeRequest(bad)
		require.Error(t, err)
	}
	for _, n := range []int{2048, 2049} {
		wire := strings.Replace(qwenProbeWire(), `"completion_tokens":2,"total_tokens":12`, fmt.Sprintf(`"completion_tokens":%d,"total_tokens":%d`, n, n+10), 1)
		observation, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(wire))
		require.EqualValues(t, n, observation.CompletionTokens, "invalid overrun retains supplier evidence")
		if n == 2048 {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestQwenHostedProbeRequiresExactPartitionAndModel(t *testing.T) {
	raw, err := os.ReadFile("../../capability-bundles/qwen-beijing-hosted-chat.json")
	require.NoError(t, err)
	require.NoError(t, ValidateBundleSource(raw))
	bundle, err := publication.DecodeBundleV2(raw)
	require.NoError(t, err)
	require.Len(t, bundle.Definitions, 2)
	require.Len(t, bundle.BindingDelta, 12)
	for _, encoded := range bundle.Definitions {
		d, err := modelprofile.DecodeDefinition(encoded)
		require.NoError(t, err)
		p := d.Profile()
		model := p.UpstreamModelIDs[0]
		t.Run(model, func(t *testing.T) {
			b, err := BindDefinition(Binding{
				OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityReasoningBudget,
				ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version,
				ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
				ProbeContractID: QwenStreamFunctionToolsProbeContractID, ProbeContractVersion: QwenHostedStreamToolsProbeVersion,
				ExpectedProvider: p.Provider, ExpectedUpstreamModel: model, PublicPath: "/v1/chat/completions",
			}, d)
			require.NoError(t, err)
			got, err := SharedProbeContract(b)
			require.NoError(t, err)
			require.Equal(t, QwenStreamFunctionToolsProbeContractID+"@"+QwenHostedStreamToolsProbeVersion, got)
			body, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
			require.NoError(t, err)
			var request map[string]any
			require.NoError(t, json.Unmarshal(body, &request))
			require.EqualValues(t, 1014, request["max_tokens"])
			require.EqualValues(t, 512, request["thinking_budget"])
			require.NotContains(t, request, "enable_thinking")
			require.NotContains(t, request, "reasoning_effort")
			require.NotContains(t, request, "max_completion_tokens")
			wire := strings.ReplaceAll(qwenProbeWire(), "qwen3.7-flash", model)
			const old = `"completion_tokens":2,"total_tokens":12`
			for _, tc := range []struct {
				name, usage string
				ok          bool
			}{
				{"within both", `"completion_tokens":1536,"total_tokens":1546,"completion_tokens_details":{"reasoning_tokens":512,"text_tokens":1536}`, true},
				{"reasoning above limit below sum", `"completion_tokens":520,"total_tokens":530,"completion_tokens_details":{"reasoning_tokens":513}`, false},
				{"answer above limit below sum", `"completion_tokens":1030,"total_tokens":1040,"completion_tokens_details":{"reasoning_tokens":5}`, false},
				{"official inclusive text partition", `"completion_tokens":44,"total_tokens":54,"completion_tokens_details":{"reasoning_tokens":15,"text_tokens":44}`, true},
				{"visible only is not text_tokens", `"completion_tokens":44,"total_tokens":54,"completion_tokens_details":{"reasoning_tokens":15,"text_tokens":29}`, false},
				{"missing partition", old, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					stream := strings.Replace(wire, old, tc.usage, 1)
					got, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(stream))
					if tc.ok {
						require.NoError(t, err)
						require.True(t, got.ToolCallObserved)
					} else {
						require.Error(t, err)
					}
				})
			}
			wrong := b
			wrong.ExpectedUpstreamModel = "qwen3.8-flash"
			_, err = BuildOpenAIChatFunctionToolsProbeRequest(wrong)
			require.Error(t, err)
			_, err = ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(strings.ReplaceAll(wire, model, "qwen3.8-flash")))
			require.Error(t, err)
		})
	}
}
