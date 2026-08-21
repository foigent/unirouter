package endpointprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"testing"
)

func TestOpenAIResponsesRawEnvelopePreservesKnownFieldsAndInt64(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	request, binding := mustBuild(t, profile, BuildInput{
		BaseURL:        "https://api.openai.com",
		APIKey:         "test-secret",
		RequestID:      "request-lossless",
		RequestedModel: "company-text",
		MappedModel:    "gpt-5.2",
		Body: []byte(`{
			"model":"company-text",
			"input":"private prompt",
			"max_output_tokens":9223372036854775807,
			"metadata":{"trace":"fixture"},
			"text":{"format":{"type":"text"},"verbosity":"low"}
		}`),
	})
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope["max_output_tokens"]) != "9223372036854775807" {
		t.Fatalf("provider field lost integer precision: %s", body)
	}
	if string(envelope["model"]) != `"gpt-5.2"` || string(envelope["store"]) != "false" || string(envelope["stream"]) != "false" {
		t.Fatalf("wire safety binding invalid: %s", body)
	}

	outcome, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(`{
		"id":"resp_lossless",
		"object":"response",
		"status":"completed",
		"model":"gpt-5.2",
		"error":null,
		"incomplete_details":null,
		"output_text":"answer",
		"output":[{"id":"msg_lossless","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer","future_content":{"n":92233720368547758081234567890}}],"future_item":true}],
		"usage":{"input_tokens":9223372036854775807,"input_tokens_details":null,"output_tokens":0,"output_tokens_details":{"reasoning_tokens":null},"total_tokens":9223372036854775807},
		"future_response_field":{"n":92233720368547758081234567890}
	}`))
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if outcome.ProviderObject != "resp_lossless" || outcome.OutputCount != 1 || !reflect.DeepEqual(
		outcome.Units,
		[]UnitObservation{
			observedUnit(UnitInputTokens, "9223372036854775807"),
			observedUnit(UnitOutputTokens, "0"),
		},
	) {
		t.Fatalf("outcome=%+v", outcome)
	}
}

func TestOpenAIResponsesUnknownRequestFieldsRemainRawButFailClosed(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	_, _, err := profile.BuildRequest(context.Background(), BuildInput{
		BaseURL:        "https://api.openai.com",
		APIKey:         "test-secret",
		RequestedModel: "company-text",
		MappedModel:    "gpt-5.2",
		Body:           []byte(`{"model":"company-text","input":"x","future_provider_field":{"n":92233720368547758081234567890}}`),
	})
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Code != "unsupported_unknown_provider_field" || !errors.Is(err, ErrUnsupportedSemantics) {
		t.Fatalf("unknown semantic field was not rejected safely: %v", err)
	}
}

func TestOpenAIResponsesFailClosedKnownUnsupportedSemantics(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	tests := []struct {
		name string
		body string
		code string
	}{
		{"tools", `{"model":"company-text","input":"x","tools":[]}`, "unsupported_tools"},
		{"tool choice", `{"model":"company-text","input":"x","tool_choice":"none"}`, "unsupported_tools"},
		{"structured text", `{"model":"company-text","input":"x","text":{"format":{"type":"json_schema","name":"x","schema":{}}}}`, "unsupported_structured_output"},
		{"chat response format", `{"model":"company-text","input":"x","response_format":{"type":"text"}}`, "unsupported_structured_output"},
		{"reasoning control", `{"model":"company-text","input":"x","reasoning":{"effort":"low"}}`, "unsupported_reasoning_controls"},
		{"include", `{"model":"company-text","input":"x","include":["message.output_text.logprobs"]}`, "unsupported_include_expansions"},
		{"store", `{"model":"company-text","input":"x","store":true}`, "unsupported_stored_response_state"},
		{"stream", `{"model":"company-text","input":"x","stream":true}`, "unsupported_streaming"},
		{"structured input", `{"model":"company-text","input":[{"role":"user","content":"x"}]}`, "unsupported_multimodal_or_structured_input"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := profile.BuildRequest(context.Background(), BuildInput{
				BaseURL:        "https://api.openai.com",
				APIKey:         "test-secret",
				RequestedModel: "company-text",
				MappedModel:    "gpt-5.2",
				Body:           []byte(test.body),
			})
			var protocolErr *ProtocolError
			if !errors.As(err, &protocolErr) || protocolErr.Code != test.code || !errors.Is(err, ErrUnsupportedSemantics) {
				t.Fatalf("error=%v code=%v want %s", err, protocolErr, test.code)
			}
		})
	}
}

