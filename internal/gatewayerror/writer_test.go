package gatewayerror

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteAnthropicEnvelopeAndRequestHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	Write(recorder, "/v1/messages", "op_test_request", http.StatusUnauthorized, "ignored.internal.code", "Invalid API key")

	if recorder.Code != http.StatusUnauthorized ||
		recorder.Header().Get("request-id") != "op_test_request" ||
		recorder.Header().Get("X-Request-ID") != "op_test_request" {
		t.Fatalf("status=%d headers=%v", recorder.Code, recorder.Header())
	}
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Type != "error" || body.Error.Type != "authentication_error" ||
		body.Error.Message != "Invalid API key" || body.RequestID != "op_test_request" {
		t.Fatalf("body=%+v", body)
	}
}

func TestWriteOpenAIEnvelopeRemainsCompatible(t *testing.T) {
	recorder := httptest.NewRecorder()
	Write(recorder, "/v1/chat/completions", "op_not_in_legacy_body", http.StatusUnauthorized, "auth_error.invalid_api_key", "Invalid API key")

	var body map[string]map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	errorBody := body["error"]
	if errorBody["type"] != "authentication_error" ||
		errorBody["code"] != "auth_error.invalid_api_key" ||
		errorBody["message"] != "Invalid API key" ||
		errorBody["doc_url"] != "/docs#error-auth_error.invalid_api_key" ||
		errorBody["param"] != nil {
		t.Fatalf("body=%v", body)
	}
	if _, exists := body["request_id"]; exists {
		t.Fatalf("OpenAI compatibility body gained request_id: %v", body)
	}
}

func TestWriteOpenAIClassifiesRateLimitWithoutChangingStableCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	Write(recorder, "/v1/embeddings", "op_rate", http.StatusTooManyRequests,
		"limit_policy.rps_exceeded", "Shared request-per-second limit exceeded")

	var body map[string]map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"]["type"] != "rate_limit_error" ||
		body["error"]["code"] != "limit_policy.rps_exceeded" {
		t.Fatalf("body=%v", body)
	}
}

func TestWriteRequestUsesOuterOperationIdentity(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("X-Request-ID", "op_outer_identity")
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	request.Header.Set("X-Request-ID", "caller_must_not_win")
	WriteRequest(recorder, request, http.StatusBadRequest, "validation_error.invalid_request", "Invalid request")

	var body struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Error     struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Type != "error" || body.Error.Type != "invalid_request_error" ||
		body.RequestID != "op_outer_identity" || recorder.Header().Get("request-id") != "op_outer_identity" ||
		recorder.Header().Get("X-Request-ID") != "op_outer_identity" {
		t.Fatalf("body=%+v headers=%v", body, recorder.Header())
	}
}
