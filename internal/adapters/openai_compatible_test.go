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
	if resp.Meta.RequestedModel != "company-chat" || resp.Meta.UpstreamModel != "gpt-4o" {
		t.Fatalf("unexpected meta: %+v", resp.Meta)
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