func TestOpenAIResponsesValidatesRequestBoundsAndMetadata(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	tests := []string{
		`{"model":"company-text","input":"x","max_output_tokens":15}`,
		`{"model":"company-text","input":"x","max_output_tokens":9223372036854775808}`,
		`{"model":"company-text","input":"x","temperature":2.1}`,
		`{"model":"company-text","input":"x","top_p":1.1}`,
		`{"model":"company-text","input":"x","metadata":{"x":1}}`,
		`{"model":"company-text","input":"x","text":{"verbosity":"extreme"}}`,
	}
	for _, body := range tests {
		if _, _, err := profile.BuildRequest(context.Background(), BuildInput{
			BaseURL:        "https://api.openai.com",
			APIKey:         "test-secret",
			RequestedModel: "company-text",
			MappedModel:    "gpt-5.2",
			Body:           []byte(body),
		}); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid request accepted: %s error=%v", body, err)
		}
	}
}

func TestOpenAIResponsesUsageNullableBoundariesAndReasoning(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	_, binding := mustBuild(t, profile, baseResponsesInput())
	base := `{
		"id":"resp_usage","object":"response","status":"completed","model":"gpt-5.2",
		"error":null,"incomplete_details":null,
		"output":[{"id":"msg_usage","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"x"}]}],
		"usage":%s
	}`
	valid := []string{
		`{"input_tokens":3,"input_tokens_details":null,"output_tokens":2,"output_tokens_details":null,"total_tokens":5}`,
		`{"input_tokens":3,"input_tokens_details":{"cached_tokens":null,"cache_write_tokens":null},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":null},"total_tokens":5}`,
		`{"input_tokens":3,"input_tokens_details":{"cached_tokens":1},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":5}`,
	}
	for _, usage := range valid {
		if _, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(fmt.Sprintf(base, usage))); err != nil {
			t.Fatalf("valid nullable usage rejected: %s error=%v", usage, err)
		}
	}
	invalid := []string{
		`null`,
		`{"input_tokens":null,"output_tokens":2,"total_tokens":2}`,
		`{"input_tokens":3,"output_tokens":2,"output_tokens_details":{"reasoning_tokens":3},"total_tokens":5}`,
		`{"input_tokens":3,"input_tokens_details":{"cached_tokens":4},"output_tokens":2,"total_tokens":5}`,
		`{"input_tokens":` + strconv.FormatInt(math.MaxInt64, 10) + `,"output_tokens":1,"total_tokens":` + strconv.FormatInt(math.MaxInt64, 10) + `}`,
	}
	for _, usage := range invalid {
		if _, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(fmt.Sprintf(base, usage))); err == nil {
			t.Fatalf("invalid usage accepted: %s", usage)
		}
	}
}

func TestOpenAIResponsesTerminalAndOpenAIErrorClassification(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	_, binding := mustBuild(t, profile, baseResponsesInput())
	for status, kind := range map[string]error{
		"queued":      ErrNonFinalResponse,
		"in_progress": ErrNonFinalResponse,
		"incomplete":  ErrNonFinalResponse,
		"failed":      ErrUpstreamResponse,
		"cancelled":   ErrUpstreamResponse,
	} {
		body := `{"id":"resp_status","object":"response","status":"` + status + `","model":"gpt-5.2","error":null,"incomplete_details":null,"output":[],"usage":null}`
		if _, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(body)); !errors.Is(err, kind) {
			t.Fatalf("status=%s error=%v want %v", status, err, kind)
		}
	}
	for _, test := range []struct {
		status int
		code   string
	}{
		{http.StatusBadRequest, "http_400_client_request"},
		{http.StatusUnauthorized, "http_401_auth"},
		{http.StatusTooManyRequests, "http_429_rate_limit"},
		{http.StatusServiceUnavailable, "http_503_channel_failure"},
	} {
		_, err := profile.ParseResponse(context.Background(), binding, test.status, []byte(`{"error":{"message":"provider detail","type":"invalid_request_error","param":"input","code":"invalid_value"}}`))
		var protocolErr *ProtocolError
		if !errors.As(err, &protocolErr) || protocolErr.Code != test.code || !errors.Is(err, ErrUpstreamResponse) {
			t.Fatalf("status=%d error=%v", test.status, err)
		}
	}
}

func TestOpenAIResponsesRejectsOutputTextMismatchAndUnsupportedItems(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	_, binding := mustBuild(t, profile, baseResponsesInput())
	bad := []string{
		`{"id":"resp","object":"response","status":"completed","model":"gpt-5.2","error":null,"incomplete_details":null,"output_text":"different","output":[{"id":"msg","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"x"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		`{"id":"resp","object":"response","status":"completed","model":"gpt-5.2","error":null,"incomplete_details":null,"output":[{"id":"call","type":"function_call","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		`{"id":"resp","object":"response","status":"completed","model":"gpt-5.2","error":null,"incomplete_details":null,"output":[{"id":"msg","type":"message","status":"completed","role":"assistant","content":[{"type":"refusal","refusal":"no"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
	}
	for _, body := range bad {
		if _, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(body)); err == nil {
			t.Fatalf("unsupported response accepted: %s", body)
		}
	}
}
