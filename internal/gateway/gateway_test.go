package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uniroute/unirouter/internal/capbilling"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/policybilling"
)

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "invalid_request_error", "validation_error.invalid_request", "model is required")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	var errResp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &errResp)
	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error object in response")
	}
	if errObj["type"] != "invalid_request_error" {
		t.Fatalf("expected type invalid_request, got %v", errObj["type"])
	}
	if errObj["message"] != "model is required" {
		t.Fatalf("expected message 'model is required', got %v", errObj["message"])
	}
	if errObj["doc_url"] != "/docs#error-validation_error.invalid_request" {
		t.Fatalf("expected doc_url for validation error, got %v", errObj["doc_url"])
	}
}

func TestChatCompletionRequest_Parse(t *testing.T) {
	maxTokens := 100
	req := ChatCompletionRequest{
		Model:     "gpt-4o",
		Messages:  []Message{{Role: "user", Content: "Hello"}},
		Stream:    false,
		MaxTokens: &maxTokens,
	}
	data, _ := json.Marshal(req)
	var decoded ChatCompletionRequest
	json.Unmarshal(data, &decoded)
	if decoded.Model != "gpt-4o" {
		t.Fatalf("model mismatch: %s", decoded.Model)
	}
	if len(decoded.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(decoded.Messages))
	}
	if decoded.Messages[0].Role != "user" {
		t.Fatalf("role mismatch: %s", decoded.Messages[0].Role)
	}
	if decoded.MaxTokens == nil || *decoded.MaxTokens != 100 {
		t.Fatal("MaxTokens mismatch")
	}
}

func TestSensitiveWordFilter_Integration(t *testing.T) {
	f := filter.NewDFAFilter([]string{"赌博", "色情", "spam"})

	testCases := []struct {
		content string
		expect  bool
	}{
		{"Hello, how are you?", false},
		{"我想了解一下赌博网站", true},
		{"正常的问题", false},
		{"this is spam content", true},
		{"clean message", false},
	}

	for _, tc := range testCases {
		matched, _ := f.IsSensitive(tc.content)
		if matched != tc.expect {
			t.Fatalf("content %q: expected match=%v, got %v", tc.content, tc.expect, matched)
		}
	}
}

func TestRequestWithContext(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := middleware.GetRequestIDFromContext(r.Context())
		orgID := middleware.GetOrgIDFromContext(r.Context())
		keyID := middleware.GetKeyIDFromContext(r.Context())

		if reqID != "test-req-123" {
			t.Fatalf("expected reqID=test-req-123, got %s", reqID)
		}
		if orgID != 42 {
			t.Fatalf("expected orgID=42, got %d", orgID)
		}
		if keyID != 99 {
			t.Fatalf("expected keyID=99, got %d", keyID)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxRequestID, "test-req-123")
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(42))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(99))
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestWriteError_VariousStatuses(t *testing.T) {
	cases := []struct {
		httpCode int
		typ      string
		code     string
		msg      string
	}{
		{400, "invalid_request_error", "validation_error.invalid", "bad input"},
		{401, "authentication_error", "auth_error.invalid_key", "unauthorized"},
		{403, "permission_error", "auth_error.model_not_allowed", "access denied"},
		{429, "rate_limit_error", "rate_limit.rpm_exceeded", "too many requests"},
		{500, "server_error", "server_error.internal", "something broke"},
		{502, "server_error", "server_error.upstream", "bad gateway"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeError(rec, c.httpCode, c.typ, c.code, c.msg)
		if rec.Code != c.httpCode {
			t.Fatalf("status %d: expected %d, got %d", c.httpCode, c.httpCode, rec.Code)
		}
		var resp map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		errObj := resp["error"].(map[string]interface{})
		if errObj["type"] != c.typ {
			t.Fatalf("status %d: type mismatch: %v", c.httpCode, errObj["type"])
		}
		if errObj["code"] != c.code {
			t.Fatalf("status %d: code mismatch: expected %s, got %v", c.httpCode, c.code, errObj["code"])
		}
	}
}

func TestWriteError_ContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 400, "test", "test.code", "msg")
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
}

func TestWriteError_InternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 500, "server_error", "server_error.internal", "something went wrong")
	if rec.Code != 500 {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	errObj := resp["error"].(map[string]interface{})
	if errObj["type"] != "server_error" {
		t.Fatalf("expected internal_error, got %v", errObj["type"])
	}
}

func TestWriteError_UnicodeMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 400, "test", "test.code", "请求包含禁止内容 🚫")
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	errObj := resp["error"].(map[string]interface{})
	if errObj["message"] != "请求包含禁止内容 🚫" {
		t.Fatalf("unicode emoji should be preserved, got %v", errObj["message"])
	}
}

