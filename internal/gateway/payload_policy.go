package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

const nonStreamResponseExtensionReserveBytes = int64(64 * 1024)

const nonStreamResponseExtensionPrefix = `,"_uniroute":`

func (relay *Relay) requestPayload(r *http.Request, profile string) ([]byte, func(), error) {
	if r == nil {
		return nil, func() {}, io.ErrUnexpectedEOF
	}
	if body, ok := payloadpolicy.BodyFromContext(r.Context()); ok {
		return body, func() {}, nil
	}
	maxBytes, err := relay.currentPayloadBudget().Limits().MaxBytes(profile)
	if err != nil {
		return nil, func() {}, err
	}
	body, lease, err := relay.currentPayloadBudget().ReadAll(r.Body, middleware.GetOrgIDFromContext(r.Context()), maxBytes)
	if err != nil {
		return nil, func() {}, err
	}
	return body, lease.Release, nil
}

func (relay *Relay) responsePayload(ctx context.Context, reader io.Reader) ([]byte, func(), error) {
	maxBytes, err := relay.currentPayloadBudget().Limits().MaxBytes(payloadpolicy.ProfileNonStreamResponse)
	if err != nil {
		return nil, func() {}, err
	}
	body, lease, err := relay.currentPayloadBudget().ReadAll(reader, middleware.GetOrgIDFromContext(ctx), maxBytes)
	if err != nil {
		return nil, func() {}, err
	}
	return body, lease.Release, nil
}

// marshalNonStreamResponseBase encodes the policy-transformed response once,
// before billing delivery preparation, and reserves bounded room for the
// server-owned _uniroute extension. This makes the configured response limit
// authoritative for the final downstream envelope rather than only the raw
// provider body.
func (relay *Relay) marshalNonStreamResponseBase(response map[string]interface{}) ([]byte, int64, error) {
	if response == nil {
		return nil, 0, fmt.Errorf("response envelope is unavailable")
	}
	delete(response, "_uniroute")
	maxBytes, err := relay.currentPayloadBudget().Limits().MaxBytes(payloadpolicy.ProfileNonStreamResponse)
	if err != nil {
		return nil, 0, err
	}
	reserve := nonStreamResponseExtensionReserve(maxBytes)
	base, err := json.Marshal(response)
	if err != nil {
		return nil, 0, err
	}
	if int64(len(base)) > maxBytes-reserve {
		return nil, maxBytes, payloadpolicy.ErrPayloadTooLarge
	}
	return base, maxBytes, nil
}

func appendNonStreamResponseExtension(base []byte, extension map[string]interface{}, maxBytes int64) ([]byte, error) {
	if len(base) < 2 || base[len(base)-1] != '}' || maxBytes <= 0 {
		return nil, fmt.Errorf("response base envelope is invalid")
	}
	extensionJSON, err := json.Marshal(extension)
	if err != nil {
		return nil, err
	}
	reserve := nonStreamResponseExtensionReserve(maxBytes)
	if int64(len(nonStreamResponseExtensionPrefix)+len(extensionJSON)+1) > reserve {
		return nil, fmt.Errorf("response extension exceeds its internal bound")
	}
	var payload bytes.Buffer
	payload.Grow(len(base) + len(extensionJSON) + 16)
	payload.Write(base[:len(base)-1])
	payload.WriteString(nonStreamResponseExtensionPrefix)
	payload.Write(extensionJSON)
	payload.WriteString("}\n")
	if int64(payload.Len()) > maxBytes {
		return nil, payloadpolicy.ErrPayloadTooLarge
	}
	return payload.Bytes(), nil
}

func nonStreamResponseExtensionReserve(maxBytes int64) int64 {
	reserve := nonStreamResponseExtensionReserveBytes
	if reserve >= maxBytes {
		reserve = maxBytes / 4
	}
	return reserve
}

func payloadReadFailure(err error) (status int, errorType, errorCode, message, failureKind string) {
	switch {
	case errors.Is(err, payloadpolicy.ErrPayloadTooLarge):
		return http.StatusBadGateway, "server_error", "server_error.upstream_response_too_large", "Upstream response exceeds the non-stream safety limit", "response_too_large"
	case errors.Is(err, payloadpolicy.ErrBudgetExceeded):
		return http.StatusServiceUnavailable, "server_error", "server_error.payload_capacity_exceeded", "Payload capacity is temporarily unavailable", "payload_capacity"
	default:
		return http.StatusInternalServerError, "server_error", "server_error.internal_error", "Failed to read upstream response", "response_read"
	}
}
