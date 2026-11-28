package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/uniroute/unirouter/internal/gatewayerror"
)

// ChatUpstreamAttemptsHeader names the opt-in shared by Chat, native and media admission.
const ChatUpstreamAttemptsHeader = "X-UniRouter-Max-Upstream-Attempts"

type singleUpstreamAttemptKey struct{}

func SingleUpstreamAttemptRequested(ctx context.Context) bool {
	single, _ := ctx.Value(singleUpstreamAttemptKey{}).(bool)
	return single
}

// ReadSingleUpstreamAttempt accepts one exact value and rejects ambiguous headers.
func ReadSingleUpstreamAttempt(headers http.Header) (bool, error) {
	values := headers.Values(ChatUpstreamAttemptsHeader)
	if len(values) == 0 {
		return false, nil
	}
	if len(values) != 1 || values[0] != "1" {
		return false, errors.New(ChatUpstreamAttemptsHeader + " must be 1")
	}
	return true, nil
}

// UpstreamAttemptPolicy records the opt-in before the idempotency claim.
// Echoing the control lets a caller distinguish a supporting preflight from
// an older installation that ignores unknown request headers.
func UpstreamAttemptPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		single, err := ReadSingleUpstreamAttempt(r.Header)
		if err != nil {
			gatewayerror.WriteRequest(w, r, http.StatusBadRequest, "validation_error.invalid_request", err.Error())
			return
		}
		if !single {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set(ChatUpstreamAttemptsHeader, "1")
		ctx := context.WithValue(r.Context(), singleUpstreamAttemptKey{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