func TestAgentCapabilityRuntimeRejectionHTTPContract(t *testing.T) {
	tests := []struct {
		code       string
		status     int
		retryAfter string
	}{
		{code: capbilling.ErrorCodeCostBudgetExceeded, status: http.StatusForbidden},
		{code: capbilling.ErrorCodeTokenBudgetExceeded, status: http.StatusForbidden},
		{code: capbilling.ErrorCodeRPSLimitExceeded, status: http.StatusTooManyRequests, retryAfter: "1"},
		{code: capbilling.ErrorCodeRPMLimitExceeded, status: http.StatusTooManyRequests, retryAfter: "60"},
		{code: capbilling.ErrorCodeConcurrencyExceeded, status: http.StatusTooManyRequests, retryAfter: "1"},
		{code: capbilling.ErrorCodeStateUnavailable, status: http.StatusServiceUnavailable, retryAfter: "1"},
		{code: capbilling.ErrorCodeRevoked, status: http.StatusUnauthorized},
		{code: capbilling.ErrorCodeConflict, status: http.StatusConflict},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			if !writeAgentCapabilityRuntimeRejection(recorder, test.code) {
				t.Fatal("typed runtime error was not recognized")
			}
			if recorder.Code != test.status || recorder.Header().Get("Retry-After") != test.retryAfter {
				t.Fatalf("status=%d Retry-After=%q, want %d/%q", recorder.Code, recorder.Header().Get("Retry-After"), test.status, test.retryAfter)
			}
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			errorObject := response["error"].(map[string]any)
			if errorObject["code"] != test.code {
				t.Fatalf("error code=%v, want %s", errorObject["code"], test.code)
			}
		})
	}
	if recorder := httptest.NewRecorder(); writeAgentCapabilityRuntimeRejection(recorder, "unknown") {
		t.Fatal("unknown error code must fall through to the ordinary billing handler")
	}
}

func TestLimitPolicyRuntimeRejectionHTTPContract(t *testing.T) {
	tests := []struct {
		code       string
		status     int
		retryAfter string
	}{
		{code: policybilling.ErrorCodeRPSExceeded, status: http.StatusTooManyRequests, retryAfter: "1"},
		{code: policybilling.ErrorCodeRPMExceeded, status: http.StatusTooManyRequests, retryAfter: "60"},
		{code: policybilling.ErrorCodeTPMExceeded, status: http.StatusTooManyRequests, retryAfter: "60"},
		{code: policybilling.ErrorCodeConcurrencyExceeded, status: http.StatusTooManyRequests, retryAfter: "1"},
		{code: policybilling.ErrorCodeWorkloadCapacity, status: http.StatusTooManyRequests, retryAfter: "1"},
		{code: policybilling.ErrorCodeWorkloadConfig, status: http.StatusServiceUnavailable, retryAfter: "1"},
		{code: policybilling.ErrorCodeStateUnavailable, status: http.StatusServiceUnavailable, retryAfter: "1"},
		{code: policybilling.ErrorCodeConflict, status: http.StatusConflict},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			if !writeLimitPolicyRuntimeRejection(recorder, test.code) {
				t.Fatal("typed limit-policy error was not recognized")
			}
			if recorder.Code != test.status || recorder.Header().Get("Retry-After") != test.retryAfter {
				t.Fatalf("status=%d Retry-After=%q, want %d/%q", recorder.Code, recorder.Header().Get("Retry-After"), test.status, test.retryAfter)
			}
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			errorObject := response["error"].(map[string]any)
			if errorObject["code"] != test.code || errorObject["doc_url"] == "" {
				t.Fatalf("error object=%#v", errorObject)
			}
		})
	}
	if recorder := httptest.NewRecorder(); writeLimitPolicyRuntimeRejection(recorder, "unknown") {
		t.Fatal("unknown error code must fall through to the ordinary billing handler")
	}
}

