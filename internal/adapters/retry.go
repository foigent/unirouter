package adapters

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
)

func classifyHTTPFailure(statusCode int) (FailureKind, bool) {
	switch statusCode {
	case 401, 403:
		return FailureAuth, false
	case 408:
		return FailureChannel, true
	case 429:
		return FailureRateLimit, true
	case 500, 502, 503, 504:
		return FailureChannel, true
	default:
		if statusCode >= 400 && statusCode < 500 {
			return FailureClientRequest, false
		}
		return FailureUnknown, false
	}
}

// ClassifyTransportError separates transient network failures from request
// cancellation and deterministic transport/configuration failures. Callers
// must only fail over when Retryable is true.
func ClassifyTransportError(ctx context.Context, cause error) *NormalizedError {
	normalized := &NormalizedError{
		Category:    "upstream_error",
		Code:        "upstream_transport_error",
		Message:     "upstream transport failed",
		FailureKind: FailureUnknown,
	}

	if ctx != nil && ctx.Err() != nil {
		normalized.Code = "request_context_done"
		normalized.Message = "request context ended"
		normalized.FailureKind = FailureClientRequest
		return normalized
	}
	if errors.Is(cause, context.Canceled) {
		normalized.Code = "request_canceled"
		normalized.Message = "request canceled"
		normalized.FailureKind = FailureClientRequest
		return normalized
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		normalized.Code = "upstream_timeout"
		normalized.Message = "upstream timed out"
		normalized.FailureKind = FailureChannel
		normalized.Retryable = true
		return normalized
	}

	if errors.Is(cause, io.EOF) || errors.Is(cause, io.ErrUnexpectedEOF) || errors.Is(cause, net.ErrClosed) ||
		errors.Is(cause, syscall.ECONNABORTED) || errors.Is(cause, syscall.ECONNREFUSED) ||
		errors.Is(cause, syscall.ECONNRESET) || errors.Is(cause, syscall.EPIPE) || errors.Is(cause, syscall.ETIMEDOUT) {
		normalized.Code = "upstream_connection_failure"
		normalized.Message = "upstream connection failed"
		normalized.FailureKind = FailureChannel
		normalized.Retryable = true
		return normalized
	}

	var netErr net.Error
	if errors.As(cause, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		normalized.Code = "upstream_network_failure"
		normalized.Message = "upstream network failed"
		normalized.FailureKind = FailureChannel
		normalized.Retryable = true
	}
	return normalized
}
