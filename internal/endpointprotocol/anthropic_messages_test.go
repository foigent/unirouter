package endpointprotocol

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestAnthropicMessagesProfileStrictTextRequestAndUsage(t *testing.T) {
	profile := mustProfile(t, ProfileAnthropicMessagesText)
	request, binding := mustBuild(t, profile, BuildInput{
		BaseURL:        "https://api.anthropic.com",
		APIKey:         "test-secret",
		RequestedModel: "company-claude",
		MappedModel:    "claude-sonnet-4-20250514",
		Body: []byte(`{
			"model":"company-claude",
			"max_tokens":256,
			"system":"Answer briefly.",
			"messages":[
				{"role":"user","content":"hello"},
				{"role":"assistant","content":"hi"},
				{"role":"user","content":"continue"}
			],
			"temperature":0.2
		}`),
	})
	if request.Method != http.MethodPost || request.URL.String() != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("request target=%s %s", request.Method, request.URL)
	}
	if request.Header.Get("x-api-key") != "test-secret" ||
		request.Header.Get("anthropic-version") != anthropicMessagesAPIVersion ||
		request.Header.Get("Authorization") != "" {
		t.Fatalf("headers=%v", request.Header)
	}
	wire := readJSONObject(t, request)
	if wire["model"] != "claude-sonnet-4-20250514" || wire["stream"] != false || wire["max_tokens"] != float64(256) {
		t.Fatalf("unsafe Anthropic wire body=%v", wire)
	}

	outcome, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(`{
		"id":"msg_fixture",
		"type":"message",
		"role":"assistant",
		"model":"claude-sonnet-4-20250514",
		"content":[{"type":"text","text":"done"}],
		"stop_reason":"end_turn",
		"stop_sequence":null,
		"usage":{
			"input_tokens":20,
			"cache_creation_input_tokens":30,
			"cache_read_input_tokens":80,
			"output_tokens":5,
			"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20}
		}
	}`))
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	wantUnits := []UnitObservation{
		observedUnit(UnitInputTokens, "20"),
		observedUnit(UnitCacheWriteTokens, "30"),
		observedUnit(UnitCacheReadTokens, "80"),
		observedUnit(UnitOutputTokens, "5"),
	}
	if outcome.ProviderObject != "msg_fixture" || outcome.OutputCount != 1 || !reflect.DeepEqual(outcome.Units, wantUnits) {
		t.Fatalf("outcome=%+v want units=%+v", outcome, wantUnits)
	}
}

func TestAnthropicMessagesProfileRejectsSemanticDegradationBeforeDispatch(t *testing.T) {
	profile := mustProfile(t, ProfileAnthropicMessagesText)
	base := func(body string) BuildInput {
		return BuildInput{
			BaseURL:        "https://api.anthropic.com",
			APIKey:         "test-secret",
			RequestedModel: "company-claude",
			MappedModel:    "claude-sonnet-4-20250514",
			Body:           []byte(body),
		}
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "tools", body: `{"model":"company-claude","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"tools":[]}`},
		{name: "tool result", body: `{"model":"company-claude","max_tokens":16,"messages":[{"role":"tool","content":"result"}]}`},
		{name: "non text", body: `{"model":"company-claude","max_tokens":16,"messages":[{"role":"user","content":[{"type":"image","source":{}}]}]}`},
		{name: "thinking", body: `{"model":"company-claude","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"adaptive"}}`},
		{name: "beta", body: `{"model":"company-claude","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"beta":"feature"}`},
		{name: "cache control", body: `{"model":"company-claude","max_tokens":16,"messages":[{"role":"user","content":"hello","cache_control":{"type":"ephemeral"}}]}`},
		{name: "stream", body: `{"model":"company-claude","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := profile.BuildRequest(context.Background(), base(test.body))
			if !errors.Is(err, ErrUnsupportedSemantics) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestAnthropicMessagesProfileRejectsNonTextOrIncompleteResponseEvidence(t *testing.T) {
	profile := mustProfile(t, ProfileAnthropicMessagesText)
	_, binding := mustBuild(t, profile, BuildInput{
		BaseURL:        "https://api.anthropic.com",
		APIKey:         "test-secret",
		RequestedModel: "company-claude",
		MappedModel:    "claude-sonnet-4-20250514",
		Body:           []byte(`{"model":"company-claude","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`),
	})
	tests := []struct {
		name string
		body string
		kind error
	}{
		{
			name: "tool use content",
			body: `{"id":"msg","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"tool_use","id":"tool","name":"lookup","input":{}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}`,
			kind: ErrUnsupportedSemantics,
		},
		{
			name: "cache detail mismatch",
			body: `{"id":"msg","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"cache_creation_input_tokens":3,"cache_read_input_tokens":0,"output_tokens":1,"cache_creation":{"ephemeral_5m_input_tokens":1,"ephemeral_1h_input_tokens":1}}}`,
			kind: ErrInvalidResponse,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(test.body))
			if !errors.Is(err, test.kind) {
				t.Fatalf("error=%v want kind=%v", err, test.kind)
			}
		})
	}
}

func TestAnthropicMessagesProfileNormalizesNullableCacheCountersWithoutLosingPositiveEvidence(t *testing.T) {
	profile := mustProfile(t, ProfileAnthropicMessagesText)
	_, binding := mustBuild(t, profile, BuildInput{
		BaseURL:        "https://api.anthropic.com",
		APIKey:         "test-secret",
		RequestedModel: "company-claude",
		MappedModel:    "claude-sonnet-4-20250514",
		Body:           []byte(`{"model":"company-claude","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`),
	})
	tests := []struct {
		name       string
		cacheUsage string
		wantWrite  string
		wantRead   string
	}{
		{name: "absent", cacheUsage: "", wantWrite: "0", wantRead: "0"},
		{name: "null", cacheUsage: `,"cache_creation_input_tokens":null,"cache_read_input_tokens":null`, wantWrite: "0", wantRead: "0"},
		{name: "explicit zero", cacheUsage: `,"cache_creation_input_tokens":0,"cache_read_input_tokens":0`, wantWrite: "0", wantRead: "0"},
		{name: "positive provider evidence", cacheUsage: `,"cache_creation_input_tokens":3,"cache_read_input_tokens":7`, wantWrite: "3", wantRead: "7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"id":"msg","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1` + tt.cacheUsage + `}}`
			outcome, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(body))
			if err != nil {
				t.Fatalf("ParseResponse failed: %v", err)
			}
			if outcome.Units[1].Quantity != tt.wantWrite || outcome.Units[2].Quantity != tt.wantRead {
				t.Fatalf("units=%+v", outcome.Units)
			}
		})
	}
}

func TestAnthropicMessagesStatusClassification(t *testing.T) {
	profile := mustProfile(t, ProfileAnthropicMessagesText)
	tests := []struct {
		status    int
		kind      string
		retryable bool
	}{
		{http.StatusBadRequest, "client_request", false},
		{http.StatusUnauthorized, "auth", false},
		{http.StatusPaymentRequired, "billing", false},
		{http.StatusConflict, "conflict", false},
		{http.StatusTooManyRequests, "rate_limit", true},
		{http.StatusGatewayTimeout, "channel_failure", true},
		{529, "channel_failure", true},
	}
	for _, test := range tests {
		kind, retryable := profile.ClassifyHTTPStatus(test.status)
		if kind != test.kind || retryable != test.retryable {
			t.Fatalf("status %d => %s/%t want %s/%t", test.status, kind, retryable, test.kind, test.retryable)
		}
	}
}