func TestLimitPolicyDecisionSummaryPreservesDegradedEvidencePriority(t *testing.T) {
	decisions := []limitpolicy.Decision{{
		PolicyRef: "policy:fallback", PolicyVersion: 1, SelectorHash: "one",
		Mode: limitpolicy.ModeEnforce, Decision: limitpolicy.DecisionAllow, Reason: limitpolicy.ReasonLastKnownSafe,
	}}
	result, reason, enforce, shadow, _ := summarizeLimitPolicyDecisions(decisions)
	if result != "degraded_allow" || reason != limitpolicy.ReasonLastKnownSafe || enforce != 1 || shadow != 0 {
		t.Fatalf("fallback summary = %s/%s enforce=%d shadow=%d", result, reason, enforce, shadow)
	}

	decisions = append(decisions, limitpolicy.Decision{
		PolicyRef: "policy:shadow", PolicyVersion: 2, SelectorHash: "two",
		Mode: limitpolicy.ModeShadow, Decision: limitpolicy.DecisionWouldDeny, Reason: limitpolicy.ReasonRPMExceeded,
	})
	result, reason, _, _, _ = summarizeLimitPolicyDecisions(decisions)
	if result != "shadow_would_deny" || reason != limitpolicy.ReasonRPMExceeded {
		t.Fatalf("shadow summary = %s/%s", result, reason)
	}

	decisions = append(decisions, limitpolicy.Decision{
		PolicyRef: "policy:deny", PolicyVersion: 3, SelectorHash: "three",
		Mode: limitpolicy.ModeEnforce, Decision: limitpolicy.DecisionDeny, Reason: limitpolicy.ReasonConcurrency,
	})
	result, reason, _, _, _ = summarizeLimitPolicyDecisions(decisions)
	if result != "rejected" || reason != limitpolicy.ReasonConcurrency {
		t.Fatalf("denial summary = %s/%s", result, reason)
	}
}

func TestWorkloadDecisionSummaryUsesServerDerivedBoundedFields(t *testing.T) {
	class, weight, waitMS, queueDepth := summarizeWorkloadDecision([]limitpolicy.Decision{
		{PolicyRef: "policy:ordinary"},
		{
			PolicyRef: "builtin:workload-admission-v2", WorkloadClass: "stream",
			WorkloadWeight: 4, WaitMS: 23, QueueDepth: 7,
		},
	})
	if class != "stream" || weight != 4 || waitMS != 23 || queueDepth != 7 {
		t.Fatalf("workload summary=%q/%d/%d/%d", class, weight, waitMS, queueDepth)
	}
}

func TestWriteError_ContentTypeOnError(t *testing.T) {
	testCases := []int{400, 401, 403, 404, 429, 500, 502, 503}
	for _, code := range testCases {
		rec := httptest.NewRecorder()
		writeError(rec, code, "test", "test.code", "msg")
		ct := rec.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("code %d: expected application/json, got %s", code, ct)
		}
	}
}

// --- ChatCompletionRequest additional parse tests ---

func TestChatCompletionRequest_Parse_Stream(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	}
	data, _ := json.Marshal(req)
	var decoded ChatCompletionRequest
	json.Unmarshal(data, &decoded)
	if !decoded.Stream {
		t.Fatal("stream should be true")
	}
}

func TestChatCompletionRequest_Parse_NilMaxTokens(t *testing.T) {
	data := []byte(`{"model": "gpt-4o", "messages": [{"role": "user", "content": "hi"}]}`)
	var decoded ChatCompletionRequest
	json.Unmarshal(data, &decoded)
	if decoded.MaxTokens != nil {
		t.Fatal("max_tokens should be nil when omitted")
	}
}

func TestChatCompletionRequest_Parse_ZeroMaxTokens(t *testing.T) {
	maxTokens := 0
	req := ChatCompletionRequest{
		Model:     "gpt-4o",
		Messages:  []Message{{Role: "user", Content: "hi"}},
		MaxTokens: &maxTokens,
	}
	data, _ := json.Marshal(req)
	var decoded ChatCompletionRequest
	json.Unmarshal(data, &decoded)
	if decoded.MaxTokens == nil || *decoded.MaxTokens != 0 {
		t.Fatal("max_tokens should be 0")
	}
}

func TestChatCompletionRequest_Parse_ExtraFields(t *testing.T) {
	data := []byte(`{"model": "gpt-4o", "messages": [{"role": "user", "content": "hi"}], "temperature": 0.7, "top_p": 1.0}`)
	var decoded ChatCompletionRequest
	err := json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("extra fields should not cause error: %v", err)
	}
	if decoded.Model != "gpt-4o" {
		t.Fatalf("model mismatch: %s", decoded.Model)
	}
}

