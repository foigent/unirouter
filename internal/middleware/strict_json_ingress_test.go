package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

func TestStrictJSONIngressRejectsDuplicateBeforeDispatchWithoutEcho(t *testing.T) {
	const secret = "private-payload-marker"
	dispatched := false
	handler := strictJSONIngressTestHandler(t, payloadpolicy.ProfileChatRequest, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		dispatched = true
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"one","messages":[{"content":"`+secret+`","content":"other"}]}`,
	))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || dispatched {
		t.Fatalf("status=%d dispatched=%v body=%s", recorder.Code, dispatched, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), secret) || !strings.Contains(recorder.Body.String(), `"code":"validation_error.invalid_request"`) {
		t.Fatalf("unsafe or incompatible error body: %s", recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Type    string  `json:"type"`
			Code    string  `json:"code"`
			Message string  `json:"message"`
			Param   *string `json:"param"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Type != "invalid_request_error" || envelope.Error.Message != "Invalid JSON body" || envelope.Error.Param != nil {
		t.Fatalf("unexpected OpenAI envelope: %+v", envelope.Error)
	}
}

func TestStrictJSONIngressUsesMessagesEnvelope(t *testing.T) {
	called := false
	handler := OperationIdentityWithSecret("strict-json-envelope-secret")(
		strictJSONIngressTestHandler(t, payloadpolicy.ProfileChatRequest, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		})),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"one","model":"two"}`))
	handler.ServeHTTP(recorder, request)
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
	operationID := recorder.Header().Get(OperationIDHeader)
	if called || recorder.Code != http.StatusBadRequest || body.Type != "error" ||
		body.Error.Type != "invalid_request_error" || body.RequestID != operationID ||
		recorder.Header().Get("request-id") != operationID {
		t.Fatalf("called=%t status=%d headers=%v body=%+v", called, recorder.Code, recorder.Header(), body)
	}
}

func TestStrictJSONIngressPreservesRawLargeIntegerAndProvidesCanonicalFingerprintBytes(t *testing.T) {
	body := []byte(` {"z":900719925474099312345678901234567890,"model":"gpt","future":{"n":1}} `)
	handler := strictJSONIngressTestHandler(t, payloadpolicy.ProfileChatRequest, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retained, ok := payloadpolicy.BodyFromContext(r.Context())
		if !ok || !bytes.Equal(retained, body) {
			t.Fatalf("raw body changed: %q", retained)
		}
		canonical, ok := strictJSONCanonicalBodyFromContext(r.Context())
		if !ok || string(canonical) != `{"future":{"n":1},"model":"gpt","z":900719925474099312345678901234567890}` {
			t.Fatalf("canonical body=%s ok=%v", canonical, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	request = request.WithContext(context.WithValue(request.Context(), requestIdentityContextKey{}, RequestIdentity{IdempotencyKeyPresent: true}))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestStrictJSONIngressFailsClosedWithoutRetainedPayload(t *testing.T) {
	called := false
	handler := StrictJSONIngress(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt"}`)))
	if recorder.Code != http.StatusInternalServerError || called {
		t.Fatalf("status=%d called=%v body=%s", recorder.Code, called, recorder.Body.String())
	}
}

func strictJSONIngressTestHandler(t *testing.T, profile string, next http.Handler) http.Handler {
	t.Helper()
	budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
	retain := payloadpolicy.RequestMiddleware(budget, profile, func(context.Context) int64 { return 1 })
	return retain(StrictJSONIngress(next))
}
