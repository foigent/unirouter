package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestMessagesCredentialAndVersionFailuresAreAnthropicAndPreDB(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(http.Header)
		status    int
		errorType string
	}{
		{
			name: "missing x-api-key",
			mutate: func(header http.Header) {
				header.Del("x-api-key")
			},
			status: http.StatusUnauthorized, errorType: "authentication_error",
		},
		{
			name: "empty x-api-key",
			mutate: func(header http.Header) {
				header["X-Api-Key"] = []string{""}
			},
			status: http.StatusUnauthorized, errorType: "authentication_error",
		},
		{
			name: "authorization coexists",
			mutate: func(header http.Header) {
				header.Set("Authorization", "Bearer do-not-accept-this-secret")
			},
			status: http.StatusUnauthorized, errorType: "authentication_error",
		},
		{
			name: "multiple x-api-key values",
			mutate: func(header http.Header) {
				header.Add("x-api-key", "second-secret")
			},
			status: http.StatusUnauthorized, errorType: "authentication_error",
		},
		{
			name: "comma-ambiguous x-api-key",
			mutate: func(header http.Header) {
				header.Set("x-api-key", "first-secret,second-secret")
			},
			status: http.StatusUnauthorized, errorType: "authentication_error",
		},
		{
			name: "whitespace-padded x-api-key",
			mutate: func(header http.Header) {
				header.Set("x-api-key", " messages-top-secret ")
			},
			status: http.StatusUnauthorized, errorType: "authentication_error",
		},
		{
			name: "missing anthropic-version",
			mutate: func(header http.Header) {
				header.Del("anthropic-version")
			},
			status: http.StatusBadRequest, errorType: "invalid_request_error",
		},
		{
			name: "wrong anthropic-version",
			mutate: func(header http.Header) {
				header.Set("anthropic-version", "2024-01-01")
			},
			status: http.StatusBadRequest, errorType: "invalid_request_error",
		},
		{
			name: "multiple anthropic-version values",
			mutate: func(header http.Header) {
				header.Add("anthropic-version", anthropicMessagesVersion)
			},
			status: http.StatusBadRequest, errorType: "invalid_request_error",
		},
		{
			name: "anthropic-beta present",
			mutate: func(header http.Header) {
				header["Anthropic-Beta"] = []string{""}
			},
			status: http.StatusBadRequest, errorType: "invalid_request_error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := unexpectedGatewayProtocolDB(t)
			called := false
			handler := GatewayAuthWithAgentCapabilitiesAndClientIP(database, "hash-secret", nil, ClientIPResolver{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))
			request := httptest.NewRequest(http.MethodPost, modeloperation.PathMessages, nil)
			request.Header.Set("x-api-key", "messages-top-secret")
			request.Header.Set("anthropic-version", anthropicMessagesVersion)
			test.mutate(request.Header)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if called {
				t.Fatal("invalid protocol headers reached the protected handler")
			}
			assertAnthropicGatewayError(t, recorder, test.status, test.errorType)
			assertNoSecrets(t, recorder.Body.String(), "messages-top-secret", "do-not-accept-this-secret", "first-secret", "second-secret")
		})
	}
}

func TestMessagesValidHeadersEnterExistingAuthentication(t *testing.T) {
	queries := 0
	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		queries++
		return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
	}}
	handler := GatewayAuthWithAgentCapabilitiesAndClientIP(database, "hash-secret", nil, ClientIPResolver{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid fixture credential reached protected handler")
	}))
	request := httptest.NewRequest(http.MethodPost, modeloperation.PathMessages, nil)
	request.Header.Set("x-api-key", "messages-valid-shape-secret")
	request.Header.Set("anthropic-version", anthropicMessagesVersion)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if queries != 1 {
		t.Fatalf("valid Messages headers produced %d DB queries, want 1", queries)
	}
	assertAnthropicGatewayError(t, recorder, http.StatusUnauthorized, "authentication_error")
	assertNoSecrets(t, recorder.Body.String(), "messages-valid-shape-secret")
}

