package adapters

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
)

func highspeedIndexInput(metadata string) ResponseInput {
	return ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: 200,
		Channel:       models.Channel{Provider: "minimax", AdapterType: TypeOpenAICompatible},
		UpstreamModel: "MiniMax-M2.7-highspeed", ModelOperationID: modelprofile.OperationOpenAIChat,
		ExecutionProfileID: "minimax.m2.7-highspeed.payg", ExecutionProfileVersion: "1.2.0",
		ProtocolProfile: "protocol.minimax-m2-openai-chat.v7", OperationProtocolProfileID: "protocol.minimax-m2-openai-chat.v7",
		OperationProtocolProfileVersion: "1.0.0", CacheUsageProfile: "cache-usage.minimax-openai.v2",
		Body: []byte(`{"model":"MiniMax-M2.7-highspeed","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"<think>fixture</think>","tool_calls":[{` + metadata + `"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"value\":\"fixture\"}"}}]}}],"usage":{"prompt_tokens":600,"completion_tokens":7,"total_tokens":607,"prompt_tokens_details":{"cached_tokens":128}}}`),
	}
}

func TestMiniMaxHighspeedExactToolIndexNormalization(t *testing.T) {
	for _, metadata := range []string{"", `"index":0,`, `"index":999999999999999999999999999999999999,`} {
		in := highspeedIndexInput(metadata)
		out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
		require.NoError(t, err)
		var payload struct {
			Choices []struct {
				Index   int `json:"index"`
				Message struct {
					ToolCalls []map[string]json.RawMessage `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}
		require.NoError(t, json.Unmarshal(out.Body, &payload))
		require.Equal(t, 0, payload.Choices[0].Index)
		call := payload.Choices[0].Message.ToolCalls[0]
		require.NotContains(t, call, "index")
		require.Equal(t, `"call_1"`, string(call["id"]))
		require.Equal(t, `"function"`, string(call["type"]))
		require.JSONEq(t, `{"name":"echo","arguments":"{\"value\":\"fixture\"}"}`, string(call["function"]))
		require.Equal(t, 600, out.Usage.PromptTokens)
		require.Equal(t, 7, out.Usage.CompletionTokens)
		require.NotNil(t, out.DeliveryObject)
	}
}

func TestMiniMaxHighspeedToolIndexInvalidAndAmbiguous(t *testing.T) {
	for _, raw := range []string{`null`, `"0"`, `-1`, `0.0`, `1e0`, `true`, `[]`, `{}`} {
		t.Run(raw, func(t *testing.T) {
			in := highspeedIndexInput(`"index":` + raw + `,`)
			out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
			require.NoError(t, err)
			require.JSONEq(t, string(in.Body), string(out.Body))
			require.Equal(t, 607, out.Usage.TotalTokens)
		})
	}
	for _, metadata := range []string{`"index":0,"index":1,`, `"index":0,"in\u0064ex":1,`} {
		out, err := NewOpenAICompatible().ParseResponse(context.Background(), highspeedIndexInput(metadata))
		require.Error(t, err)
		require.Nil(t, out, "ambiguous bytes cannot establish provider usage")
	}
}

func TestMiniMaxHighspeedToolIndexExactScopeAndHistory(t *testing.T) {
	for name, mutate := range map[string]func(*ResponseInput){
		"provider":           func(i *ResponseInput) { i.Channel.Provider = "other" },
		"model":              func(i *ResponseInput) { i.UpstreamModel = "MiniMax-M2.7" },
		"operation":          func(i *ResponseInput) { i.ModelOperationID = "openai.responses.v1" },
		"protocol":           func(i *ResponseInput) { i.ProtocolProfile = "openai.chat_completions" },
		"operation protocol": func(i *ResponseInput) { i.OperationProtocolProfileID = "openai.chat_completions" },
		"protocol version":   func(i *ResponseInput) { i.OperationProtocolProfileVersion = "2.0.0" },
	} {
		t.Run(name, func(t *testing.T) {
			in := highspeedIndexInput(`"index":0,`)
			mutate(&in)
			_, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
			require.Error(t, err)
		})
	}
	for _, version := range []string{"1.0.0", "1.1.0", ""} {
		in := highspeedIndexInput(`"index":0,`)
		in.ExecutionProfileVersion = version
		out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
		require.NoError(t, err)
		require.JSONEq(t, string(in.Body), string(out.Body))
	}
	for _, model := range []string{"MiniMax-M2.7", "MiniMax-M3"} {
		in := highspeedIndexInput(`"index":0,`)
		in.ExecutionProfileID, in.UpstreamModel = "other-minimax-profile", model
		out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
		require.NoError(t, err)
		require.JSONEq(t, string(in.Body), string(out.Body))
	}
	// The exported triple-only legacy API must not silently widen old Profiles.
	in := highspeedIndexInput(`"index":0,`)
	out, changed, err := NormalizeOpenAICompatibleChatToolCallIndex("minimax", in.UpstreamModel, in.ProtocolProfile, in.Body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, in.Body, out)
	// This shared mechanical helper cannot remove streaming delta indexes.
	delta := []byte(strings.Replace(string(in.Body), `"message":`, `"delta":`, 1))
	out, changed, err = normalizeOpenAICompatibleChatToolCallIndexDocument(delta)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, delta, out)
}
