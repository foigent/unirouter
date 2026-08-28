package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/models"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOpenAICompatibleBuildRequest_RewritesModelAndURL(t *testing.T) {
	adapter := NewOpenAICompatible()
	req, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointChatCompletions,
		DownstreamBody: []byte(`{"model":"company-chat","messages":[]}`),
		RequestedModel: "company-chat",
		MappedModel:    "gpt-4o",
		Channel:        models.Channel{BaseURL: "https://api.example.com/v1"},
		APIKey:         "sk-test",
		RequestID:      "req-1",
	})
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	if got := req.URL.String(); got != "https://api.example.com/v1/chat/completions" {
		t.Fatalf("unexpected url %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Fatalf("unexpected auth header %q", got)
	}
	body, _ := io.ReadAll(req.Body)
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("bad payload: %v", err)
	}
	if payload["model"] != "gpt-4o" {
		t.Fatalf("expected rewritten model gpt-4o, got %v", payload["model"])
	}
}

func TestGPT56CacheUsageSeparatesReadsWritesAndOrdinaryInput(t *testing.T) {
	usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		[]byte(`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":60,"cache_write_tokens":30}}}`),
		EndpointChatCompletions,
		metering.UsageSourceUpstream,
		"cache-usage.openai-gpt-5.6.v1",
	)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 10, usage.UncachedPromptTokens)
	require.Equal(t, 60, usage.CacheReadTokens)
	require.Equal(t, 30, usage.CacheCreationTokens)
	require.Equal(t, CacheUsageSchemaOpenAIV2, usage.CacheUsageSchema)
	require.Equal(t, CacheUsageStatusReadWriteReported, usage.CacheUsageStatus)

	_, present, err = ParseOpenAICompatibleUsageWithCacheProfile(
		[]byte(`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":30}}}`),
		EndpointChatCompletions,
		metering.UsageSourceUpstream,
		"cache-usage.openai-gpt-5.6.v1",
	)
	require.True(t, present)
	require.Error(t, err)
}

func TestOpenAICompatibleEndpointPathModeIsTypedAndProviderIndependent(t *testing.T) {
	adapter := NewOpenAICompatible()
	require.NoError(t, adapter.ValidateConfig([]byte(`{"endpoint_path_mode":"base_relative"}`)))
	for _, raw := range []string{
		`{"endpoint_path_mode":"future"}`,
		`{"endpoint_path_mode":"base_relative","unknown":true}`,
		`{"endpoint_path_mode":"base_relative"}{}`,
	} {
		require.Error(t, adapter.ValidateConfig([]byte(raw)))
	}
	for _, test := range []struct {
		base, want string
	}{
		{base: "https://api.deepseek.com", want: "https://api.deepseek.com/chat/completions"},
		{base: "https://open.bigmodel.cn/api/paas/v4", want: "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{base: "https://generativelanguage.googleapis.com/v1beta/openai", want: "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
		{base: "https://ark.cn-beijing.volces.com/api/v3", want: "https://ark.cn-beijing.volces.com/api/v3/chat/completions"},
	} {
		req, err := adapter.BuildRequest(context.Background(), RequestInput{
			Endpoint: EndpointChatCompletions, DownstreamBody: []byte(`{"model":"m","messages":[]}`),
			RequestedModel: "m", MappedModel: "m", APIKey: "fixture",
			Channel: models.Channel{Provider: "enterprise-alias", BaseURL: test.base, AdapterType: TypeOpenAICompatible,
				AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)},
		})
		require.NoError(t, err)
		require.Equal(t, test.want, req.URL.String())
	}
}

func TestKimiPlatformCanonicalV1BuildsTheReviewedChatEndpoint(t *testing.T) {
	adapter := NewOpenAICompatible()
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions, DownstreamBody: []byte(`{"model":"public-kimi","messages":[]}`),
		RequestedModel: "public-kimi", MappedModel: "kimi-k3", APIKey: "fixture",
		Channel: models.Channel{
			Provider: "kimi", BaseURL: "https://api.moonshot.cn/v1", AdapterType: TypeOpenAICompatible,
			AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.moonshot.cn/v1/chat/completions", request.URL.String())
}

func TestMiniMaxCanonicalV1BuildsTheReviewedChatEndpoint(t *testing.T) {
	adapter := NewOpenAICompatible()
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions, DownstreamBody: []byte(`{"model":"public-minimax","messages":[]}`),
		RequestedModel: "public-minimax", MappedModel: "MiniMax-M3", APIKey: "fixture",
		Channel: models.Channel{
			Provider: "minimax", BaseURL: "https://api.minimaxi.com/v1", AdapterType: TypeOpenAICompatible,
			AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.minimaxi.com/v1/chat/completions", request.URL.String())
}

func TestXAICanonicalV1BuildsTheReviewedChatEndpoint(t *testing.T) {
	adapter := NewOpenAICompatible()
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions, DownstreamBody: []byte(`{"model":"public-grok","messages":[]}`),
		RequestedModel: "public-grok", MappedModel: "grok-4.6", APIKey: "fixture",
		Channel: models.Channel{
			Provider: "xai", BaseURL: "https://api.x.ai/v1", AdapterType: TypeOpenAICompatible,
			AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.x.ai/v1/chat/completions", request.URL.String())
}

func TestOpenAICompatibleBuildRequest_PreservesOpenAIAndProviderFields(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]},{"role":"assistant","content":null,"reasoning_content":"checked","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto","parallel_tool_calls":true,"response_format":{"type":"json_object"},"stream_options":{"include_usage":true},"reasoning":{"effort":"high"},"reasoning_effort":"high","thinking":{"type":"enabled"},"provider_counter":9223372036854775807}`)
	req, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointChatCompletions,
		DownstreamBody: body,
		RequestedModel: "public-model",
		MappedModel:    "upstream-model",
		Channel:        models.Channel{BaseURL: "https://api.example.com/v1"},
		APIKey:         "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	upstreamBody, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(upstreamBody, &payload); err != nil {
		t.Fatal(err)
	}
	var model string
	if err := json.Unmarshal(payload["model"], &model); err != nil || model != "upstream-model" {
		t.Fatalf("model rewrite failed: model=%q err=%v body=%s", model, err, upstreamBody)
	}
	for _, field := range []string{"messages", "tools", "tool_choice", "parallel_tool_calls", "response_format", "stream_options", "reasoning", "reasoning_effort", "thinking", "provider_counter"} {
		if len(payload[field]) == 0 {
			t.Fatalf("field %q was dropped: %s", field, upstreamBody)
		}
	}
	if string(payload["provider_counter"]) != "9223372036854775807" {
		t.Fatalf("provider extension number lost precision: %s", payload["provider_counter"])
	}
}

func TestOpenAICompatibleParseResponse_Usage(t *testing.T) {
	adapter := NewOpenAICompatible()
	resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint:       EndpointChatCompletions,
		StatusCode:     http.StatusOK,
		Body:           []byte(`{"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`),
		Channel:        models.Channel{ID: 9, Name: "test", Provider: "openai", AdapterType: TypeOpenAICompatible},
		RequestedModel: "company-chat",
		UpstreamModel:  "gpt-4o",
	})
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if resp.Usage.TotalTokens != 7 || resp.Usage.PromptTokens != 3 || resp.Usage.CompletionTokens != 4 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if resp.Usage.UncachedPromptTokens != 3 || resp.Usage.CacheReadTokens != 0 || resp.Usage.CacheUsageStatus != CacheUsageStatusNotReported {
		t.Fatalf("unexpected uncached usage: %+v", resp.Usage)
	}
	if resp.Meta.RequestedModel != "company-chat" || resp.Meta.UpstreamModel != "gpt-4o" {
		t.Fatalf("unexpected meta: %+v", resp.Meta)
	}
}

