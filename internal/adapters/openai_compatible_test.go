package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

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

func TestOpenAICompatibleParseResponse_Usage(t *testing.T) {
	adapter := NewOpenAICompatible()
	resp, err := adapter.ParseResponse(context.Background(), ResponseInput{
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
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"usage":{"total_tokens":11}}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("Test failed: %v", err)
	}
	if !result.Success || result.TokensUsed != 11 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