func TestChatCompletionRequest_Parse_OpenAICompatibilityEnvelope(t *testing.T) {
	data := []byte(`{
		"model":"deepseek-v4-flash",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"Use the image to answer"},
				{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}
			]},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"id\":1}"}}]}
		],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"tool_choice":"auto",
		"parallel_tool_calls":true,
		"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}},
		"stream":true,
		"stream_options":{"include_usage":true},
		"reasoning_effort":"high",
		"max_completion_tokens":2048
	}`)
	var request ChatCompletionRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("OpenAI-compatible envelope should parse: %v", err)
	}
	if request.Model != "deepseek-v4-flash" || !request.Stream {
		t.Fatalf("unexpected routing envelope: %+v", request)
	}
	if got := request.Messages[0].Content; got != "Use the image to answer" {
		t.Fatalf("multimodal text projection = %q", got)
	}
	if request.Messages[0].nonTextParts != 1 {
		t.Fatalf("expected one non-text part, got %d", request.Messages[0].nonTextParts)
	}
	if limit := request.completionTokenLimit(); limit == nil || *limit != 2048 {
		t.Fatalf("max_completion_tokens was not recognized: %#v", limit)
	}
	assistantJSON, err := json.Marshal(request.Messages[1])
	if err != nil {
		t.Fatal(err)
	}
	var assistant map[string]json.RawMessage
	if err := json.Unmarshal(assistantJSON, &assistant); err != nil {
		t.Fatal(err)
	}
	if string(assistant["content"]) != "null" || len(assistant["tool_calls"]) == 0 {
		t.Fatalf("assistant tool-call envelope was not retained: %s", assistantJSON)
	}
}

func TestChatCompletionRequest_MaxCompletionTokensDrivesReservation(t *testing.T) {
	data := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":700}`)
	var request ChatCompletionRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	_, reserved := estimateTokenReservation(request.Messages, request.completionTokenLimit())
	if reserved != 700 || !request.hasCompletionTokenLimit() {
		t.Fatalf("reservation=%d has_limit=%t, want 700/true", reserved, request.hasCompletionTokenLimit())
	}
}

func TestChatCompletionRequest_Parse_EmptyMessages(t *testing.T) {
	data := []byte(`{"model": "gpt-4o", "messages": []}`)
	var decoded ChatCompletionRequest
	err := json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("empty messages should parse: %v", err)
	}
	if len(decoded.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(decoded.Messages))
	}
}

// --- EmbeddingsRequest parse tests ---

func TestEmbeddingsRequest_Parse_StringInput(t *testing.T) {
	data := []byte(`{"model": "text-embedding-3-small", "input": "hello world"}`)
	var req EmbeddingsRequest
	err := json.Unmarshal(data, &req)
	if err != nil {
		t.Fatalf("should parse string input: %v", err)
	}
	if req.Model != "text-embedding-3-small" {
		t.Fatalf("model mismatch: %s", req.Model)
	}
	s, ok := req.Input.(string)
	if !ok || s != "hello world" {
		t.Fatalf("input mismatch: %v", req.Input)
	}
}

func TestEmbeddingsRequest_Parse_ArrayInput(t *testing.T) {
	data := []byte(`{"model": "text-embedding-3-small", "input": ["hello", "world"]}`)
	var req EmbeddingsRequest
	err := json.Unmarshal(data, &req)
	if err != nil {
		t.Fatalf("should parse array input: %v", err)
	}
	arr, ok := req.Input.([]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("input should be []interface{} with 2 items, got %T", req.Input)
	}
}

func TestEmbeddingsRequest_Parse_EmptyModel(t *testing.T) {
	data := []byte(`{"model": "", "input": "hello"}`)
	var req EmbeddingsRequest
	err := json.Unmarshal(data, &req)
	if err != nil {
		t.Fatalf("empty model should parse: %v", err)
	}
	if req.Model != "" {
		t.Fatalf("model should be empty")
	}
}

func TestEmbeddingsRequest_Parse_InvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	var req EmbeddingsRequest
	err := json.Unmarshal(data, &req)
	if err == nil {
		t.Fatal("invalid JSON should error")
	}
}

// --- writeError status code edge cases ---

func TestWriteError_StatusCode200(t *testing.T) {
	// Unusual: writeError with 200 means success with error body.
	// Not in production but should not panic.
	rec := httptest.NewRecorder()
	writeError(rec, 200, "ok", "ok.success", "everything is fine")
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestWriteError_LargeMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	largeMsg := ""
	for i := 0; i < 10000; i++ {
		largeMsg += "x"
	}
	writeError(rec, 400, "test", "test.large", largeMsg)
	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	errObj := resp["error"].(map[string]interface{})
	if len(errObj["message"].(string)) != 10000 {
		t.Fatalf("message should be 10000 chars, got %d", len(errObj["message"].(string)))
	}
}

// --- estimateTokens with maxTokens=nil vs pointer to 0 ---

func TestEstimateTokens_NilVersusZeroMaxTokens(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "Hi"}}
	nilResult := estimateTokens(msgs, nil)
	zero := 0
	zeroResult := estimateTokens(msgs, &zero)
	// nil uses default 1000, zero explicitly means 0
	if nilResult <= zeroResult {
		t.Fatalf("nil (default 1000) should be > zero maxTokens: nil=%d, zero=%d", nilResult, zeroResult)
	}
}