func TestMessagesAgentCapabilityUsesXAPIKeyCredentialSlot(t *testing.T) {
	database := unexpectedGatewayProtocolDB(t)
	handler := GatewayAuthWithAgentCapabilitiesAndClientIP(database, "hash-secret", nil, ClientIPResolver{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unconfigured Agent capability reached protected handler")
	}))
	request := httptest.NewRequest(http.MethodPost, modeloperation.PathMessages, nil)
	request.Header.Set("x-api-key", "urc_agent-capability-secret")
	request.Header.Set("anthropic-version", anthropicMessagesVersion)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertAnthropicGatewayError(t, recorder, http.StatusUnauthorized, "authentication_error")
	assertNoSecrets(t, recorder.Body.String(), "urc_agent-capability-secret")
}

func TestOpenAIOperationsRejectXAPIKeyMixingBeforeDB(t *testing.T) {
	for _, path := range []string{
		modeloperation.PathChatCompletions,
		modeloperation.PathEmbeddings,
		modeloperation.PathResponses,
		modeloperation.PathModels,
	} {
		t.Run(path, func(t *testing.T) {
			database := unexpectedGatewayProtocolDB(t)
			handler := GatewayAuthWithAgentCapabilitiesAndClientIP(database, "hash-secret", nil, ClientIPResolver{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("mixed credentials reached protected handler")
			}))
			request := httptest.NewRequest(http.MethodPost, path, nil)
			request.Header.Set("Authorization", "Bearer openai-top-secret")
			request.Header.Set("x-api-key", "foreign-top-secret")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertOpenAIGatewayError(t, recorder, http.StatusUnauthorized, "auth_error.invalid_api_key")
			assertNoSecrets(t, recorder.Body.String(), "openai-top-secret", "foreign-top-secret")
		})
	}
}

func TestResponsesBearerAndProtocolHeadersAreStrictBeforeDB(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(http.Header)
		status int
	}{
		{name: "missing bearer", mutate: func(header http.Header) { header.Del("Authorization") }, status: http.StatusUnauthorized},
		{name: "empty bearer", mutate: func(header http.Header) { header.Set("Authorization", "Bearer ") }, status: http.StatusUnauthorized},
		{name: "wrong scheme", mutate: func(header http.Header) { header.Set("Authorization", "bearer response-secret") }, status: http.StatusUnauthorized},
		{name: "whitespace padded", mutate: func(header http.Header) { header.Set("Authorization", "Bearer  response-secret ") }, status: http.StatusUnauthorized},
		{name: "multiple", mutate: func(header http.Header) { header.Add("Authorization", "Bearer other-secret") }, status: http.StatusUnauthorized},
		{name: "comma ambiguous", mutate: func(header http.Header) { header.Set("Authorization", "Bearer response-secret,Bearer other-secret") }, status: http.StatusUnauthorized},
		{name: "anthropic version", mutate: func(header http.Header) { header.Set("anthropic-version", anthropicMessagesVersion) }, status: http.StatusBadRequest},
		{name: "anthropic beta", mutate: func(header http.Header) { header.Set("anthropic-beta", "future") }, status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := unexpectedGatewayProtocolDB(t)
			handler := GatewayAuthWithAgentCapabilitiesAndClientIP(database, "hash-secret", nil, ClientIPResolver{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("invalid Responses protocol headers reached handler")
			}))
			request := httptest.NewRequest(http.MethodPost, modeloperation.PathResponses, nil)
			request.Header.Set("Authorization", "Bearer response-secret")
			test.mutate(request.Header)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertOpenAIGatewayError(t, recorder, test.status, map[int]string{
				http.StatusUnauthorized: "auth_error.invalid_api_key",
				http.StatusBadRequest:   "validation_error.invalid_request",
			}[test.status])
			assertNoSecrets(t, recorder.Body.String(), "response-secret", "other-secret")
		})
	}
}

