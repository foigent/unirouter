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

func TestAnthropicNativeBuildRequest_ConvertsOpenAIChat(t *testing.T) {
	adapter := NewAnthropicNative()
	req, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions,
		DownstreamBody: []byte(`{
			"model":"company-chat",
			"messages":[
				{"role":"system","content":"You are concise."},
				{"role":"user","content":[{"type":"text","text":"Hello"}]},
				{"role":"assistant","content":"Hi"},
				{"role":"user","content":"Continue"}
			],
			"max_tokens":32,
			"temperature":0.2
		}`),
		RequestedModel: "company-chat",
		MappedModel:    "claude-3-5-sonnet-20241022",
		Channel: models.Channel{
			BaseURL:       "https://api.anthropic.com/v1",
			AdapterType:   TypeAnthropicNative,
			AdapterConfig: json.RawMessage(`{"anthropic_version":"2023-06-01"}`),
		},
		APIKey:    "sk-ant-test",
		RequestID: "req-ant-1",
	})
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	if got := req.Method; got != http.MethodPost {
		t.Fatalf("expected POST, got %s", got)
	}
	if got := req.URL.String(); got != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("unexpected url %q", got)
	}
	if got := req.Header.Get("x-api-key"); got != "sk-ant-test" {
		t.Fatalf("unexpected x-api-key %q", got)
	}
	if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("unexpected anthropic-version %q", got)
	}
	if got := req.Header.Get("anthropic-beta"); got != "" {
		t.Fatalf("unexpected anthropic-beta %q", got)
	}

	body, _ := io.ReadAll(req.Body)
	var payload struct {
		Model       string  `json:"model"`
		MaxTokens   int     `json:"max_tokens"`
		System      string  `json:"system"`
		Temperature float64 `json:"temperature"`
		Messages    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("bad payload: %v", err)
	}
	if payload.Model != "claude-3-5-sonnet-20241022" || payload.MaxTokens != 32 {
		t.Fatalf("unexpected model/max_tokens: %+v", payload)
	}
	if payload.System != "You are concise." {
		t.Fatalf("unexpected system prompt %q", payload.System)
	}
	if len(payload.Messages) != 3 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "Hello" {
		t.Fatalf("unexpected messages: %+v", payload.Messages)
	}
}

func TestAnthropicNativeBuildRequest_RejectsUnsupportedEndpoints(t *testing.T) {
	adapter := NewAnthropicNative()
	_, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointEmbeddings,
		DownstreamBody: []byte(`{"model":"claude","input":"hello"}`),
		Channel:        models.Channel{BaseURL: "https://api.anthropic.com"},
		APIKey:         "sk",
	})
	if err == nil || !strings.Contains(err.Error(), "does not support endpoint") {
		t.Fatalf("expected endpoint error, got %v", err)
	}
}

func TestAnthropicNativeBuildRequest_RejectsLossyCompatibilitySemantics(t *testing.T) {
	adapter := NewAnthropicNative()
	base := func(body string) RequestInput {
		return RequestInput{
			Endpoint:       EndpointChatCompletions,
			DownstreamBody: []byte(body),
			RequestedModel: "company-chat",
			MappedModel:    "claude-sonnet",
			Channel:        models.Channel{BaseURL: "https://api.anthropic.com"},
			APIKey:         "sk",
		}
	}
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "tools", body: `{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"tools":[]}`, want: "tools"},
		{name: "tool choice", body: `{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"tool_choice":"auto"}`, want: "tool"},
		{name: "tool message", body: `{"model":"company-chat","messages":[{"role":"tool","content":"result","tool_call_id":"call"}]}`, want: "tool messages"},
		{name: "image content", body: `{"model":"company-chat","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/x"}}]}]}`, want: "non-text"},
		{name: "structured output", body: `{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"response_format":{"type":"json_object"}}`, want: "structured output"},
		{name: "reasoning", body: `{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"}`, want: "reasoning"},
		{name: "cache control", body: `{"model":"company-chat","messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}]}`, want: "cache"},
		{name: "unknown field", body: `{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"provider_extension":true}`, want: "provider_extension"},
		{name: "temperature range", body: `{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"temperature":1.1}`, want: "temperature"},
		{name: "top p range", body: `{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"top_p":-0.1}`, want: "top_p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := adapter.BuildRequest(context.Background(), base(tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v want marker=%q", err, tt.want)
			}
		})
	}

	in := base(`{"model":"company-chat","messages":[{"role":"user","content":"hello"}]}`)
	in.Channel.AdapterConfig = json.RawMessage(`{"anthropic_version":"2023-06-01","anthropic_beta":"prompt-caching-2024-07-31"}`)
	if _, err := adapter.BuildRequest(context.Background(), in); err == nil || !strings.Contains(err.Error(), "anthropic_beta") {
		t.Fatalf("beta config was not rejected: %v", err)
	}
}

