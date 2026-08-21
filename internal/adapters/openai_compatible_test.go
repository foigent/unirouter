package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

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

func TestOpenAICompatibleBuildRequest_PreservesOpenAIAndProviderFields(t *testing.T) {
	adapter := NewOpenAICompatible()
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto","parallel_tool_calls":true,"response_format":{"type":"json_object"},"stream_options":{"include_usage":true},"reasoning":{"effort":"high"},"reasoning_effort":"high","provider_counter":9223372036854775807}`)
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
	for _, field := range []string{"messages", "tools", "tool_choice", "parallel_tool_calls", "response_format", "stream_options", "reasoning", "reasoning_effort", "provider_counter"} {
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
		name     string
		body     string
		prompt   int
		uncached int
		read     int
		creation int
		schema   string
		status   string
		wantErr  bool
	}{
		{
			name:   "OpenAI cached-token subset",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":80}}}`,
			prompt: 100, uncached: 20, read: 80,
			schema: CacheUsageSchemaOpenAIV1, status: CacheUsageStatusReadReported,
		},
		{
			name:   "OpenAI cached-read and cache-write partition",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":60,"cache_write_tokens":30}}}`,
			prompt: 100, uncached: 10, read: 60, creation: 30,
			schema: CacheUsageSchemaOpenAIV2, status: CacheUsageStatusReadWriteReported,
		},
		{
			name:   "OpenAI cache-write-only contract",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cache_write_tokens":80}}}`,
			prompt: 100, uncached: 20, creation: 80,
			schema: CacheUsageSchemaOpenAIV2, status: CacheUsageStatusWriteReported,
		},
		{
			name:   "DeepSeek hit-miss partition",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`,
			prompt: 100, uncached: 20, read: 80,
			schema: CacheUsageSchemaDeepSeekV1, status: CacheUsageStatusHitMissReported,
		},
		{
			name:   "authoritative zero hit",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":0}}}`,
			prompt: 100, uncached: 100, read: 0,
			schema: CacheUsageSchemaOpenAIV1, status: CacheUsageStatusReadReported,
		},
		{
			name:   "unknown cache-like field is not billing truth",
			body:   `{"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"cache_hit_tokens":99}}`,
			prompt: 100, uncached: 100, read: 0,
			status: CacheUsageStatusNotReported,
		},
		{
			name:    "cached subset cannot exceed prompt total",
			body:    `{"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":11}}}`,
			wantErr: true,
		},
		{
			name:    "OpenAI read-write partition cannot exceed prompt total",
			body:    `{"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":21}}}`,
			wantErr: true,
		},
		{
			name:    "DeepSeek partition must match prompt total",
			body:    `{"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":19}}`,
			wantErr: true,
		},
		{
			name:    "DeepSeek partial hit-only contract is rejected",
			body:    `{"usage":{"prompt_tokens":80,"completion_tokens":1,"total_tokens":81,"prompt_cache_hit_tokens":80}}`,
			wantErr: true,
		},
		{
			name:    "DeepSeek partial miss-only contract is rejected",
			body:    `{"usage":{"prompt_tokens":80,"completion_tokens":1,"total_tokens":81,"prompt_cache_miss_tokens":80}}`,
			wantErr: true,
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
				Endpoint:   EndpointChatCompletions,
				StatusCode: http.StatusOK,
				Body:       []byte(tt.body),
				Channel:    models.Channel{Provider: "fixture", AdapterType: TypeOpenAICompatible},
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