func TestGLM53BaseNonStreamChatPreservesOfficialEnvelopeAndUsage(t *testing.T) {
	adapter := NewOpenAICompatible()
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointChatCompletions,
		DownstreamBody: []byte(`{"model":"public-glm","messages":[{"role":"user","content":"Hello"}],"stream":false}`),
		RequestedModel: "public-glm",
		MappedModel:    "glm-5.3",
		Channel:        models.Channel{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Provider: "renamed-glm-proxy", AdapterType: TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)},
		APIKey:         "fixture-key",
	})
	require.NoError(t, err)
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/chat/completions", request.URL.String())
	requestBody, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"glm-5.3","messages":[{"role":"user","content":"Hello"}],"stream":false}`, string(requestBody))

	body := []byte(`{"id":"glm-fixture","request_id":"req-fixture","created":1787568000,"model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	response, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel:        models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		RequestedModel: "public-glm", UpstreamModel: "glm-5.3",
		ProtocolProfile: "protocol.glm-openai-chat.v1",
	})
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(response.Body))
	require.Equal(t, 3, response.Usage.PromptTokens)
	require.Equal(t, 4, response.Usage.CompletionTokens)
	require.Equal(t, 7, response.Usage.TotalTokens)
	require.Equal(t, metering.UsageAuthorityProviderReported, response.Usage.Authority)
	require.Equal(t, metering.UsageFinalityFinal, response.Usage.Finality)
}

func TestGLM52BaseNonStreamChatPreservesOfficialEnvelopeAndUsage(t *testing.T) {
	adapter := NewOpenAICompatible()
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointChatCompletions,
		DownstreamBody: []byte(`{"model":"public-glm","messages":[{"role":"user","content":"Hello"}],"stream":false}`),
		RequestedModel: "public-glm",
		MappedModel:    "glm-5.2",
		Channel:        models.Channel{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Provider: "renamed-glm-proxy", AdapterType: TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)},
		APIKey:         "fixture-key",
	})
	require.NoError(t, err)
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/chat/completions", request.URL.String())
	requestBody, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"glm-5.2","messages":[{"role":"user","content":"Hello"}],"stream":false}`, string(requestBody))

	body := []byte(`{"id":"glm52-fixture","model":"glm-5.2","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	response, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel:        models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		RequestedModel: "public-glm", UpstreamModel: "glm-5.2",
		ProtocolProfile: "protocol.glm-openai-chat.v1",
	})
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(response.Body))
	require.Equal(t, 7, response.Usage.TotalTokens)
	require.Equal(t, metering.UsageAuthorityProviderReported, response.Usage.Authority)
	require.Equal(t, metering.UsageFinalityFinal, response.Usage.Finality)
}

func TestGLM5VTurboBaseTextNonStreamChatPreservesOfficialEnvelopeAndUsage(t *testing.T) {
	adapter := NewOpenAICompatible()
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointChatCompletions,
		DownstreamBody: []byte(`{"model":"public-glm-vision","messages":[{"role":"user","content":"Hello"}],"stream":false}`),
		RequestedModel: "public-glm-vision",
		MappedModel:    "glm-5v-turbo",
		Channel:        models.Channel{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Provider: "renamed-glm-proxy", AdapterType: TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)},
		APIKey:         "fixture-key",
	})
	require.NoError(t, err)
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/chat/completions", request.URL.String())
	requestBody, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"glm-5v-turbo","messages":[{"role":"user","content":"Hello"}],"stream":false}`, string(requestBody))

	body := []byte(`{"id":"glm5v-fixture","model":"glm-5v-turbo","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	response, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel:        models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		RequestedModel: "public-glm-vision", UpstreamModel: "glm-5v-turbo",
		ProtocolProfile: "protocol.glm-openai-chat.v1",
	})
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(response.Body))
	require.Equal(t, 3, response.Usage.PromptTokens)
	require.Equal(t, 4, response.Usage.CompletionTokens)
	require.Equal(t, 7, response.Usage.TotalTokens)
	require.Equal(t, metering.UsageAuthorityProviderReported, response.Usage.Authority)
	require.Equal(t, metering.UsageFinalityFinal, response.Usage.Finality)
}

func TestGLM5VTurboStreamingChatPreservesOfficialChunksAndFinalUsage(t *testing.T) {
	adapter := NewOpenAICompatible()
	channel := models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible}
	frames := [][]byte{
		[]byte(`{"id":"glm5v-stream","model":"glm-5v-turbo","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`),
		[]byte(`{"id":"glm5v-stream","model":"glm-5v-turbo","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`),
		[]byte(`{"id":"glm5v-stream","model":"glm-5v-turbo","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`),
	}
	for _, frame := range frames {
		got, err := adapter.NormalizeStreamFrame(context.Background(), StreamChunkInput{Data: frame, Channel: channel})
		require.NoError(t, err)
		require.JSONEq(t, string(frame), string(got))
	}

	usage, present, err := ParseOpenAICompatibleUsage(frames[2], EndpointChatCompletions, metering.UsageSourceUpstream)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, metering.UsageAuthorityProviderReported, usage.Authority)
	require.Equal(t, metering.UsageFinalityFinal, usage.Finality)
}

func TestGLM5VTurboReasoningRequestAndResponsePreserveOfficialControls(t *testing.T) {
	adapter := NewOpenAICompatible()
	requestBody := []byte(`{"model":"public-glm-vision","messages":[{"role":"user","content":"Solve"}],"thinking":{"type":"enabled","clear_thinking":false},"reasoning_effort":"high","max_tokens":4096}`)
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions, DownstreamBody: requestBody,
		RequestedModel: "public-glm-vision", MappedModel: "glm-5v-turbo",
		Channel: models.Channel{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Provider: "glm", AdapterType: TypeOpenAICompatible},
		APIKey:  "fixture-key",
	})
	require.NoError(t, err)
	wire, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"glm-5v-turbo","messages":[{"role":"user","content":"Solve"}],"thinking":{"type":"enabled","clear_thinking":false},"reasoning_effort":"high","max_tokens":4096}`, string(wire))

	responseBody := []byte(`{"id":"glm5v-reasoning","model":"glm-5v-turbo","choices":[{"index":0,"message":{"role":"assistant","reasoning_content":"Think","content":"Answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":6,"total_tokens":9}}`)
	response, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: responseBody,
		Channel:        models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		RequestedModel: "public-glm-vision", UpstreamModel: "glm-5v-turbo",
		ProtocolProfile: "protocol.glm-openai-chat.v3",
	})
	require.NoError(t, err)
	require.JSONEq(t, string(responseBody), string(response.Body))
	require.Equal(t, 9, response.Usage.TotalTokens)
}

func TestGLM5VTurboFunctionToolResponsePreservesOfficialToolCallAndUsage(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"id":"glm5v-tool","model":"glm-5v-turbo","choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":"Need inspection","tool_calls":[{"id":"call_1","type":"function","function":{"name":"inspect_layout","arguments":"{\"selector\":\"#app\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}}`)
	response, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel:        models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		RequestedModel: "public-glm-vision", UpstreamModel: "glm-5v-turbo",
		ProtocolProfile: "protocol.glm-openai-chat.v4",
	})
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(response.Body))
	require.Equal(t, 20, response.Usage.PromptTokens)
	require.Equal(t, 8, response.Usage.CompletionTokens)
	require.Equal(t, 28, response.Usage.TotalTokens)
}