func TestAnthropicNativeBuildRequest_MapsMaxCompletionTokensWithoutLoss(t *testing.T) {
	req, err := NewAnthropicNative().BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointChatCompletions,
		DownstreamBody: []byte(`{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":77}`),
		RequestedModel: "company-chat",
		MappedModel:    "claude-sonnet",
		Channel:        models.Channel{BaseURL: "https://api.anthropic.com"},
		APIKey:         "sk",
	})
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	body, _ := io.ReadAll(req.Body)
	var payload struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.MaxTokens != 77 {
		t.Fatalf("max_completion_tokens was lost: body=%s err=%v", body, err)
	}
}

func TestAnthropicNativeBuildRequest_ConvertsStreamFlag(t *testing.T) {
	adapter := NewAnthropicNative()
	req, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointChatCompletions,
		DownstreamBody: []byte(`{"model":"claude","stream":true,"messages":[{"role":"user","content":"hello"}]}`),
		Channel:        models.Channel{BaseURL: "https://api.anthropic.com"},
		APIKey:         "sk",
	})
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	body, _ := io.ReadAll(req.Body)
	var payload struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("bad payload: %v", err)
	}
	if !payload.Stream {
		t.Fatalf("expected anthropic stream flag, got body %s", body)
	}
}

