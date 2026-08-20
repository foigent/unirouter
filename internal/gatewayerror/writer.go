// Package gatewayerror owns the protocol-aware public error envelope used at
// model-gateway request boundaries. It does not publish routes or classify
// provider payloads.
package gatewayerror

import (
	"encoding/json"
	"net/http"

	"github.com/uniroute/unirouter/internal/errdocs"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

// Write emits the error envelope declared by the canonical model operation.
// Unknown paths retain the established OpenAI-compatible shape rather than
// acquiring Anthropic semantics through inference.
func Write(w http.ResponseWriter, path, requestID string, status int, code, message string) {
	definition, known := modeloperation.Lookup(modeloperation.CanonicalPath(path))
	if known && definition.ErrorEnvelope == modeloperation.ErrorEnvelopeAnthropicV1 {
		writeAnthropic(w, requestID, status, message)
		return
	}
	writeOpenAI(w, status, code, message)
}

// WriteRequest derives protocol identity from the canonical request path and
// the server-owned operation ID already placed on the response by the outer
// OperationIdentity middleware. It never trusts a caller-supplied request
// header for the Anthropic request_id field.
func WriteRequest(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	path := ""
	if r != nil && r.URL != nil {
		path = r.URL.Path
	}
	Write(w, path, w.Header().Get("X-Request-ID"), status, code, message)
}

func writeAnthropic(w http.ResponseWriter, requestID string, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("request-id", requestID)
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}{
		Type: "error",
		Error: struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}{
			Type:    anthropicErrorType(status),
			Message: message,
		},
		RequestID: requestID,
	})
}

func anthropicErrorType(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case 529:
		return "overloaded_error"
	default:
		if status >= http.StatusInternalServerError {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

func writeOpenAI(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"type":    openAIErrorType(status),
			"code":    code,
			"message": message,
			"doc_url": errdocs.URLForCode(code),
			"param":   nil,
		},
	})
}

func openAIErrorType(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	}
	if status >= http.StatusInternalServerError {
		return "server_error"
	}
	return "invalid_request_error"
}