func TestGLM5VTurboNonStreamCacheReadUsageUsesExactOfficialWire(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"id":"glm5v-cache","model":"glm-5v-turbo","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80}}}`)
	response, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel:        models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		RequestedModel: "public-glm-vision", UpstreamModel: "glm-5v-turbo",
		CacheUsageProfile: "cache-usage.glm-openai.v1",
		ProtocolProfile:   "protocol.glm-openai-chat.glm5v.v1",
	})
	require.NoError(t, err)
	require.Equal(t, 100, response.Usage.PromptTokens)
	require.Equal(t, 20, response.Usage.UncachedPromptTokens)
	require.Equal(t, 80, response.Usage.CacheReadTokens)
	require.Zero(t, response.Usage.CacheCreationTokens)
	require.Equal(t, 7, response.Usage.CompletionTokens)
	require.Equal(t, CacheUsageSchemaOpenAIV1, response.Usage.CacheUsageSchema)
	require.Equal(t, CacheUsageStatusReadReported, response.Usage.CacheUsageStatus)
	require.Equal(t, metering.UsageAuthorityProviderReported, response.Usage.Authority)
	require.Equal(t, metering.UsageFinalityFinal, response.Usage.Finality)
}

func TestDoubaoSeedEvolvingCacheReadUsageRequiresExactNestedCounter(t *testing.T) {
	profile := "cache-usage.doubao-ark-chat.v1"
	tests := []struct {
		name     string
		usage    string
		wantRead int
		wantErr  string
	}{
		{
			name:     "cache hit",
			usage:    `{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80}}`,
			wantRead: 80,
		},
		{
			name:  "authoritative zero",
			usage: `{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":0}}`,
		},
		{
			name:    "missing prompt details",
			usage:   `{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107}`,
			wantErr: "including an authoritative zero",
		},
		{
			name:    "null prompt details",
			usage:   `{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":null}`,
			wantErr: "including an authoritative zero",
		},
		{
			name:    "null counter",
			usage:   `{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":null}}`,
			wantErr: "including an authoritative zero",
		},
		{
			name:    "counter exceeds prompt",
			usage:   `{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":101}}`,
			wantErr: "uncached_prompt_tokens must be nonnegative",
		},
		{
			name:    "conflicting top-level schema",
			usage:   `{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"cached_tokens":80,"prompt_tokens_details":{"cached_tokens":80}}`,
			wantErr: "without another cache schema",
		},
		{
			name:    "unmodeled cache write",
			usage:   `{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":10}}`,
			wantErr: "without another cache schema",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"choices":[],"usage":` + test.usage + `}`)
			usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
				body, EndpointChatCompletions, metering.UsageSourceUpstream, profile,
			)
			if test.wantErr != "" {
				require.True(t, present)
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.True(t, present)
			require.Equal(t, 100, usage.PromptTokens)
			require.Equal(t, test.wantRead, usage.CacheReadTokens)
			require.Equal(t, 100-test.wantRead, usage.UncachedPromptTokens)
			require.Equal(t, CacheUsageSchemaOpenAIV1, usage.CacheUsageSchema)
			require.Equal(t, CacheUsageStatusReadReported, usage.CacheUsageStatus)
		})
	}

	legacy, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		[]byte(`{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`),
		EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.glm-openai.v1",
	)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, CacheUsageStatusNotReported, legacy.CacheUsageStatus)

	adapter := NewOpenAICompatible()
	for _, body := range []string{
		`{"choices":[]}`,
		`{"choices":[],"usage":null}`,
		`{"choices":[],"usage":{"completion_tokens":1,"total_tokens":1,"prompt_tokens_details":{"cached_tokens":0}}}`,
		`{"choices":[],"usage":{"prompt_tokens":1,"total_tokens":1,"prompt_tokens_details":{"cached_tokens":0}}}`,
	} {
		_, err := adapter.ParseResponse(context.Background(), ResponseInput{
			Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: []byte(body),
			Channel:           models.Channel{Provider: "doubao", AdapterType: TypeOpenAICompatible},
			CacheUsageProfile: profile,
		})
		require.ErrorContains(t, err, "requires final response usage")
	}
}

func TestGLM5VTurboImageRequestRemainsProviderNative(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"model":"public-glm-vision","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://cdn.example.test/layout.png"}},{"type":"text","text":"Describe"}]}]}`)
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions, DownstreamBody: body,
		RequestedModel: "public-glm-vision", MappedModel: "glm-5v-turbo",
		Channel: models.Channel{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Provider: "glm", AdapterType: TypeOpenAICompatible},
		APIKey:  "fixture-key",
	})
	require.NoError(t, err)
	wire, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://cdn.example.test/layout.png"}},{"type":"text","text":"Describe"}]}]}`, string(wire))
}

func TestGLM5VTurboVideoRequestRemainsProviderNative(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"model":"public-glm-vision","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://cdn.example.test/demo.mov"}},{"type":"text","text":"Track"}]}]}`)
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions, DownstreamBody: body,
		RequestedModel: "public-glm-vision", MappedModel: "glm-5v-turbo",
		Channel: models.Channel{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Provider: "glm", AdapterType: TypeOpenAICompatible},
		APIKey:  "fixture-key",
	})
	require.NoError(t, err)
	wire, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://cdn.example.test/demo.mov"}},{"type":"text","text":"Track"}]}]}`, string(wire))
}

func TestGLM5VTurboFileRequestRemainsProviderNative(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"model":"public-glm-vision","messages":[{"role":"user","content":[{"type":"file_url","file_url":{"url":"https://cdn.example.test/report.pdf"}},{"type":"text","text":"Summarize"}]}]}`)
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions, DownstreamBody: body,
		RequestedModel: "public-glm-vision", MappedModel: "glm-5v-turbo",
		Channel: models.Channel{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Provider: "glm", AdapterType: TypeOpenAICompatible},
		APIKey:  "fixture-key",
	})
	require.NoError(t, err)
	wire, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"glm-5v-turbo","messages":[{"role":"user","content":[{"type":"file_url","file_url":{"url":"https://cdn.example.test/report.pdf"}},{"type":"text","text":"Summarize"}]}]}`, string(wire))
}

func TestGLM52StreamingChatPreservesOfficialChunksAndFinalUsage(t *testing.T) {
	adapter := NewOpenAICompatible()
	frames := [][]byte{
		[]byte(`{"id":"glm52-stream","model":"glm-5.2","choices":[{"index":0,"delta":{"reasoning_content":"Think"},"finish_reason":null}]}`),
		[]byte(`{"id":"glm52-stream","model":"glm-5.2","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`),
		[]byte(`{"id":"glm52-stream","model":"glm-5.2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`),
	}
	for _, frame := range frames {
		got, err := adapter.NormalizeStreamFrame(context.Background(), StreamChunkInput{
			Data: frame, Channel: models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		})
		require.NoError(t, err)
		require.JSONEq(t, string(frame), string(got))
	}
	usage, present, err := ParseOpenAICompatibleUsage(frames[2], EndpointChatCompletions, metering.UsageSourceUpstream)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 7, usage.TotalTokens)
}