func TestAnthropicNativeParseResponse_ConvertsToOpenAICompatibleBody(t *testing.T) {
	adapter := NewAnthropicNative()
	resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"id":"msg_123",
			"type":"message",
			"role":"assistant",
			"model":"claude-3-5-sonnet-20241022",
			"content":[{"type":"text","text":"Hello"},{"type":"text","text":" world"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":7,"output_tokens":5}
		}`),
		Channel:        models.Channel{ID: 9, Name: "anthropic-primary", Provider: "anthropic", AdapterType: TypeAnthropicNative},
		RequestedModel: "company-chat",
		UpstreamModel:  "claude-3-5-sonnet-20241022",
	})
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 5 || resp.Usage.TotalTokens != 12 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if resp.Usage.UncachedPromptTokens != 7 || resp.Usage.CacheReadTokens != 0 || resp.Usage.CacheUsageStatus != CacheUsageStatusNotReported {
		t.Fatalf("unexpected uncached usage: %+v", resp.Usage)
	}
	if resp.Usage.Source != metering.UsageSourceUpstream {
		t.Fatalf("expected upstream usage source, got %s", resp.Usage.Source)
	}
	var body struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("decode normalized body: %v", err)
	}
	if body.ID != "msg_123" || body.Object != "chat.completion" || body.Model != "company-chat" {
		t.Fatalf("unexpected normalized body: %+v", body)
	}
	if len(body.Choices) != 1 || body.Choices[0].Message.Content != "Hello world" || body.Choices[0].FinishReason != "stop" {
		t.Fatalf("unexpected choices: %+v", body.Choices)
	}
	if body.Usage.TotalTokens != 12 {
		t.Fatalf("unexpected body usage: %+v", body.Usage)
	}
}

func TestAnthropicNativeParseResponse_RejectsUnsupportedContent(t *testing.T) {
	adapter := NewAnthropicNative()
	for _, body := range []string{
		`{"id":"msg_tool","content":[{"type":"tool_use","id":"tool","name":"lookup","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`,
		`{"id":"msg_thinking","content":[{"type":"thinking","thinking":"private"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
	} {
		if _, err := adapter.ParseResponse(context.Background(), ResponseInput{StatusCode: http.StatusOK, Body: []byte(body)}); err == nil || !strings.Contains(err.Error(), "cannot preserve") {
			t.Fatalf("unsupported response was not rejected: body=%s err=%v", body, err)
		}
	}
}

func TestP6F41AnthropicExplicitZeroAndMissingUsageRemainDistinct(t *testing.T) {
	adapter := NewAnthropicNative()
	parse := func(body string) Usage {
		t.Helper()
		resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
			StatusCode: http.StatusOK,
			Body:       []byte(body),
			Channel:    models.Channel{Provider: "anthropic", AdapterType: TypeAnthropicNative},
		})
		if err != nil {
			t.Fatalf("ParseResponse failed: %v", err)
		}
		return resp.Usage
	}

	explicitZero := parse(`{"id":"msg_zero","content":[],"usage":{"input_tokens":0,"output_tokens":0}}`)
	if explicitZero.IsMissing() || explicitZero.Authority != metering.UsageAuthorityProviderReported || explicitZero.Finality != metering.UsageFinalityFinal {
		t.Fatalf("explicit Anthropic zero lost provider authority: %+v", explicitZero)
	}
	missing := parse(`{"id":"msg_missing","content":[]}`)
	if !missing.IsMissing() || missing.Authority != metering.UsageAuthorityNone || missing.Finality != metering.UsageFinalityMissing {
		t.Fatalf("missing Anthropic usage became authoritative zero: %+v", missing)
	}
}

func TestAnthropicNativeIncompleteCoreUsageIsNotAuthoritative(t *testing.T) {
	adapter := NewAnthropicNative()
	tests := []struct {
		name  string
		usage string
	}{
		{name: "empty usage object", usage: `{}`},
		{name: "input only", usage: `{"input_tokens":7}`},
		{name: "output only", usage: `{"output_tokens":5}`},
		{name: "null input", usage: `{"input_tokens":null,"output_tokens":5}`},
		{name: "null output", usage: `{"input_tokens":7,"output_tokens":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
				StatusCode: http.StatusOK,
				Body:       []byte(`{"id":"msg_incomplete","content":[],"usage":` + tt.usage + `}`),
			})
			if err != nil {
				t.Fatalf("incomplete usage should allow the caller fallback: %v", err)
			}
			if !resp.Usage.IsMissing() {
				t.Fatalf("incomplete usage became provider authority: %+v", resp.Usage)
			}
		})
	}
}

func TestAnthropicNativeParseResponse_CacheReadCreationUsage(t *testing.T) {
	adapter := NewAnthropicNative()
	resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"id":"msg_cache",
			"model":"claude-sonnet",
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"usage":{
				"input_tokens":20,
				"cache_read_input_tokens":80,
				"cache_creation_input_tokens":30,
				"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20},
				"output_tokens":5
			}
		}`),
		Channel: models.Channel{Provider: "anthropic", AdapterType: TypeAnthropicNative},
	})
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	usage := resp.Usage
	if usage.PromptTokens != 130 || usage.UncachedPromptTokens != 20 ||
		usage.CacheReadTokens != 80 || usage.CacheCreationTokens != 30 ||
		usage.CompletionTokens != 5 || usage.TotalTokens != 135 ||
		usage.CacheUsageSchema != CacheUsageSchemaAnthropicV1 ||
		usage.CacheUsageStatus != CacheUsageStatusReadWriteReported {
		t.Fatalf("unexpected cache usage: %+v", usage)
	}
	if !usage.HasAuthoritativeCacheHit() {
		t.Fatal("expected authoritative cache hit")
	}
}

func TestAnthropicNativeParseResponse_RejectsContradictoryCreationDetail(t *testing.T) {
	adapter := NewAnthropicNative()
	_, err := adapter.ParseResponse(context.Background(), ResponseInput{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"id":"msg_cache",
			"content":[],
			"usage":{
				"input_tokens":20,
				"cache_creation_input_tokens":30,
				"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":19},
				"output_tokens":0
			}
		}`),
	})
	if err == nil {
		t.Fatal("expected contradictory cache creation detail to fail")
	}
}

