package modelcapabilityqualification

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGLMStreamProbeExactBindingsAndSharedFixture(t *testing.T) {
	all, err := allBindings()
	require.NoError(t, err)
	fixture, err := os.ReadFile("../adapters/testdata/glm_stream_tools.sse")
	require.NoError(t, err)
	count := 0
	for _, b := range all {
		if b.ProbeContractID != GLMStreamFunctionToolsProbeContractID {
			continue
		}
		count++
		request, err := BuildOpenAIChatFunctionToolsProbeRequest(b)
		require.NoError(t, err)
		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(request, &payload))
		require.Equal(t, float64(1024), payload["max_tokens"])
		require.Equal(t, true, payload["stream"])
		require.Equal(t, "enabled", payload["thinking"].(map[string]interface{})["type"])
		wire := strings.ReplaceAll(string(fixture), "glm-5.3", b.ExpectedUpstreamModel)
		obs, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(wire))
		require.NoError(t, err, b.ExecutionProfileID)
		require.True(t, obs.ToolCallObserved)
		require.True(t, obs.ReasoningObserved)
		require.Equal(t, int64(100), obs.PromptTokens)
		require.Equal(t, int64(10), obs.CachedTokens)
		require.Equal(t, b.ExpectedUpstreamModel, obs.ReportedModel)
		bad := b
		bad.ExpectedUpstreamModel += "-other"
		_, err = BuildOpenAIChatFunctionToolsProbeRequest(bad)
		require.Error(t, err)
		bad = b
		bad.ProbeContractVersion = "2.0.0"
		_, err = BuildOpenAIChatFunctionToolsProbeRequest(bad)
		require.Error(t, err)
	}
	require.Equal(t, 30, count)
}

func TestGLMStreamProbeRejectsSemanticAndWireGaps(t *testing.T) {
	b, err := Resolve("openai.chat_completions.v1", "function_tools", "glm.5.3.api", "1.9.0", "protocol.glm-openai-chat.v7", "1.0.0")
	require.NoError(t, err)
	fixture, err := os.ReadFile("../adapters/testdata/glm_stream_tools.sse")
	require.NoError(t, err)
	s := string(fixture)
	for name, bad := range map[string]string{
		"missing DONE":      strings.ReplaceAll(s, "data: [DONE]", ": no DONE"),
		"missing reasoning": strings.ReplaceAll(s, "Consider the requested echo.", ""),
		"wrong model":       strings.ReplaceAll(s, "glm-5.3", "glm-5.2"),
		"wrong argument":    strings.ReplaceAll(s, "UNIROUTER_TOOL_PROBE_OK", "OTHER"),
		"wrong tool":        strings.ReplaceAll(s, "unirouter_probe_echo", "read"),
		"duplicate key":     strings.Replace(s, `"model":"glm-5.3"`, `"model":"glm-5.3","model":"glm-5.3"`, 1),
		"unknown delta":     strings.Replace(s, `"reasoning_content":`, `"unknown":true,"reasoning_content":`, 1),
		"null index":        strings.Replace(s, `"index":0,"id":"call_echo"`, `"index":null,"id":"call_echo"`, 1),
		"truncated tools":   strings.Replace(s, `"finish_reason":"tool_calls"`, `"finish_reason":"length"`, 1),
		"missing usage":     strings.Replace(s, `"usage":`, `"other_usage":`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(bad))
			require.Error(t, err)
		})
	}
	obs, err := ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(s+"data: private-trailing\n\n"))
	require.NoError(t, err)
	require.True(t, obs.ToolCallObserved)
}