func TestResponsesValidBearerShapeEntersExistingAuthentication(t *testing.T) {
	queries := 0
	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		queries++
		return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
	}}
	handler := GatewayAuthWithAgentCapabilitiesAndClientIP(database, "hash-secret", nil, ClientIPResolver{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid fixture credential reached protected handler")
	}))
	request := httptest.NewRequest(http.MethodPost, modeloperation.PathResponses, nil)
	request.Header.Set("Authorization", "Bearer responses-valid-shape-secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if queries != 1 {
		t.Fatalf("valid Responses headers produced %d DB queries, want 1", queries)
	}
	assertOpenAIGatewayError(t, recorder, http.StatusUnauthorized, "auth_error.invalid_api_key")
	assertNoSecrets(t, recorder.Body.String(), "responses-valid-shape-secret")
}

func TestChatBearerCompatibilityStillEntersExistingAuthentication(t *testing.T) {
	queries := 0
	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		queries++
		return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
	}}
	handler := GatewayAuthWithAgentCapabilitiesAndClientIP(database, "hash-secret", nil, ClientIPResolver{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid fixture credential reached protected handler")
	}))
	request := httptest.NewRequest(http.MethodPost, modeloperation.PathChatCompletions, nil)
	request.Header.Set("Authorization", "Bearer openai-valid-shape-secret")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if queries != 1 {
		t.Fatalf("valid Chat headers produced %d DB queries, want 1", queries)
	}
	assertOpenAIGatewayError(t, recorder, http.StatusUnauthorized, "auth_error.invalid_api_key")
	assertNoSecrets(t, recorder.Body.String(), "openai-valid-shape-secret")
}

func TestGatewayRequestAdmissionErrorsUseMessagesEnvelope(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
		type_  string
	}{
		{http.StatusForbidden, "auth_error.access_grant_endpoint_not_allowed", "permission_error"},
		{http.StatusConflict, "billing_error.provider_cost_pending", "invalid_request_error"},
		{http.StatusServiceUnavailable, "billing_error.provider_cost_state_unavailable", "api_error"},
		{http.StatusForbidden, "auth_error.ip_not_allowed", "permission_error"},
	} {
		request := httptest.NewRequest(http.MethodPost, modeloperation.PathMessages, nil)
		recorder := httptest.NewRecorder()
		writeGatewayRequestError(recorder, request, test.status, test.code, "Safe admission failure")
		assertAnthropicGatewayError(t, recorder, test.status, test.type_)
	}
}

func unexpectedGatewayProtocolDB(t *testing.T) *testutil.MockDB {
	t.Helper()
	return &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		t.Fatal("protocol/header rejection performed a DB query")
		return &testutil.MockRow{}
	}}
}

func assertAnthropicGatewayError(t *testing.T, recorder *httptest.ResponseRecorder, status int, errorType string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, status, recorder.Body.String())
	}
	requestID := recorder.Header().Get("request-id")
	if requestID == "" || recorder.Header().Get("X-Request-ID") != requestID {
		t.Fatalf("request ID headers=%v", recorder.Header())
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
		t.Fatalf("decode Anthropic error: %v body=%s", err, recorder.Body.String())
	}
	if body.Type != "error" || body.Error.Type != errorType || body.Error.Message == "" || body.RequestID != requestID {
		t.Fatalf("Anthropic body=%+v headers=%v", body, recorder.Header())
	}
}

func assertOpenAIGatewayError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, status, recorder.Body.String())
	}
	var body map[string]map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode OpenAI error: %v body=%s", err, recorder.Body.String())
	}
	if body["error"]["code"] != code || body["error"]["message"] == "" || body["error"]["doc_url"] == "" {
		t.Fatalf("OpenAI body=%v", body)
	}
}

func assertNoSecrets(t *testing.T, body string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(body, secret) {
			t.Fatalf("response leaked credential %q: %s", secret, body)
		}
	}
}