func TestGLM53StreamingChatPreservesOfficialChunksAndFinalUsage(t *testing.T) {
	adapter := NewOpenAICompatible()
	channel := models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible}
	frames := [][]byte{
		[]byte(`{"id":"glm-stream","created":1787568000,"model":"glm-5.3","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`),
		[]byte(`{"id":"glm-stream","created":1787568000,"model":"glm-5.3","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`),
		[]byte(`{"id":"glm-stream","created":1787568000,"model":"glm-5.3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`),
	}
	for _, frame := range frames {
		got, err := adapter.NormalizeStreamFrame(context.Background(), StreamChunkInput{Data: frame, Channel: channel})
		require.NoError(t, err)
		require.JSONEq(t, string(frame), string(got))
	}

	usage, present, err := ParseOpenAICompatibleUsage(frames[2], EndpointChatCompletions, metering.UsageSourceUpstream)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, metering.UsageAuthorityProviderReported, usage.Authority)
	require.Equal(t, metering.UsageFinalityFinal, usage.Finality)
}

func TestGLM53ReasoningRequestPreservesOfficialControls(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"model":"public-glm","messages":[{"role":"user","content":"Solve"}],"thinking":{"type":"enabled","clear_thinking":false},"reasoning_effort":"high","max_tokens":4096}`)
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions, DownstreamBody: body,
		RequestedModel: "public-glm", MappedModel: "glm-5.3",
		Channel: models.Channel{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Provider: "glm", AdapterType: TypeOpenAICompatible},
		APIKey:  "fixture-key",
	})
	require.NoError(t, err)
	wire, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"glm-5.3","messages":[{"role":"user","content":"Solve"}],"thinking":{"type":"enabled","clear_thinking":false},"reasoning_effort":"high","max_tokens":4096}`, string(wire))
}

func TestGLM53FunctionToolResponsePreservesOfficialToolCallAndUsage(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"id":"glm-tool","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Shanghai\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}}`)
	response, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel:        models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		RequestedModel: "public-glm", UpstreamModel: "glm-5.3",
		ProtocolProfile: "protocol.glm-openai-chat.v4",
	})
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(response.Body))
	require.Equal(t, 20, response.Usage.PromptTokens)
	require.Equal(t, 8, response.Usage.CompletionTokens)
	require.Equal(t, 28, response.Usage.TotalTokens)
}

func TestGLM53JSONObjectRequestAndResponseRemainUnmodified(t *testing.T) {
	adapter := NewOpenAICompatible()
	requestBody := []byte(`{"model":"public-glm","messages":[{"role":"user","content":"Return JSON"}],"response_format":{"type":"json_object"}}`)
	request, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions, DownstreamBody: requestBody,
		RequestedModel: "public-glm", MappedModel: "glm-5.3",
		Channel: models.Channel{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Provider: "glm", AdapterType: TypeOpenAICompatible},
		APIKey:  "fixture-key",
	})
	require.NoError(t, err)
	wire, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"glm-5.3","messages":[{"role":"user","content":"Return JSON"}],"response_format":{"type":"json_object"}}`, string(wire))

	responseBody := []byte(`{"id":"glm-json","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"{\"ok\":true}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":4,"total_tokens":9}}`)
	response, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: responseBody,
		Channel:        models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		RequestedModel: "public-glm", UpstreamModel: "glm-5.3",
		ProtocolProfile: "protocol.glm-openai-chat.v5",
	})
	require.NoError(t, err)
	require.JSONEq(t, string(responseBody), string(response.Body))
	require.Equal(t, 9, response.Usage.TotalTokens)
}

func TestGLM53NonStreamCacheReadUsageUsesExactOfficialWire(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"id":"glm-cache","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80}}}`)
	response, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel:        models.Channel{Provider: "glm", AdapterType: TypeOpenAICompatible},
		RequestedModel: "public-glm", UpstreamModel: "glm-5.3",
		CacheUsageProfile: "cache-usage.glm-openai.v1",
		ProtocolProfile:   "protocol.glm-openai-chat.v5",
	})
	require.NoError(t, err)
	require.Equal(t, 100, response.Usage.PromptTokens)
	require.Equal(t, 20, response.Usage.UncachedPromptTokens)
	require.Equal(t, 80, response.Usage.CacheReadTokens)
	require.Zero(t, response.Usage.CacheCreationTokens)
	require.Equal(t, 7, response.Usage.CompletionTokens)
	require.Equal(t, CacheUsageSchemaOpenAIV1, response.Usage.CacheUsageSchema)
	require.Equal(t, CacheUsageStatusReadReported, response.Usage.CacheUsageStatus)
	require.Equal(t, metering.UsageAuthorityProviderReported, response.Usage.Authority)
	require.Equal(t, metering.UsageFinalityFinal, response.Usage.Finality)

	for _, invalid := range []string{
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"cached_tokens":80}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":10}}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":101}}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`,
	} {
		_, _, err := ParseOpenAICompatibleUsageWithCacheProfile(
			[]byte(invalid), EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.glm-openai.v1",
		)
		require.Error(t, err, invalid)
	}

	withoutCache := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
	usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		withoutCache, EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.glm-openai.v1",
	)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 10, usage.UncachedPromptTokens)
	require.Zero(t, usage.CacheReadTokens)
	require.Equal(t, CacheUsageStatusNotReported, usage.CacheUsageStatus)
}

func TestGLM52NonStreamCacheReadUsageUsesExactOfficialWire(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":50,"completion_tokens":5,"total_tokens":55,"prompt_tokens_details":{"cached_tokens":30}}}`)
	usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		body, EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.glm-openai.v1",
	)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 20, usage.UncachedPromptTokens)
	require.Equal(t, 30, usage.CacheReadTokens)
	require.Zero(t, usage.CacheCreationTokens)
	require.Equal(t, CacheUsageSchemaOpenAIV1, usage.CacheUsageSchema)
}

func TestMiMoCacheReadUsageUsesSealedOpenAIWireProfile(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80,"audio_tokens":3,"image_tokens":4,"video_tokens":5}}}`)
	usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		body, EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.mimo-openai.v1",
	)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 20, usage.UncachedPromptTokens)
	require.Equal(t, 80, usage.CacheReadTokens)
	require.Zero(t, usage.CacheCreationTokens)
	require.Equal(t, CacheUsageSchemaOpenAIV1, usage.CacheUsageSchema)
	require.Equal(t, CacheUsageStatusReadReported, usage.CacheUsageStatus)
}

func TestHy3CacheReadUsageUsesSealedOpenAIWireProfile(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":64},"completion_tokens_details":{"reasoning_tokens":3}}}`)
	usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		body, EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.hy3-openai.v1",
	)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 36, usage.UncachedPromptTokens)
	require.Equal(t, 64, usage.CacheReadTokens)
	require.Zero(t, usage.CacheCreationTokens)
	require.Equal(t, CacheUsageSchemaOpenAIV1, usage.CacheUsageSchema)
	require.Equal(t, CacheUsageStatusReadReported, usage.CacheUsageStatus)
}

