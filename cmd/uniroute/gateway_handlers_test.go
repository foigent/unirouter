package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

func TestRewriteGovernedModelPreservesAliasRequestEnvelope(t *testing.T) {
	body := []byte(`{"model":"company-chat","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto","parallel_tool_calls":true,"response_format":{"type":"json_object"},"stream_options":{"include_usage":true},"provider_counter":9223372036854775807,"provider_extension":{"keep":true}}`)
	var original map[string]json.RawMessage
	if err := json.Unmarshal(body, &original); err != nil {
		t.Fatal(err)
	}

	rewritten, err := rewriteGovernedModel(body, "gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rewritten, &payload); err != nil {
		t.Fatal(err)
	}
	var model string
	if err := json.Unmarshal(payload["model"], &model); err != nil || model != "gpt-4o" {
		t.Fatalf("model=%q err=%v body=%s", model, err, rewritten)
	}
	for _, field := range []string{
		"messages", "tools", "tool_choice", "parallel_tool_calls", "response_format",
		"stream_options", "provider_counter", "provider_extension",
	} {
		if !bytes.Equal(payload[field], original[field]) {
			t.Fatalf("field %q changed: before=%s after=%s", field, original[field], payload[field])
		}
	}
	if string(payload["provider_counter"]) != "9223372036854775807" {
		t.Fatalf("provider counter lost precision: %s", payload["provider_counter"])
	}
}

func TestAPIKeyAllowsOnlyExactRequestedPublicModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		apiKey    *models.ApiKey
		requested string
		want      bool
	}{
		{name: "nil key", requested: "public-chat", want: false},
		{name: "fail closed scope", apiKey: &models.ApiKey{ModelScopeDenied: true}, requested: "public-chat", want: false},
		{name: "blank legacy scope", apiKey: &models.ApiKey{}, requested: "public-chat", want: true},
		{name: "wildcard", apiKey: &models.ApiKey{AllowedModels: "*"}, requested: "public-chat", want: true},
		{name: "exact", apiKey: &models.ApiKey{AllowedModels: "public-chat"}, requested: "public-chat", want: true},
		{name: "historical separators", apiKey: &models.ApiKey{AllowedModels: "other；public-chat\nthird"}, requested: "public-chat", want: true},
		{name: "case sensitive", apiKey: &models.ApiKey{AllowedModels: "Public-Chat"}, requested: "public-chat", want: false},
		{name: "substring rejected", apiKey: &models.ApiKey{AllowedModels: "public-chat-large"}, requested: "public-chat", want: false},
		{name: "blank request rejected", apiKey: &models.ApiKey{AllowedModels: "*"}, requested: "  ", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := apiKeyAllowsRequestedModel(tc.apiKey, tc.requested); got != tc.want {
				t.Fatalf("apiKeyAllowsRequestedModel() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestPrepareGatewayModelRequestRejectsScopeBeforeGovernance(t *testing.T) {
	t.Parallel()
	const requestedModel = "private-catalog-marker"
	const allowedModel = "allowed-scope-marker"
	body := `{"model":"` + requestedModel + `","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.CtxAPIKey, &models.ApiKey{
		ID:            17,
		OrgID:         23,
		AllowedModels: allowedModel,
	}))
	recorder := httptest.NewRecorder()

	governed, ok := prepareGatewayModelRequest(recorder, req, nil, nil, nil)
	if ok || governed != nil {
		t.Fatalf("prepareGatewayModelRequest() = (%v, %t), want denied", governed, ok)
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if version := recorder.Header().Get("X-UniRouter-Limit-Policy-Version"); version != "" {
		t.Fatalf("scope denial leaked limit-policy topology: %q", version)
	}
	response := recorder.Body.String()
	if strings.Contains(response, requestedModel) || strings.Contains(response, allowedModel) {
		t.Fatalf("scope denial leaked model identity: %s", response)
	}
	if !strings.Contains(response, "auth_error.model_not_allowed") {
		t.Fatalf("scope denial missing stable error code: %s", response)
	}
}

func TestPrepareGatewayPublicModelRequestRequiresRelayBeforeGovernance(t *testing.T) {
	t.Parallel()
	body := `{"model":"public-messages","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	request = request.WithContext(context.WithValue(request.Context(), middleware.CtxAPIKey, &models.ApiKey{
		ID:            31,
		OrgID:         41,
		AllowedModels: "public-messages",
	}))
	request = request.WithContext(context.WithValue(request.Context(), middleware.CtxRequestID, "op_public_runtime_missing"))
	recorder := httptest.NewRecorder()
	recorder.Header().Set("X-Request-ID", "op_public_runtime_missing")

	guard := payloadpolicy.RequestMiddlewareWithMax(
		payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()),
		8*payloadpolicy.MiB,
		nil,
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		governed, ok := prepareGatewayPublicModelRequest(w, r, nil, nil, nil)
		if ok || governed != nil {
			t.Fatalf("prepareGatewayPublicModelRequest() = (%v, %t), want fail closed", governed, ok)
		}
	}))
	guard.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"type":"api_error"`) ||
		!strings.Contains(recorder.Body.String(), `"request_id":"op_public_runtime_missing"`) {
		t.Fatalf("Messages runtime fence used wrong envelope: %s", recorder.Body.String())
	}
}
