package modelcapabilityqualification

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"os"
	"strings"
	"testing"
)

func TestStreamTaskReuseAcrossLegallyBoundProviderProtocols(t *testing.T) {
	_, deepseek := meaningfulProbeBindings(t)
	b := deepseek[0]
	b.ProbeContractVersion = "1.0.0"
	p, ok := modelprofile.Lookup("kimi.k3.api", "1.9.0")
	require.True(t, ok)
	p.Version = "99.0.0"
	p.ProtocolProfile = "protocol.kimi-openai-chat.k3.v6"
	p.ReasoningProfile = "reasoning.kimi-k3-effort.v2"
	d, err := modelprofile.NewDefinition(p, "kimi/kimi-k3", "")
	require.NoError(t, err)
	kimi, err := BindDefinition(Binding{OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: "1.0.0", ProbeContractID: KimiStreamFunctionToolsProbeContractID, ProbeContractVersion: "1.0.0", ExpectedProvider: p.Provider, ExpectedUpstreamModel: p.UpstreamModelIDs[0], PublicPath: "/v1/chat/completions"}, d)
	require.NoError(t, err)
	deepWire, err := os.ReadFile("../adapters/testdata/deepseek_provider_completion_v6.sse")
	require.NoError(t, err)
	kimiWire := "data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"kimi-k3","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"brief","tool_calls":[{"index":0,"id":"call_echo","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}]}` + "\n\n" + "data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"kimi-k3","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls","usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"cached_tokens":0}}]}` + "\n\ndata: [DONE]\n\n"
	for _, tc := range []struct {
		binding Binding
		wire    string
	}{{b, string(deepWire)}, {kimi, kimiWire}} {
		t.Run(tc.binding.ExpectedProvider, func(t *testing.T) {
			request, err := BuildOpenAIChatFunctionToolsProbeRequest(tc.binding)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(request, &payload))
			task, err := streamToolProbeScenarioForBinding(tc.binding)
			require.NoError(t, err)
			require.Equal(t, legacyStreamToolProbeScenario(), task)
			require.Contains(t, string(request), task.prompt)
			require.Contains(t, string(request), task.functionName)
			result, err := ValidateOpenAIChatStreamFunctionToolsForBinding(tc.binding, "text/event-stream", strings.NewReader(tc.wire))
			require.NoError(t, err)
			require.True(t, result.ToolCallObserved)
			for _, bad := range []string{strings.ReplaceAll(tc.wire, "UNIROUTER_TOOL_PROBE_OK", "wrong"), strings.ReplaceAll(tc.wire, "data: [DONE]\n\n", "")} {
				_, err := ValidateOpenAIChatStreamFunctionToolsForBinding(tc.binding, "text/event-stream", strings.NewReader(bad))
				require.Error(t, err)
			}
			other := tc.binding
			other.ExpectedProvider = "unbound-provider"
			_, err = BuildOpenAIChatFunctionToolsProbeRequest(other)
			require.Error(t, err, "shared mechanics never grant a cross-provider binding")
		})
	}
	// The new task uses the same provider-neutral argument oracle. This is local
	// algorithm reuse, not qualification of the new task on another Provider.
	task, err := streamToolProbeScenarioForBinding(deepseek[0])
	require.NoError(t, err)
	require.True(t, task.matchesArguments(`{"value":"6429"}`))
	for _, bad := range []string{`{"value":6429}`, `{"value":"wrong"}`, `{"value":"6429","value":"6429"}`, `{"value":"6429","extra":true}`} {
		require.False(t, task.matchesArguments(bad))
	}
}