func TestAnthropicNativeParseResponse_CacheUsageStatusReflectsReportedClasses(t *testing.T) {
	adapter := NewAnthropicNative()
	tests := []struct {
		name       string
		usage      string
		wantStatus string
	}{
		{
			name:       "read only",
			usage:      `{"input_tokens":20,"cache_read_input_tokens":80,"output_tokens":1}`,
			wantStatus: CacheUsageStatusReadReported,
		},
		{
			name:       "creation only",
			usage:      `{"input_tokens":20,"cache_creation_input_tokens":30,"output_tokens":1}`,
			wantStatus: CacheUsageStatusWriteReported,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
				StatusCode: http.StatusOK,
				Body:       []byte(`{"id":"msg_cache_status","content":[],"usage":` + tt.usage + `}`),
			})
			if err != nil {
				t.Fatalf("ParseResponse failed: %v", err)
			}
			if resp.Usage.CacheUsageStatus != tt.wantStatus {
				t.Fatalf("expected status %s, got %+v", tt.wantStatus, resp.Usage)
			}
		})
	}
}

func TestAnthropicNativeClassifyHTTPStatus(t *testing.T) {
	adapter := NewAnthropicNative()
	cases := []struct {
		status int
		kind   FailureKind
		retry  bool
	}{
		{400, FailureClientRequest, false},
		{401, FailureAuth, false},
		{429, FailureRateLimit, true},
		{500, FailureChannel, true},
		{529, FailureChannel, true},
	}
	for _, tc := range cases {
		got := adapter.ClassifyHTTPStatus(tc.status, []byte(`{"error":{"type":"overloaded_error","message":"overloaded"}}`))
		if got.FailureKind != tc.kind || got.Retryable != tc.retry {
			t.Fatalf("status %d = kind %s retry %v, want %s %v", tc.status, got.FailureKind, got.Retryable, tc.kind, tc.retry)
		}
		if got.Code != "overloaded_error" || got.Message != "overloaded" {
			t.Fatalf("status %d error parse failed: %+v", tc.status, got)
		}
	}
}

func TestAnthropicNativeNormalizeStreamChunk(t *testing.T) {
	adapter := NewAnthropicNative()
	state := &StreamState{}
	input := StreamChunkInput{
		RequestedModel: "company-chat",
		UpstreamModel:  "claude-3-5-sonnet-20241022",
		State:          state,
	}

	chunk, err := adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, `{"type":"message_start","message":{"id":"msg_stream","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":11,"output_tokens":0}}}`))
	if err != nil {
		t.Fatalf("message_start failed: %v", err)
	}
	if chunk.Skip || !strings.Contains(string(chunk.Data), `"role":"assistant"`) {
		t.Fatalf("expected role chunk, got %+v body=%s", chunk, chunk.Data)
	}
	if chunk.HasUsage {
		t.Fatalf("message_start should store partial usage without marking complete usage")
	}

	chunk, err = adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`))
	if err != nil {
		t.Fatalf("content_block_delta failed: %v", err)
	}
	if chunk.TextDelta != "Hello" || !strings.Contains(string(chunk.Data), `"content":"Hello"`) {
		t.Fatalf("expected content chunk, got %+v body=%s", chunk, chunk.Data)
	}

	chunk, err = adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`))
	if err != nil {
		t.Fatalf("message_delta failed: %v", err)
	}
	if !chunk.HasUsage || chunk.Usage.PromptTokens != 11 || chunk.Usage.CompletionTokens != 7 || chunk.Usage.Source != metering.UsageSourceUpstream {
		t.Fatalf("unexpected usage: %+v", chunk.Usage)
	}
	if !strings.Contains(string(chunk.Data), `"finish_reason":"stop"`) {
		t.Fatalf("expected stop chunk, got %s", chunk.Data)
	}

	chunk, err = adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, `{"type":"message_stop"}`))
	if err != nil {
		t.Fatalf("message_stop failed: %v", err)
	}
	if !chunk.Done || !chunk.Skip {
		t.Fatalf("expected done skip chunk, got %+v", chunk)
	}
}