func TestQwenContextCacheUsageUsesExactOfficialWire(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":60,"cache_creation_input_tokens":30}}}`)
	usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		body, EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.qwen-context-cache.v1",
	)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 10, usage.UncachedPromptTokens)
	require.Equal(t, 60, usage.CacheReadTokens)
	require.Equal(t, 30, usage.CacheCreationTokens)
	require.Equal(t, CacheUsageSchemaQwenV1, usage.CacheUsageSchema)
	require.Equal(t, CacheUsageStatusReadWriteReported, usage.CacheUsageStatus)

	for _, invalid := range []string{
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"cached_tokens":60}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_cache_hit_tokens":60,"prompt_cache_miss_tokens":40}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":80,"cache_creation_input_tokens":30}}}`,
		`{"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":-1}}}`,
	} {
		_, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
			[]byte(invalid), EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.qwen-context-cache.v1",
		)
		require.True(t, present, invalid)
		require.Error(t, err, invalid)
	}
}

func TestOpenAICompatibleParseResponse_MetersKimiBuiltinSearchFromTypedProfile(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"type":"function","function":{"name":"$web_search","arguments":"{}"}}]}}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	for _, profile := range []string{
		"protocol.kimi-openai-chat.v2",
		"protocol.kimi-openai-chat.v3",
		"protocol.kimi-openai-chat.k3.v2",
	} {
		response, err := adapter.ParseResponse(context.Background(), ResponseInput{
			Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
			ProtocolProfile: profile,
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []metering.MeterQuantity{{Meter: "builtin_web_search_calls", Quantity: 1}}
		if len(response.Usage.MeterUsage) != 1 || response.Usage.MeterUsage[0] != want[0] {
			t.Fatalf("profile %s meter usage = %+v, want %+v", profile, response.Usage.MeterUsage, want)
		}
	}

	withoutProfile, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
	})
	if err != nil || len(withoutProfile.Usage.MeterUsage) != 0 {
		t.Fatalf("untyped profile must not infer billable usage: usage=%+v err=%v", withoutProfile.Usage.MeterUsage, err)
	}
}

func TestOpenAICompatibleParseResponse_RejectsBuiltinSearchAboveFrozenMaximum(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"type":"function","function":{"name":"$web_search"}},{"type":"function","function":{"name":"$web_search"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	_, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		ProtocolProfile: "protocol.kimi-openai-chat.v2",
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds profile maximum") {
		t.Fatalf("error = %v, want frozen maximum rejection", err)
	}
}

func TestOpenAICompatibleParseResponse_NormalizesOnlySafeMiniMaxControlFields(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"id":"chatcmpl-minimax","object":"chat.completion","created":1780152150,"model":"MiniMax-M3","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello","name":"MiniMax AI","audio_content":""}}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7,"total_characters":0},"input_sensitive":false,"output_sensitive":false,"input_sensitive_type":0,"output_sensitive_type":0,"output_sensitive_int":0,"base_resp":{"status_code":0,"status_msg":"success"}}`)
	resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint:   EndpointChatCompletions,
		StatusCode: http.StatusOK,
		Body:       body,
		Channel:    models.Channel{Provider: "MiniMax", AdapterType: TypeOpenAICompatible},
	})
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body, &normalized); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"input_sensitive", "output_sensitive", "input_sensitive_type", "output_sensitive_type", "output_sensitive_int", "base_resp"} {
		if _, exists := normalized[field]; exists {
			t.Fatalf("safe MiniMax control field %q was not removed: %s", field, resp.Body)
		}
	}
	var choices []struct {
		Message map[string]json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(normalized["choices"], &choices); err != nil || len(choices) != 1 {
		t.Fatalf("decode normalized choices: choices=%v err=%v", choices, err)
	}
	for _, field := range []string{"name", "audio_content"} {
		if _, exists := choices[0].Message[field]; exists {
			t.Fatalf("safe MiniMax message control field %q was not removed: %s", field, resp.Body)
		}
	}
	if got := string(choices[0].Message["content"]); got != `"hello"` {
		t.Fatalf("model content changed during normalization: %s", got)
	}
	if resp.Usage.PromptTokens != 3 || resp.Usage.CompletionTokens != 4 || resp.Usage.TotalTokens != 7 {
		t.Fatalf("usage changed during normalization: %+v", resp.Usage)
	}
}

