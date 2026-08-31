package adapters

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/strictjson"
)

func exactKimiK26ResponseInput(body []byte) ResponseInput {
	return ResponseInput{
		Endpoint:                        EndpointChatCompletions,
		StatusCode:                      http.StatusOK,
		Body:                            body,
		Channel:                         models.Channel{Provider: "kimi", AdapterType: TypeOpenAICompatible},
		UpstreamModel:                   "kimi-k2.6",
		ModelOperationID:                "openai.chat_completions.v1",
		ExecutionProfileID:              "kimi.k2.6.api",
		ExecutionProfileVersion:         "1.7.0",
		ProtocolProfile:                 "protocol.kimi-openai-chat.v5",
		OperationProtocolProfileID:      "protocol.kimi-openai-chat.v5",
		OperationProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
	}
}

func TestKimiRuntimeChatNonStreamMechanicsRequireExactIdentity(t *testing.T) {
	in := exactKimiK26ResponseInput(nil)
	mechanics, found, err := kimiRuntimeChatNonStreamMechanics(in)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ChatNonStreamMechanics{RejectDuplicateMembers: true, NormalizeToolCallIndex: true}, mechanics)

	for name, mutate := range map[string]func(*ResponseInput){
		"operation":        func(in *ResponseInput) { in.ModelOperationID = "openai.responses.v1" },
		"profile version":  func(in *ResponseInput) { in.ExecutionProfileVersion = "1.6.0" },
		"protocol id":      func(in *ResponseInput) { in.OperationProtocolProfileID = "openai.chat_completions" },
		"protocol version": func(in *ResponseInput) { in.OperationProtocolProfileVersion = "2.0.0" },
	} {
		t.Run(name, func(t *testing.T) {
			mismatch := in
			mutate(&mismatch)
			_, found, err := kimiRuntimeChatNonStreamMechanics(mismatch)
			if name == "profile version" {
				require.NoError(t, err)
				require.False(t, found, "historical profiles retain their existing fallback behavior")
				return
			}
			require.Error(t, err)
			require.False(t, found)
		})
	}
}

func TestOpenAICompatibleChatNonStreamPreparationPreservesPublishedMechanics(t *testing.T) {
	exact := ChatNonStreamMechanics{RejectDuplicateMembers: true, NormalizeToolCallIndex: true}
	invalid := ChatNonStreamMechanics{NormalizeToolCallIndex: true}
	require.ErrorIs(t, ValidateOpenAICompatibleChatNonStreamDocument([]byte(`{}`), invalid), errInvalidChatNonStreamMechanics)

	duplicate := []byte(`{"id":"shadow","\u0069d":"effective"}`)
	require.NoError(t, ValidateOpenAICompatibleChatNonStreamDocument(duplicate, ChatNonStreamMechanics{}))
	require.ErrorIs(t, ValidateOpenAICompatibleChatNonStreamDocument(duplicate, exact), strictjson.ErrDuplicateKey)

	for _, body := range [][]byte{
		[]byte(`{"choices":[{"message":{"tool_calls":[{"index":0,"id":"call_1"}]}}]}`),
		[]byte(`{"choices":[{"message":{"tool_calls":[{"index":999999999999999999999999999999999999999,"id":"call_1"}]}}]}`),
	} {
		prepared, err := PrepareOpenAICompatibleChatNonStreamDocument(
			"kimi", "kimi-k2.6", "protocol.kimi-openai-chat.v5",
			modelprofile.ChatOperationProtocolBindingVersionV1, body, exact,
		)
		require.NoError(t, err)
		require.True(t, prepared.Changed)
		require.Equal(t, ChatNonStreamDeliveryIssueNone, prepared.DeliveryIssue)
		require.NotContains(t, string(prepared.Body), `"index"`)
	}

	for name, body := range map[string][]byte{
		"invalid index": []byte(`{"choices":[{"message":{"tool_calls":[{"index":null,"id":"call_1"}]}}]}`),
		"choices type":  []byte(`{"choices":"invalid"}`),
		"message type":  []byte(`{"choices":[{"message":"invalid"}]}`),
		"tools type":    []byte(`{"choices":[{"message":{"tool_calls":"invalid"}}]}`),
		"root array":    []byte(`[]`),
		"root scalar":   []byte(`1`),
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, ValidateOpenAICompatibleChatNonStreamDocument(body, exact))
			prepared, err := PrepareOpenAICompatibleChatNonStreamDocument(
				"kimi", "kimi-k2.6", "protocol.kimi-openai-chat.v5",
				modelprofile.ChatOperationProtocolBindingVersionV1, body, exact,
			)
			require.NoError(t, err)
			require.False(t, prepared.Changed)
			require.Equal(t, ChatNonStreamToolCallNormalizationInvalid, prepared.DeliveryIssue)
			require.Equal(t, body, prepared.Body)
		})
	}

	rootNull := []byte(`null`)
	require.NoError(t, ValidateOpenAICompatibleChatNonStreamDocument(rootNull, exact))
	prepared, err := PrepareOpenAICompatibleChatNonStreamDocument(
		"kimi", "kimi-k2.6", "protocol.kimi-openai-chat.v5",
		modelprofile.ChatOperationProtocolBindingVersionV1, rootNull, exact,
	)
	require.NoError(t, err)
	require.Equal(t, rootNull, prepared.Body)
	require.Equal(t, ChatNonStreamDeliveryIssueNone, prepared.DeliveryIssue)

	_, err = PrepareOpenAICompatibleChatNonStreamDocument(
		"kimi", "kimi-k2.6", "protocol.kimi-openai-chat.v5", "2.0.0", []byte(`{}`), exact,
	)
	require.ErrorIs(t, err, errInvalidChatNonStreamMechanics)

	for _, body := range [][]byte{[]byte(`{"choices":[]`), []byte(`{"n":01}`), []byte(`{} {}`)} {
		require.Error(t, ValidateOpenAICompatibleChatNonStreamDocument(body, exact))
	}
}

