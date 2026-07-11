package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperationIdentitySeparatesClientCorrelationAndIdempotency(t *testing.T) {
	var captured RequestIdentity
	handler := OperationIdentityWithSecret("identity-test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetRequestIdentityFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(OperationIDHeader, "client-correlation-1")
	req.Header.Set(IdempotencyKeyHeader, "payment-retry-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if captured.OperationID == "" || !strings.HasPrefix(captured.OperationID, "op_") {
		t.Fatalf("expected server operation ID, got %q", captured.OperationID)
	}
	if captured.OperationID == "client-correlation-1" {
		t.Fatal("client correlation ID must not override the server operation ID")
	}
	if captured.ClientRequestID != "client-correlation-1" {
		t.Fatalf("expected client correlation ID, got %q", captured.ClientRequestID)
	}
	if !captured.IdempotencyKeyPresent || len(captured.IdempotencyKeyHash) != 64 {
		t.Fatalf("expected a hashed idempotency key, got %#v", captured)
	}
	if got := rec.Header().Get(OperationIDHeader); got != captured.OperationID {
		t.Fatalf("response operation ID mismatch: %q", got)
	}
	if got := rec.Header().Get(ClientRequestIDHeader); got != captured.ClientRequestID {
		t.Fatalf("response client correlation mismatch: %q", got)
	}
}

func TestOperationIdentityGeneratesUniqueOperationsForSharedClientIdentity(t *testing.T) {
	operationIDs := make(map[string]struct{}, 100)
	var expectedIdempotencyHash string
	handler := OperationIdentityWithSecret("identity-test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := GetRequestIdentityFromContext(r.Context())
		if identity.ClientRequestID != "shared-client-request" {
			t.Errorf("unexpected client ID %q", identity.ClientRequestID)
		}
		if expectedIdempotencyHash == "" {
			expectedIdempotencyHash = identity.IdempotencyKeyHash
		} else if identity.IdempotencyKeyHash != expectedIdempotencyHash {
			t.Errorf("same idempotency key produced a different hash")
		}
		operationIDs[identity.OperationID] = struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set(OperationIDHeader, "shared-client-request")
		req.Header.Set(IdempotencyKeyHeader, "shared-idempotency-key")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	if len(operationIDs) != 100 {
		t.Fatalf("expected 100 unique server operations, got %d", len(operationIDs))
	}
}

func TestOperationIdentityRejectsMalformedIdempotencyKey(t *testing.T) {
	called := false
	handler := OperationIdentityWithSecret("identity-test-secret")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(IdempotencyKeyHeader, strings.Repeat("x", 256))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("malformed idempotency key must fail before the handler")
	}
	if !strings.HasPrefix(rec.Header().Get(OperationIDHeader), "op_") {
		t.Fatal("rejected requests still need a server operation ID")
	}
}

func TestOperationIdentityKeepsOneOperationAcrossNestedMiddleware(t *testing.T) {
	var captured string
	handler := OperationIdentity(OperationIdentity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetRequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if captured == "" || captured != rec.Header().Get(OperationIDHeader) {
		t.Fatalf("nested middleware changed operation ID: captured=%q header=%q", captured, rec.Header().Get(OperationIDHeader))
	}
}

func TestIdempotencyKeyDigestIsKeyedAndDomainSeparated(t *testing.T) {
	first := hashIdempotencyKey("predictable-client-key", "secret-a")
	second := hashIdempotencyKey("predictable-client-key", "secret-b")
	if len(first) != 64 || len(second) != 64 || first == second {
		t.Fatalf("expected secret-specific HMAC digests, first=%q second=%q", first, second)
	}
	if strings.Contains(first, "predictable-client-key") {
		t.Fatal("raw idempotency key leaked into digest")
	}
}

func TestOperationIdentityRejectsConflictingClientCorrelationHeaders(t *testing.T) {
	called := false
	handler := OperationIdentityWithSecret("identity-test-secret")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(OperationIDHeader, "legacy-client-id")
	req.Header.Set(ClientRequestIDHeader, "explicit-client-id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || called {
		t.Fatalf("expected ambiguous client identity to fail before dispatch, status=%d called=%v", rec.Code, called)
	}
}

func TestOperationIdentityFailsClosedForUnkeyedIdempotencyDigest(t *testing.T) {
	called := false
	handler := OperationIdentity(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(IdempotencyKeyHeader, "must-not-use-plain-sha")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || called {
		t.Fatalf("expected missing HMAC secret to fail closed, status=%d called=%v", rec.Code, called)
	}
}