func TestOpenAICompatibleParseResponse_LeavesUnsafeOrUnknownMiniMaxFieldsForFailClosedDelivery(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"hidden","name":"unexpected","audio_content":"audio"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2},"input_sensitive":true,"output_sensitive":true,"input_sensitive_type":1,"output_sensitive_type":2,"output_sensitive_int":3,"base_resp":{"status_code":1001,"status_msg":"blocked"},"unknown_provider_field":"opaque"}`)
	resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint:   EndpointChatCompletions,
		StatusCode: http.StatusOK,
		Body:       body,
		Channel:    models.Channel{Provider: "minimax", AdapterType: TypeOpenAICompatible},
	})
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body, &normalized); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"input_sensitive", "output_sensitive", "input_sensitive_type", "output_sensitive_type", "output_sensitive_int", "base_resp", "unknown_provider_field"} {
		if _, exists := normalized[field]; !exists {
			t.Fatalf("unsafe or unknown field %q was silently removed: %s", field, resp.Body)
		}
	}
}

func TestOpenAICompatibleNormalizeStreamFrame_NormalizesSafeMiniMaxEnvelopeOnly(t *testing.T) {
	adapter := NewOpenAICompatible()
	input := []byte(`{"id":"chunk-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hello","name":"MiniMax AI","audio_content":""}}],"input_sensitive":false,"output_sensitive":false,"base_resp":{"status_code":0,"status_msg":""}}`)
	got, err := adapter.NormalizeStreamFrame(context.Background(), StreamChunkInput{
		Data: input, Channel: models.Channel{Provider: "minimax"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]json.RawMessage
	if err := json.Unmarshal(got, &normalized); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"input_sensitive", "output_sensitive", "base_resp"} {
		if _, exists := normalized[field]; exists {
			t.Fatalf("safe stream control field %q was not removed: %s", field, got)
		}
	}
	if _, exists := normalized["choices"]; !exists {
		t.Fatalf("stream choices were lost: %s", got)
	}
	var choices []struct {
		Delta map[string]json.RawMessage `json:"delta"`
	}
	if err := json.Unmarshal(normalized["choices"], &choices); err != nil || len(choices) != 1 {
		t.Fatalf("decode normalized stream choices: choices=%v err=%v", choices, err)
	}
	for _, field := range []string{"name", "audio_content"} {
		if _, exists := choices[0].Delta[field]; exists {
			t.Fatalf("safe MiniMax stream delta field %q was not removed: %s", field, got)
		}
	}
	if gotContent := string(choices[0].Delta["content"]); gotContent != `"hello"` {
		t.Fatalf("stream content changed during normalization: %s", gotContent)
	}

	unsafe := []byte(`{"choices":[{"delta":{"content":"hidden","name":"unexpected","audio_content":"audio"}}]}`)
	unsafeGot, err := adapter.NormalizeStreamFrame(context.Background(), StreamChunkInput{
		Data: unsafe, Channel: models.Channel{Provider: "minimax"},
	})
	if err != nil || string(unsafeGot) != string(unsafe) {
		t.Fatalf("unsafe MiniMax stream metadata was removed: got=%s err=%v", unsafeGot, err)
	}
	unchanged, err := adapter.NormalizeStreamFrame(context.Background(), StreamChunkInput{
		Data: input, Channel: models.Channel{Provider: "openai"},
	})
	if err != nil || string(unchanged) != string(input) {
		t.Fatalf("non-MiniMax stream frame changed: got=%s err=%v", unchanged, err)
	}
}

func TestP6F41OpenAICompatibleExplicitZeroAndMissingUsageRemainDistinct(t *testing.T) {
	explicitZero, present, err := ParseOpenAICompatibleUsage(
		[]byte(`{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`),
		EndpointChatCompletions,
		metering.UsageSourceUpstream,
	)
	if err != nil || !present {
		t.Fatalf("explicit zero must remain present: present=%t err=%v", present, err)
	}
	if explicitZero.IsMissing() || explicitZero.Authority != metering.UsageAuthorityProviderReported || explicitZero.Finality != metering.UsageFinalityFinal {
		t.Fatalf("explicit zero lost provider authority: %+v", explicitZero)
	}
	if _, err := explicitZero.ForCustomerCharge(); err != nil {
		t.Fatalf("explicit provider zero must be eligible for the customer-charge contract: %v", err)
	}

	missing, present, err := ParseOpenAICompatibleUsage([]byte(`{"data":[]}`), EndpointChatCompletions, metering.UsageSourceUpstream)
	if err != nil || present {
		t.Fatalf("absent usage must remain absent: present=%t err=%v", present, err)
	}
	if !missing.IsMissing() || missing.Authority != metering.UsageAuthorityNone || missing.Finality != metering.UsageFinalityMissing {
		t.Fatalf("absent usage became an authoritative zero: %+v", missing)
	}
	if _, err := missing.ForCustomerCharge(); err == nil {
		t.Fatal("missing usage must not be relabelled as a customer charge")
	}
}

func TestOpenAICompatibleIncompleteCoreUsageIsNotAuthoritative(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty usage object", body: `{"usage":{}}`},
		{name: "total only", body: `{"usage":{"total_tokens":12}}`},
		{name: "prompt only", body: `{"usage":{"prompt_tokens":7}}`},
		{name: "completion only", body: `{"usage":{"completion_tokens":5}}`},
		{name: "null prompt", body: `{"usage":{"prompt_tokens":null,"completion_tokens":5}}`},
		{name: "null completion", body: `{"usage":{"prompt_tokens":7,"completion_tokens":null}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage, present, err := ParseOpenAICompatibleUsage([]byte(tt.body), EndpointChatCompletions, metering.UsageSourceUpstream)
			if err != nil {
				t.Fatalf("incomplete usage should allow the caller fallback: %v", err)
			}
			if present || !usage.IsMissing() {
				t.Fatalf("incomplete usage became provider authority: present=%t usage=%+v", present, usage)
			}
		})
	}
}

func TestOpenAICompatibleUsageCompletenessIsEndpointAware(t *testing.T) {
	chat, present, err := ParseOpenAICompatibleUsage(
		[]byte(`{"usage":{"prompt_tokens":7,"completion_tokens":5}}`),
		EndpointChatCompletions,
		metering.UsageSourceUpstream,
	)
	if err != nil || !present || chat.TotalTokens != 12 {
		t.Fatalf("complete Chat usage without total must remain authoritative and derive total: present=%t usage=%+v err=%v", present, chat, err)
	}

	embeddings, present, err := ParseOpenAICompatibleUsage(
		[]byte(`{"usage":{"prompt_tokens":4,"total_tokens":4}}`),
		EndpointEmbeddings,
		metering.UsageSourceUpstream,
	)
	if err != nil || !present || embeddings.PromptTokens != 4 || embeddings.CompletionTokens != 0 || embeddings.TotalTokens != 4 {
		t.Fatalf("Embeddings structural completion zero was not preserved: present=%t usage=%+v err=%v", present, embeddings, err)
	}

	missing, present, err := ParseOpenAICompatibleUsage(
		[]byte(`{"usage":{"total_tokens":4}}`),
		EndpointEmbeddings,
		metering.UsageSourceUpstream,
	)
	if err != nil || present || !missing.IsMissing() {
		t.Fatalf("Embeddings usage without prompt_tokens became authoritative: present=%t usage=%+v err=%v", present, missing, err)
	}
}

func TestOpenAICompatibleInvalidPresentCounterStillFails(t *testing.T) {
	_, present, err := ParseOpenAICompatibleUsage(
		[]byte(`{"usage":{"prompt_tokens":-1}}`),
		EndpointChatCompletions,
		metering.UsageSourceUpstream,
	)
	if err == nil || present {
		t.Fatalf("invalid present counter must fail before fallback: present=%t err=%v", present, err)
	}
}

