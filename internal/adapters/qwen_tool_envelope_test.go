package adapters

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

// Synthetic fixtures based on the documented Chat envelope. They do not claim
// a live Flash response. SDK None is not assumed to mean raw JSON null.
func qwenToolFrame(delta, finish, usage string) []byte {
	choices := `[{"index":0,"logprobs":null,"delta":` + delta + `,"finish_reason":` + finish + `}]`
	if delta == "" {
		choices = `[]`
	}
	return []byte(`{"id":"chat-x","object":"chat.completion.chunk","created":1,"model":"qwen3.7-flash","system_fingerprint":null,"service_tier":null,"choices":` + choices + `,"usage":` + usage + `}`)
}
func TestQwenToolPreparationUsesSharedIdentityAndTerminalState(t *testing.T) {
	e, err := NewToolStreamEnvelope("protocol.qwen-openai-chat.flash.v7", "dashscope", "qwen3.7-flash", "cache-usage.qwen-context-cache.v1")
	require.NoError(t, err)
	c, _ := modelprofile.ResolveChatProtocolContract("protocol.qwen-openai-chat.flash.v7")
	state := NewFunctionToolStreamState("qwen3.7-flash")
	frames := [][]byte{
		qwenToolFrame(`{"role":"assistant","content":null,"reasoning_content":"check","function_call":null,"refusal":null,"tool_calls":null}`, `null`, `null`),
		qwenToolFrame(`{"role":null,"tool_calls":[{"index":0,"id":"call-a","type":"function","function":{"name":"read","arguments":"{"}}]}`, `null`, `null`),
		qwenToolFrame(`{"tool_calls":[{"index":0,"id":"","type":"function","function":{"arguments":"}"}}]}`, `"tool_calls"`, `null`),
		qwenToolFrame("", `null`, `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":0}}`),
	}
	for _, wire := range frames {
		body, skip, err := e.Prepare(wire)
		require.NoError(t, err)
		require.False(t, skip)
		facts, err := ParseFunctionToolStreamFactsForContract(body, c.StreamContract)
		require.NoError(t, err)
		require.NoError(t, state.Accept(facts, facts.HasUsage))
	}
	require.NoError(t, state.Finish())
	require.Equal(t, []StreamFunctionCall{{ID: "call-a", Name: "read", Arguments: "{}"}}, state.Calls())
	for _, raw := range []string{
		strings.Replace(string(frames[0]), `"service_tier":null`, `"service_tier":"priority"`, 1),
		strings.Replace(string(frames[0]), `"system_fingerprint":null`, `"system_fingerprint":"fp"`, 1),
		strings.Replace(string(frames[0]), `"refusal":null`, `"refusal":"refused"`, 1),
		strings.Replace(string(frames[0]), `"model":"qwen3.7-flash"`, `"model":"other"`, 1),
	} {
		_, _, err := e.Prepare([]byte(raw))
		require.Error(t, err)
	}
	old, _ := NewToolStreamEnvelope("protocol.qwen-openai-chat.flash.v6", "dashscope", "qwen3.7-flash", "cache-usage.qwen-context-cache.v1")
	require.Nil(t, old)
	_, err = ParseFunctionToolStreamFactsForContract(frames[0], c.StreamContract)
	require.Error(t, err, "strict shared parser remains unchanged")
}
func TestQwenEmptyIDDoesNotInventInitialOrChangedIdentity(t *testing.T) {
	e, _ := NewToolStreamEnvelope("protocol.qwen-openai-chat.flash.v7", "dashscope", "qwen3.7-flash", "cache-usage.qwen-context-cache.v1")
	c, _ := modelprofile.ResolveChatProtocolContract("protocol.qwen-openai-chat.flash.v7")
	accept := func(s *FunctionToolStreamState, delta string) error {
		body, _, err := e.Prepare(qwenToolFrame(delta, `null`, `null`))
		if err != nil {
			return err
		}
		f, err := ParseFunctionToolStreamFactsForContract(body, c.StreamContract)
		if err != nil {
			return err
		}
		return s.Accept(f, false)
	}
	initial := `{"tool_calls":[{"index":0,"id":"call-a","type":"function","function":{"name":"read","arguments":"{"}}]}`
	require.Error(t, accept(NewFunctionToolStreamState("qwen3.7-flash"), strings.Replace(initial, `"call-a"`, `""`, 1)))
	for _, delta := range []string{strings.Replace(initial, `"call-a"`, `"call-b"`, 1), strings.Replace(initial, `"index":0`, `"index":1`, 1), `{"tool_calls":[{"index":0,"id":"","function":{"name":null,"arguments":"}"}}]}`} {
		state := NewFunctionToolStreamState("qwen3.7-flash")
		require.NoError(t, accept(state, initial))
		require.Error(t, accept(state, delta))
	}
}