func TestOpenAICompatibleParseResponseUsesExactKimiPreparedBodyAndRetainsUsageOnDeliveryIssue(t *testing.T) {
	adapter := NewOpenAICompatible()
	valid := []byte(`{"id":"chatcmpl-kimi","model":"kimi-k2.6","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{}"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	response, err := adapter.ParseResponse(context.Background(), exactKimiK26ResponseInput(valid))
	require.NoError(t, err)
	require.Equal(t, 2, response.Usage.TotalTokens)
	require.NotContains(t, string(response.Body), `"index":0,"id":"call_1"`)
	require.NotNil(t, response.DeliveryObject)
	require.True(t, bytes.Equal(response.Body, response.DeliveryObjectBody))
	require.Equal(t, "kimi-k2.6", response.DeliveryObject["model"])

	for name, replacement := range map[string]string{
		"invalid index": `"index":null,"id":"call_1"`,
		"choices type":  `"choices":"invalid","unused":`,
		"message type":  `"message":"invalid","unused":`,
		"tools type":    `"tool_calls":"invalid","unused":`,
	} {
		t.Run(name, func(t *testing.T) {
			body := valid
			switch name {
			case "invalid index":
				body = []byte(strings.Replace(string(valid), `"index":0,"id":"call_1"`, replacement, 1))
			case "choices type":
				body = []byte(`{"id":"chatcmpl-kimi","model":"kimi-k2.6","choices":"invalid","usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			case "message type":
				body = []byte(`{"id":"chatcmpl-kimi","model":"kimi-k2.6","choices":[{"index":0,"finish_reason":"stop","message":"invalid"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			case "tools type":
				body = []byte(`{"id":"chatcmpl-kimi","model":"kimi-k2.6","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":"invalid"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			}
			response, err := adapter.ParseResponse(context.Background(), exactKimiK26ResponseInput(body))
			require.NoError(t, err)
			require.Equal(t, 2, response.Usage.TotalTokens)
			require.Equal(t, body, response.Body)
			require.NotNil(t, response.DeliveryObject)
		})
	}

	duplicate := []byte(strings.Replace(string(valid), `"id":"call_1"`, `"id":"shadow","id":"call_1"`, 1))
	_, err = adapter.ParseResponse(context.Background(), exactKimiK26ResponseInput(duplicate))
	require.ErrorIs(t, err, strictjson.ErrDuplicateKey)

	mismatch := exactKimiK26ResponseInput(valid)
	mismatch.OperationProtocolProfileVersion = "2.0.0"
	_, err = adapter.ParseResponse(context.Background(), mismatch)
	require.Error(t, err)
}