func TestAnthropicNativeNormalizeStreamChunk_RejectsUnsupportedContent(t *testing.T) {
	adapter := NewAnthropicNative()
	for _, event := range []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool","name":"lookup","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":1}}`,
	} {
		_, err := adapter.NormalizeStreamChunk(context.Background(), StreamChunkInput{
			Data:  []byte(event),
			State: &StreamState{},
		})
		if err == nil || !strings.Contains(err.Error(), "cannot preserve") {
			t.Fatalf("unsupported stream event was not rejected: event=%s err=%v", event, err)
		}
	}
}

func TestAnthropicNativeNormalizeStreamChunk_RejectsUnknownEventType(t *testing.T) {
	_, err := NewAnthropicNative().NormalizeStreamChunk(context.Background(), StreamChunkInput{
		Data:  []byte(`{"type":"future_semantic_event","payload":{"private":"must-not-drop"}}`),
		State: &StreamState{},
	})
	if err == nil || !strings.Contains(err.Error(), "event type") {
		t.Fatalf("unknown stream event was silently ignored: %v", err)
	}

	chunk, err := NewAnthropicNative().NormalizeStreamChunk(context.Background(), StreamChunkInput{
		Data:  []byte(`{"type":"content_block_stop","index":0}`),
		State: &StreamState{},
	})
	if err != nil || chunk == nil || !chunk.Skip {
		t.Fatalf("known empty content_block_stop must remain compatible: chunk=%+v err=%v", chunk, err)
	}
}

func TestAnthropicNativeNormalizeStreamChunk_CarriesCachePartition(t *testing.T) {
	adapter := NewAnthropicNative()
	state := &StreamState{}
	input := StreamChunkInput{RequestedModel: "company-chat", State: state}

	_, err := adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, `{
		"type":"message_start",
		"message":{"id":"msg_stream_cache","usage":{
			"input_tokens":20,
			"cache_read_input_tokens":80,
			"cache_creation_input_tokens":30,
			"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":0},
			"output_tokens":0
		}}
	}`))
	if err != nil {
		t.Fatalf("message_start failed: %v", err)
	}
	chunk, err := adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`))
	if err != nil {
		t.Fatalf("message_delta failed: %v", err)
	}
	usage := chunk.Usage
	if !chunk.HasUsage || usage.PromptTokens != 130 || usage.UncachedPromptTokens != 20 ||
		usage.CacheReadTokens != 80 || usage.CacheCreationTokens != 30 ||
		usage.CompletionTokens != 7 || usage.CacheUsageSchema != CacheUsageSchemaAnthropicV1 {
		t.Fatalf("unexpected stream cache usage: %+v", usage)
	}
}

func TestAnthropicNativeNormalizeStreamChunk_MissingUsageAllowsFallback(t *testing.T) {
	adapter := NewAnthropicNative()
	state := &StreamState{}
	input := StreamChunkInput{RequestedModel: "company-chat", State: state}

	_, err := adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, `{"type":"message_start","message":{"id":"msg_stream","usage":{"input_tokens":11,"output_tokens":0}}}`))
	if err != nil {
		t.Fatalf("message_start failed: %v", err)
	}
	chunk, err := adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"fallback text"}}`))
	if err != nil {
		t.Fatalf("content_block_delta failed: %v", err)
	}
	if chunk.HasUsage {
		t.Fatalf("content delta without message_delta usage should not mark upstream usage")
	}
	if chunk.TextDelta != "fallback text" {
		t.Fatalf("expected fallback text delta, got %+v", chunk)
	}
}

