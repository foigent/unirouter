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

func exactGLMResponseInput(model, profileID, version, protocol string, body []byte) ResponseInput {
	return ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: 200, Body: body,
		Channel:       models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		UpstreamModel: model, ModelOperationID: modelprofile.OperationOpenAIChat,
		ExecutionProfileID: profileID, ExecutionProfileVersion: version,
		ProtocolProfile: protocol, OperationProtocolProfileID: protocol,
		OperationProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
	}
}

func TestGLMRequestIDNormalizationExactCurrentTuples(t *testing.T) {
	for _, tuple := range []struct{ model, profile, version, protocol string }{
		{"glm-5.3", "glm.5.3.api", "1.8.0", "protocol.glm-openai-chat.v5"},
		{"glm-5.2", "glm.5.2.api", "1.8.0", "protocol.glm-openai-chat.v6"},
		{"glm-5.1", "glm.5.1.api", "1.0.0", "protocol.glm-openai-chat.v6"},
		{"glm-5-turbo", "glm.5-turbo.api", "1.0.0", "protocol.glm-openai-chat.v6"},
		{"glm-5", "glm.5.api", "1.0.0", "protocol.glm-openai-chat.v6"},
		{"glm-5v-turbo", "glm.5v-turbo.api", "1.11.0", "protocol.glm-openai-chat.glm5v.v5"},
	} {
		t.Run(tuple.model, func(t *testing.T) {
			body := []byte(`{"id":"canonical-id","model":"` + tuple.model + `","request_id":"provider-correlation","unknown_sibling":{"preserved":true},"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
			in := exactGLMResponseInput(tuple.model, tuple.profile, tuple.version, tuple.protocol, body)
			out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
			require.NoError(t, err)
			var object map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(out.Body, &object))
			require.NotContains(t, object, "request_id")
			require.JSONEq(t, `{"preserved":true}`, string(object["unknown_sibling"]))
			require.Equal(t, `"canonical-id"`, string(object["id"]))
			require.Equal(t, 2, out.Usage.PromptTokens)
			require.Equal(t, 3, out.Usage.CompletionTokens)
			for name, mutate := range map[string]func(*ResponseInput){
				"provider":           func(i *ResponseInput) { i.Channel.Provider = "other" },
				"model":              func(i *ResponseInput) { i.UpstreamModel += "-other" },
				"operation":          func(i *ResponseInput) { i.ModelOperationID = "openai.responses.v1" },
				"protocol":           func(i *ResponseInput) { i.ProtocolProfile = "openai.chat_completions" },
				"operation protocol": func(i *ResponseInput) { i.OperationProtocolProfileID = "openai.chat_completions" },
				"protocol version":   func(i *ResponseInput) { i.OperationProtocolProfileVersion = "2.0.0" },
			} {
				t.Run(name, func(t *testing.T) {
					bad := in
					mutate(&bad)
					_, err := NewOpenAICompatible().ParseResponse(context.Background(), bad)
					require.Error(t, err)
				})
			}
		})
	}
}

func TestGLMRequestIDNormalizationRetainsInvalidMetadataAndUsage(t *testing.T) {
	for _, raw := range []string{`null`, `123`, `true`, `[]`, `{}`} {
		body := []byte(`{"request_id":` + raw + `,"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
		in := exactGLMResponseInput("glm-5.3", "glm.5.3.api", "1.8.0", "protocol.glm-openai-chat.v5", body)
		out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
		require.NoError(t, err)
		require.Equal(t, body, out.Body)
		require.Equal(t, 5, out.Usage.TotalTokens)
	}
	for _, value := range []string{"", "响应关联", strings.Repeat("x", 4096)} {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		body := []byte(`{"request_id":` + string(raw) + `,"choices":[]}`)
		in := exactGLMResponseInput("glm-5.3", "glm.5.3.api", "1.8.0", "protocol.glm-openai-chat.v5", body)
		out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
		require.NoError(t, err)
		require.NotContains(t, string(out.Body), "request_id")
	}
	for _, body := range []string{
		`{"request_id":"a","request_id":"b","choices":[]}`,
		`{"request_id":"a","request_\u0069d":"b","choices":[]}`,
		`{"request_id":"a","usage":{"prompt_tokens":2,"prompt_tokens":3}}`,
		`{"request_id":`,
	} {
		in := exactGLMResponseInput("glm-5.3", "glm.5.3.api", "1.8.0", "protocol.glm-openai-chat.v5", []byte(body))
		out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
		require.Error(t, err)
		require.Nil(t, out, "ambiguous usage must not be established")
	}
}

func TestGLMRequestIDNormalizationDoesNotBroadenOtherTuples(t *testing.T) {
	body := []byte(`{"request_id":"retained","choices":[]}`)
	for _, in := range []ResponseInput{
		exactGLMResponseInput("glm-5.3", "glm.5.3.api", "1.6.0", "protocol.glm-openai-chat.v5", body),
		exactGLMResponseInput("glm-5.3", "glm.5.3.api", "1.7.0", "protocol.glm-openai-chat.v5", body),
		{Endpoint: EndpointChatCompletions, StatusCode: 200, Body: body, Channel: models.Channel{Provider: "glm"}, ProtocolProfile: "openai.chat_completions"},
		{Endpoint: EndpointChatCompletions, StatusCode: 200, Body: body, Channel: models.Channel{Provider: "minimax"}},
	} {
		out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
		require.NoError(t, err)
		require.Equal(t, body, out.Body)
	}
	withoutID := []byte(`{"choices":[]}`)
	in := exactGLMResponseInput("glm-5.3", "glm.5.3.api", "1.8.0", "protocol.glm-openai-chat.v5", withoutID)
	out, err := NewOpenAICompatible().ParseResponse(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, withoutID, out.Body)
}