func TestOpenAICompatibleParseResponse_CacheUsageFixtures(t *testing.T) {
	adapter := NewOpenAICompatible()
	tests := []struct {
		name         string
		body         string
		prompt       int
		uncached     int
		read         int
		creation     int
		schema       string
		status       string
		cacheProfile string
		wantErr      bool
	}{
		{
			name:   "unmodeled OpenAI cached-token subset is not billing truth",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80}}}`,
			prompt: 100, uncached: 100, status: CacheUsageStatusNotReported,
		},
		{
			name:   "unmodeled OpenAI read-write partition is not billing truth",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":60,"cache_write_tokens":30}}}`,
			prompt: 100, uncached: 100, status: CacheUsageStatusNotReported,
		},
		{
			name:   "unmodeled OpenAI cache-write-only field is not billing truth",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cache_write_tokens":80}}}`,
			prompt: 100, uncached: 100, status: CacheUsageStatusNotReported,
		},
		{
			name:   "DeepSeek hit-miss partition",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`,
			prompt: 100, uncached: 20, read: 80,
			schema: CacheUsageSchemaDeepSeekV1, status: CacheUsageStatusHitMissReported,
			cacheProfile: "cache-usage.deepseek-v4.v1",
		},
		{
			name:   "DeepSeek official redundant cached-token mirror",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80},"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`,
			prompt: 100, uncached: 20, read: 80,
			schema: CacheUsageSchemaDeepSeekV1, status: CacheUsageStatusHitMissReported,
			cacheProfile: "cache-usage.deepseek-v4.v1",
		},
		{
			name:   "DeepSeek official zero cached-token mirror",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":0},"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":100}}`,
			prompt: 100, uncached: 100, read: 0,
			schema: CacheUsageSchemaDeepSeekV1, status: CacheUsageStatusHitMissReported,
			cacheProfile: "cache-usage.deepseek-v4.v1",
		},
		{
			name:   "unmodeled DeepSeek counters are not billing truth",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`,
			prompt: 100, uncached: 100, read: 0,
			status: CacheUsageStatusNotReported,
		},
		{
			name:   "unmodeled zero hit is not authoritative",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":0}}}`,
			prompt: 100, uncached: 100, read: 0,
			status: CacheUsageStatusNotReported,
		},
		{
			name:   "unknown cache-like field is not billing truth",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"cache_hit_tokens":99}}`,
			prompt: 100, uncached: 100, read: 0,
			status: CacheUsageStatusNotReported,
		},
		{
			name:   "unmodeled invalid cached subset is ignored",
			body:   `{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":11}}}`,
			prompt: 10, uncached: 10, status: CacheUsageStatusNotReported,
		},
		{
			name:   "unmodeled invalid read-write partition is ignored",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":21}}}`,
			prompt: 100, uncached: 100, status: CacheUsageStatusNotReported,
		},
		{
			name:         "DeepSeek partition must match prompt total",
			body:         `{"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":19}}`,
			cacheProfile: "cache-usage.deepseek-v4.v1",
			wantErr:      true,
		},
		{
			name:         "DeepSeek partial hit-only contract is rejected",
			body:         `{"usage":{"prompt_tokens":80,"completion_tokens":1,"total_tokens":81,"prompt_cache_hit_tokens":80}}`,
			cacheProfile: "cache-usage.deepseek-v4.v1",
			wantErr:      true,
		},
		{
			name:         "DeepSeek partial miss-only contract is rejected",
			body:         `{"usage":{"prompt_tokens":80,"completion_tokens":1,"total_tokens":81,"prompt_cache_miss_tokens":80}}`,
			cacheProfile: "cache-usage.deepseek-v4.v1",
			wantErr:      true,
		},
		{
			name:         "DeepSeek profile rejects OpenAI nested cache wire",
			body:         `{"usage":{"prompt_tokens":80,"completion_tokens":1,"total_tokens":81,"prompt_tokens_details":{"cached_tokens":80}}}`,
			cacheProfile: "cache-usage.deepseek-v4.v1",
			wantErr:      true,
		},
		{
			name:         "DeepSeek profile rejects contradictory cached-token mirror",
			body:         `{"usage":{"prompt_tokens":80,"completion_tokens":1,"total_tokens":81,"prompt_tokens_details":{"cached_tokens":79},"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":0}}`,
			cacheProfile: "cache-usage.deepseek-v4.v1",
			wantErr:      true,
		},
		{
			name:         "DeepSeek profile rejects Kimi top-level cache wire",
			body:         `{"usage":{"prompt_tokens":80,"completion_tokens":1,"total_tokens":81,"cached_tokens":80}}`,
			cacheProfile: "cache-usage.deepseek-v4.v1",
			wantErr:      true,
		},
		{
			name:    "provider token count above persisted integer limit is rejected",
			body:    `{"usage":{"prompt_tokens":2147483648,"completion_tokens":0,"total_tokens":2147483648}}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
				Endpoint:          EndpointChatCompletions,
				StatusCode:        http.StatusOK,
				Body:              []byte(tt.body),
				Channel:           models.Channel{Provider: "fixture", AdapterType: TypeOpenAICompatible},
				CacheUsageProfile: tt.cacheProfile,
			})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got usage %+v", resp.Usage)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseResponse failed: %v", err)
			}
			if resp.Usage.PromptTokens != tt.prompt ||
				resp.Usage.UncachedPromptTokens != tt.uncached ||
				resp.Usage.CacheReadTokens != tt.read ||
				resp.Usage.CacheCreationTokens != tt.creation ||
				resp.Usage.CacheUsageSchema != tt.schema ||
				resp.Usage.CacheUsageStatus != tt.status {
				t.Fatalf("unexpected usage: %+v", resp.Usage)
			}
		})
	}
}

func TestKimiTopLevelCacheUsageRequiresExactProfile(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"cached_tokens":80,"prompt_tokens_details":{"cached_tokens":null,"audio_tokens":3}}}`)
	adapter := NewOpenAICompatible()

	kimi, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel:           models.Channel{Provider: "moonshot", AdapterType: TypeOpenAICompatible},
		CacheUsageProfile: "cache-usage.kimi-platform.v1",
	})
	if err != nil {
		t.Fatalf("parse Kimi cache usage: %v", err)
	}
	if kimi.Usage.PromptTokens != 100 || kimi.Usage.UncachedPromptTokens != 20 ||
		kimi.Usage.CacheReadTokens != 80 || kimi.Usage.CacheUsageSchema != CacheUsageSchemaKimiV1 ||
		kimi.Usage.CacheUsageStatus != CacheUsageStatusReadReported {
		t.Fatalf("unexpected Kimi cache usage: %+v", kimi.Usage)
	}
	var kimiWire struct {
		Usage struct {
			CachedTokens  int `json:"cached_tokens"`
			PromptDetails struct {
				CachedTokens int `json:"cached_tokens"`
				AudioTokens  int `json:"audio_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(kimi.Body, &kimiWire); err != nil ||
		kimiWire.Usage.CachedTokens != 80 || kimiWire.Usage.PromptDetails.CachedTokens != 80 ||
		kimiWire.Usage.PromptDetails.AudioTokens != 3 {
		t.Fatalf("Kimi client cache projection is incomplete: wire=%+v err=%v body=%s", kimiWire, err, kimi.Body)
	}

	generic, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel: models.Channel{Provider: "generic", AdapterType: TypeOpenAICompatible},
	})
	if err != nil {
		t.Fatalf("parse generic usage: %v", err)
	}
	if generic.Usage.CacheReadTokens != 0 || generic.Usage.CacheUsageSchema != "" ||
		generic.Usage.UncachedPromptTokens != 100 {
		t.Fatalf("generic route trusted Kimi-only usage: %+v", generic.Usage)
	}
	if string(generic.Body) != string(body) {
		t.Fatalf("generic route projected provider cache fields: %s", generic.Body)
	}

	coding, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK, Body: body,
		Channel:           models.Channel{Provider: "moonshot-coding", AdapterType: TypeOpenAICompatible},
		CacheUsageProfile: "cache-usage.kimi-coding.v1",
	})
	if err != nil {
		t.Fatalf("parse unverified Kimi Coding usage: %v", err)
	}
	if coding.Usage.CacheReadTokens != 0 || coding.Usage.CacheUsageSchema != "" ||
		coding.Usage.UncachedPromptTokens != 100 {
		t.Fatalf("unverified Kimi Coding profile trusted platform cache wire: %+v", coding.Usage)
	}
	if string(coding.Body) != string(body) {
		t.Fatalf("unverified Kimi Coding route projected provider cache fields: %s", coding.Body)
	}
}

func TestOpenAICompatibleNormalizeStreamFrameProjectsProfileCacheUsageForGenericClients(t *testing.T) {
	adapter := NewOpenAICompatible()
	tests := []struct {
		name         string
		body         string
		cacheProfile string
		wantCached   int
		wantChanged  bool
	}{
		{
			name: "Kimi top-level cache", cacheProfile: "cache-usage.kimi-platform.v1", wantCached: 80, wantChanged: true,
			body: `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"cached_tokens":80}}`,
		},
		{
			name: "DeepSeek hit miss", cacheProfile: "cache-usage.deepseek-v4.v1", wantCached: 80, wantChanged: true,
			body: `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`,
		},
		{
			name: "untyped provider extension remains unchanged", wantCached: 0,
			body: `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"cached_tokens":80}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := adapter.NormalizeStreamFrame(context.Background(), StreamChunkInput{
				Data: []byte(test.body), CacheUsageProfile: test.cacheProfile,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !test.wantChanged {
				if string(got) != test.body {
					t.Fatalf("untyped frame changed: %s", got)
				}
				return
			}
			var wire struct {
				Usage struct {
					PromptDetails struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"prompt_tokens_details"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(got, &wire); err != nil || wire.Usage.PromptDetails.CachedTokens != test.wantCached {
				t.Fatalf("cache projection=%+v err=%v body=%s", wire, err, got)
			}
		})
	}
}