func TestP6F41AnthropicStreamExplicitZeroAndMissingUsageRemainDistinct(t *testing.T) {
	adapter := NewAnthropicNative()
	finalUsage := func(messageStart string) *NormalizedStreamChunk {
		t.Helper()
		state := &StreamState{}
		input := StreamChunkInput{RequestedModel: "company-chat", State: state}
		if _, err := adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, messageStart)); err != nil {
			t.Fatalf("message_start failed: %v", err)
		}
		chunk, err := adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`))
		if err != nil {
			t.Fatalf("message_delta failed: %v", err)
		}
		return chunk
	}

	explicitZero := finalUsage(`{"type":"message_start","message":{"id":"msg_zero","usage":{"input_tokens":0,"output_tokens":0}}}`)
	if !explicitZero.HasUsage || explicitZero.Usage.IsMissing() || explicitZero.Usage.Authority != metering.UsageAuthorityProviderReported {
		t.Fatalf("explicit stream zero lost provider authority: %+v", explicitZero)
	}
	missing := finalUsage(`{"type":"message_start","message":{"id":"msg_missing"}}`)
	if missing.HasUsage {
		t.Fatalf("missing stream usage became authoritative zero: %+v", missing)
	}
}

func TestAnthropicNativeStreamIncompleteCoreUsageIsNotAuthoritative(t *testing.T) {
	adapter := NewAnthropicNative()
	tests := []struct {
		name         string
		messageStart string
		messageDelta string
		wantUsage    bool
	}{
		{
			name:         "empty start usage",
			messageStart: `{"type":"message_start","message":{"id":"msg_empty","usage":{}}}`,
			messageDelta: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		},
		{
			name:         "null start input",
			messageStart: `{"type":"message_start","message":{"id":"msg_null_input","usage":{"input_tokens":null}}}`,
			messageDelta: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		},
		{
			name:         "empty final usage",
			messageStart: `{"type":"message_start","message":{"id":"msg_empty_final","usage":{"input_tokens":7}}}`,
			messageDelta: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{}}`,
		},
		{
			name:         "null final output",
			messageStart: `{"type":"message_start","message":{"id":"msg_null_output","usage":{"input_tokens":7}}}`,
			messageDelta: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":null}}`,
		},
		{
			name:         "complete split usage",
			messageStart: `{"type":"message_start","message":{"id":"msg_complete","usage":{"input_tokens":7}}}`,
			messageDelta: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			wantUsage:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &StreamState{}
			input := StreamChunkInput{RequestedModel: "company-chat", State: state}
			if _, err := adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, tt.messageStart)); err != nil {
				t.Fatalf("message_start failed: %v", err)
			}
			chunk, err := adapter.NormalizeStreamChunk(context.Background(), withStreamData(input, tt.messageDelta))
			if err != nil {
				t.Fatalf("message_delta failed: %v", err)
			}
			if chunk.HasUsage != tt.wantUsage {
				t.Fatalf("HasUsage=%t, want %t: %+v", chunk.HasUsage, tt.wantUsage, chunk.Usage)
			}
		})
	}
}

func TestAnthropicNativeTest(t *testing.T) {
	adapter := NewAnthropicNative()
	result, err := adapter.Test(context.Background(), TestInput{
		Channel: models.Channel{BaseURL: "https://api.anthropic.com", Provider: "anthropic", AdapterType: TypeAnthropicNative},
		APIKey:  "sk-ant-test",
		Model:   "claude-3-5-sonnet-20241022",
		Client: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(req.URL.String(), "/v1/messages") {
				t.Fatalf("unexpected url: %s", req.URL.String())
			}
			if req.Header.Get("x-api-key") != "sk-ant-test" {
				t.Fatalf("missing x-api-key header")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"id":"msg_test",
					"model":"claude-3-5-sonnet-20241022",
					"content":[{"type":"text","text":"pong"}],
					"stop_reason":"end_turn",
					"usage":{"input_tokens":4,"output_tokens":3}
				}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("Test failed: %v", err)
	}
	if !result.Success || result.TokensUsed != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.ReportedModel != "claude-3-5-sonnet-20241022" {
		t.Fatalf("reported model = %q", result.ReportedModel)
	}
	if !strings.Contains(string(result.Body), `"chat.completion"`) {
		t.Fatalf("expected OpenAI-compatible body, got %s", result.Body)
	}
}

func TestDefaultRegistryIncludesAnthropicNative(t *testing.T) {
	if !DefaultRegistry().Has(TypeAnthropicNative) {
		t.Fatal("default registry should include anthropic_native")
	}
}

func withStreamData(in StreamChunkInput, data string) StreamChunkInput {
	in.Data = []byte(data)
	return in
}
