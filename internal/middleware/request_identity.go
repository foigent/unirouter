package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/uniroute/unirouter/internal/errdocs"
)

const (
	OperationIDHeader       = "X-Request-ID"
	ClientRequestIDHeader   = "X-UniRouter-Client-Request-ID"
	OriginalOperationHeader = "X-UniRouter-Original-Request-ID"
	IdempotencyKeyHeader    = "Idempotency-Key"
)

// RequestIdentity separates the server-owned operation identity from caller
// correlation and financial idempotency. OperationID remains exposed as
// request_id in legacy database/API fields so existing operator workflows keep
// one canonical lookup value.
type RequestIdentity struct {
	OperationID           string
	ClientRequestID       string
	IdempotencyKeyHash    string
	IdempotencyKeyPresent bool
}

type requestIdentityContextKey struct{}

// OperationIdentity assigns an unoverrideable server operation ID before the
// request enters authentication, routing, billing, audit, or error handling.
func OperationIdentity(next http.Handler) http.Handler {
	return OperationIdentityWithSecret("")(next)
}

// OperationIdentityWithSecret assigns the server identity and derives a
// domain-separated keyed digest when the caller supplies Idempotency-Key.
// Production wiring must provide a non-empty secret; requests without an
// idempotency key remain available to health and other public middleware.
func OperationIdentityWithSecret(idempotencyHashSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identifiedRequest, ok := ensureRequestIdentity(w, r, idempotencyHashSecret)
			if !ok {
				return
			}
			next.ServeHTTP(w, identifiedRequest)
		})
	}
}

func ensureRequestIdentity(w http.ResponseWriter, r *http.Request, idempotencyHashSecret string) (*http.Request, bool) {
	if existing := GetRequestIdentityFromContext(r.Context()); existing.OperationID != "" {
		writeRequestIdentityHeaders(w, existing)
		return r, true
	}

	operationID, err := newOperationID()
	if err != nil {
		writeRequestIdentityError(w, http.StatusInternalServerError, "server_error.internal_error", "Failed to generate request identity")
		return r, false
	}
	identity := RequestIdentity{OperationID: operationID}
	writeRequestIdentityHeaders(w, identity)

	rawClientID := strings.TrimSpace(r.Header.Get(ClientRequestIDHeader))
	legacyClientID := strings.TrimSpace(r.Header.Get(OperationIDHeader))
	if rawClientID != "" && legacyClientID != "" && rawClientID != legacyClientID {
		writeRequestIdentityError(w, http.StatusBadRequest, "validation_error.invalid_request", "Conflicting client request identity headers")
		return r, false
	}
	if rawClientID == "" {
		rawClientID = legacyClientID
	}
	if rawClientID != "" {
		clientID, valid := sanitizeIdentityValue(rawClientID, 128)
		if !valid {
			writeRequestIdentityError(w, http.StatusBadRequest, "validation_error.invalid_request", "Invalid client request identity header")
			return r, false
		}
		identity.ClientRequestID = clientID
	}

	if rawIdempotencyKey := r.Header.Get(IdempotencyKeyHeader); strings.TrimSpace(rawIdempotencyKey) != "" {
		idempotencyKey, valid := sanitizeIdentityValue(rawIdempotencyKey, 255)
		if !valid {
			writeRequestIdentityError(w, http.StatusBadRequest, "validation_error.invalid_request", "Invalid Idempotency-Key header")
			return r, false
		}
		if strings.TrimSpace(idempotencyHashSecret) == "" {
			writeRequestIdentityError(w, http.StatusInternalServerError, "server_error.internal_error", "Idempotency hashing is not configured")
			return r, false
		}
		identity.IdempotencyKeyPresent = true
		identity.IdempotencyKeyHash = hashIdempotencyKey(idempotencyKey, idempotencyHashSecret)
	}

	writeRequestIdentityHeaders(w, identity)
	ctx := context.WithValue(r.Context(), requestIdentityContextKey{}, identity)
	ctx = context.WithValue(ctx, CtxRequestID, identity.OperationID)
	return r.WithContext(ctx), true
}

func GetRequestIdentityFromContext(ctx context.Context) RequestIdentity {
	if ctx == nil {
		return RequestIdentity{}
	}
	identity, _ := ctx.Value(requestIdentityContextKey{}).(RequestIdentity)
	if identity.OperationID == "" {
		identity.OperationID = GetRequestIDFromContext(ctx)
	}
	return identity
}

func GetClientRequestIDFromContext(ctx context.Context) string {
	return GetRequestIdentityFromContext(ctx).ClientRequestID
}

func GetIdempotencyKeyHashFromContext(ctx context.Context) string {
	return GetRequestIdentityFromContext(ctx).IdempotencyKeyHash
}

func writeRequestIdentityHeaders(w http.ResponseWriter, identity RequestIdentity) {
	if identity.OperationID != "" {
		w.Header().Set(OperationIDHeader, identity.OperationID)
	}
	if identity.ClientRequestID != "" {
		w.Header().Set(ClientRequestIDHeader, identity.ClientRequestID)
	}
}

func newOperationID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "op_" + hex.EncodeToString(random), nil
}

// NewOperationID returns a cryptographically random server operation identity.
func NewOperationID() (string, error) {
	return newOperationID()
}

func hashIdempotencyKey(key, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("unirouter.idempotency-key.v1\x00"))
	_, _ = mac.Write([]byte(key))
	return hex.EncodeToString(mac.Sum(nil))
}

func sanitizeIdentityValue(value string, maxLen int) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxLen {
		return "", false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 33 || value[i] > 126 {
			return "", false
		}
	}
	return value, true
}

func writeRequestIdentityError(w http.ResponseWriter, status int, code, message string) {
	errType := "invalid_request_error"
	if status >= http.StatusInternalServerError {
		errType = "server_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"type":    errType,
			"code":    code,
			"message": message,
			"doc_url": errdocs.URLForCode(code),
			"param":   nil,
		},
	})
}