func TestKimiTopLevelCacheUsagePreservesZeroAndRejectsContradiction(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantRead int
		wantErr  bool
	}{
		{name: "authoritative zero", body: `{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"cached_tokens":0}}`},
		{name: "cache exceeds prompt", body: `{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"cached_tokens":11}}`, wantErr: true},
		{name: "negative", body: `{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"cached_tokens":-1}}`, wantErr: true},
		{name: "wrong type", body: `{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"cached_tokens":"1"}}`, wantErr: true},
		{name: "matching nested mirror", body: `{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"cached_tokens":5,"prompt_tokens_details":{"cached_tokens":5}}}`, wantRead: 5},
		{name: "contradictory nested mirror", body: `{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"cached_tokens":5,"prompt_tokens_details":{"cached_tokens":4}}}`, wantErr: true},
		{name: "wrong cache schema", body: `{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":5}}}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
				[]byte(test.body), EndpointChatCompletions, metering.UsageSourceUpstream,
				"cache-usage.kimi-platform.v1",
			)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error, got present=%t usage=%+v", present, usage)
				}
				return
			}
			if err != nil || !present || usage.CacheReadTokens != test.wantRead ||
				usage.CacheUsageSchema != CacheUsageSchemaKimiV1 {
				t.Fatalf("unexpected result: present=%t usage=%+v err=%v", present, usage, err)
			}
		})
	}
}

func TestNormalizeDeepSeekFlashChatToolResponseIsExactScopedAndLossless(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-deepseek","model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"index":999999999999999999999999999999999999999,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{}"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	normalized, changed, err := NormalizeDeepSeekFlashChatToolResponse(
		"deepseek", "deepseek-v4-flash", "protocol.deepseek-openai-chat.v4", body,
	)
	require.NoError(t, err)
	require.True(t, changed)
	var envelope map[string]interface{}
	require.NoError(t, json.Unmarshal(normalized, &envelope))
	choice := envelope["choices"].([]interface{})[0].(map[string]interface{})
	require.EqualValues(t, 0, choice["index"])
	tool := choice["message"].(map[string]interface{})["tool_calls"].([]interface{})[0].(map[string]interface{})
	require.NotContains(t, tool, "index")
	require.Equal(t, "call_1", tool["id"])

	unknown := []byte(strings.Replace(string(body), `"index":999999999999999999999999999999999999999,"id"`, `"index":0,"unexpected":true,"id"`, 1))
	normalized, changed, err = NormalizeDeepSeekFlashChatToolResponse(
		"deepseek", "deepseek-v4-flash", "openai.chat_completions", unknown,
	)
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, string(normalized), `"unexpected":true`)

	for _, invalid := range []string{`null`, `"0"`, `-1`, `0.5`, `1e2`, `01`} {
		mutated := []byte(strings.Replace(string(body), `999999999999999999999999999999999999999`, invalid, 1))
		_, _, normalizeErr := NormalizeDeepSeekFlashChatToolResponse(
			"deepseek", "deepseek-v4-flash", "protocol.deepseek-openai-chat.v4", mutated,
		)
		require.Error(t, normalizeErr, invalid)
	}

	for _, scope := range []struct{ provider, model, protocol string }{
		{"other", "deepseek-v4-flash", "protocol.deepseek-openai-chat.v4"},
		{"deepseek", "deepseek-v4-pro", "protocol.deepseek-openai-chat.v4"},
		{"deepseek", "deepseek-v4-flash", "protocol.deepseek-openai-chat.v3"},
	} {
		unchanged, applied, scopeErr := NormalizeDeepSeekFlashChatToolResponse(scope.provider, scope.model, scope.protocol, body)
		require.NoError(t, scopeErr)
		require.False(t, applied)
		require.Equal(t, body, unchanged)
	}
}

func TestOpenAICompatibleParseResponseNormalizesDeepSeekFlashToolCallIndex(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-deepseek","model":"deepseek-v4-flash","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{}"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	response, err := NewOpenAICompatible().ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK,
		Channel:       models.Channel{Provider: "deepseek", AdapterType: TypeOpenAICompatible},
		UpstreamModel: "deepseek-v4-flash", ProtocolProfile: "openai.chat_completions", Body: body,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, response.Usage.TotalTokens)
	var envelope map[string]interface{}
	require.NoError(t, json.Unmarshal(response.Body, &envelope))
	tool := envelope["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["tool_calls"].([]interface{})[0].(map[string]interface{})
	require.NotContains(t, tool, "index")

	invalid := []byte(strings.Replace(string(body), `"index":0,"id":"call_1"`, `"index":null,"id":"call_1"`, 1))
	response, err = NewOpenAICompatible().ParseResponse(context.Background(), ResponseInput{
		Endpoint: EndpointChatCompletions, StatusCode: http.StatusOK,
		Channel:       models.Channel{Provider: "deepseek", AdapterType: TypeOpenAICompatible},
		UpstreamModel: "deepseek-v4-flash", ProtocolProfile: "openai.chat_completions", Body: invalid,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, response.Usage.TotalTokens)
	require.Contains(t, string(response.Body), `"index":null`)
}

func TestOpenAICompatibleClassifyHTTPStatus(t *testing.T) {
	adapter := NewOpenAICompatible()
	cases := []struct {
		status int
		kind   FailureKind
	}{
		{400, FailureClientRequest},
		{401, FailureAuth},
		{429, FailureRateLimit},
		{500, FailureChannel},
	}
	for _, tc := range cases {
		got := adapter.ClassifyHTTPStatus(tc.status, []byte(`{"error":{"message":"boom"}}`))
		if got.FailureKind != tc.kind {
			t.Fatalf("status %d kind = %s, want %s", tc.status, got.FailureKind, tc.kind)
		}
		if got.Message != "boom" {
			t.Fatalf("status %d message = %q", tc.status, got.Message)
		}
	}
}

func TestOpenAICompatibleTest(t *testing.T) {
	adapter := NewOpenAICompatible()
	result, err := adapter.Test(context.Background(), TestInput{
		Channel: models.Channel{BaseURL: "https://api.example.com", Provider: "openai", AdapterType: TypeOpenAICompatible},
		APIKey:  "sk-test",
		Model:   "gpt-4o",
		Client: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(req.URL.String(), "/v1/chat/completions") {
				t.Fatalf("unexpected url: %s", req.URL.String())
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read test request: %v", err)
			}
			var payload map[string]interface{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode test request: %v", err)
			}
			if _, exists := payload["temperature"]; exists {
				t.Fatalf("connectivity test must not force an optional temperature: %s", body)
			}
			if _, exists := payload["max_tokens"]; exists {
				t.Fatalf("connectivity test must not force an optional token limit: %s", body)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"model":"gpt-4o-2026-08-06","usage":{"prompt_tokens":6,"completion_tokens":5,"total_tokens":11}}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("Test failed: %v", err)
	}
	if !result.Success || result.TokensUsed != 11 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.ReportedModel != "gpt-4o-2026-08-06" {
		t.Fatalf("reported model = %q", result.ReportedModel)
	}
}
