package modelcapabilityqualification

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func m27StreamTestBinding() Binding {
	return Binding{OperationID: "openai.chat_completions.v1", CapabilityID: "function_tools", ExecutionProfileID: "minimax.m2.7.payg", ExecutionProfileVersion: "1.3.0", ProtocolProfileID: "protocol.minimax-m27-openai-chat.v1", ProtocolProfileVersion: "1.0.0", ProbeContractID: MiniMaxM27StreamFunctionToolsProbeContractID, ProbeContractVersion: "1.0.0", ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M2.7", PublicPath: "/v1/chat/completions"}
}

func TestMiniMaxM27StreamProbeUsesSharedFixtureAndExactIdentity(t *testing.T) {
	b := m27StreamTestBinding()
	body, err := BuildMiniMaxM27StreamFunctionToolsProbeRequest(b)
	require.NoError(t, err)
	var request map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &request))
	require.Equal(t, true, request["stream"])
	require.Equal(t, "auto", request["tool_choice"])
	tools, err := json.Marshal(openAIChatProbeTools())
	require.NoError(t, err)
	actualTools, err := json.Marshal(request["tools"])
	require.NoError(t, err)
	require.JSONEq(t, string(tools), string(actualTools))
	fixture, err := os.ReadFile("../adapters/testdata/minimax_m27_stream_tools.sse")
	require.NoError(t, err)
	for _, suffix := range []string{"", "data: [DONE]\n\ndata: trailing-do-not-consume\n\n"} {
		observation, err := ValidateMiniMaxM27StreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(string(fixture)+suffix))
		require.NoError(t, err)
		require.True(t, observation.ToolCallObserved)
		require.True(t, observation.ReasoningObserved)
		require.Equal(t, int64(5585), observation.PromptTokens)
		require.False(t, observation.CacheReadCounterObserved)
	}
	for _, change := range []func(*Binding){func(x *Binding) { x.ExecutionProfileVersion = "1.2.0" }, func(x *Binding) { x.ExpectedUpstreamModel = "MiniMax-M2.7-highspeed" }, func(x *Binding) { x.CapabilityID = "cache_read_usage" }, func(x *Binding) { x.ProbeContractVersion = "2.0.0" }} {
		bad := b
		change(&bad)
		_, err := BuildMiniMaxM27StreamFunctionToolsProbeRequest(bad)
		require.Error(t, err)
	}
	for _, bad := range []string{
		strings.ReplaceAll(string(fixture), `"index":0`, `"index":1`),
		strings.Replace(string(fixture), `"type":"function"`, `"type":"function","unexpected":true`, 1),
		strings.Replace(string(fixture), `"model":"MiniMax-M2.7"`, `"model":"MiniMax-M2.7","model":"MiniMax-M2.7"`, 1),
		strings.ReplaceAll(string(fixture), `"finish_reason":"tool_calls"`, `"finish_reason":"length"`),
		string(fixture[:strings.LastIndex(string(fixture), "data:")]),
	} {
		_, err := ValidateMiniMaxM27StreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(bad))
		require.Error(t, err)
	}
}
