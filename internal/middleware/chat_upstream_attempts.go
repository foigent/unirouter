package middleware

import (
	"context"
	"net/http"

	"github.com/uniroute/unirouter/internal/gatewayerror"
)

// ChatUpstreamAttemptsHeader is accepted only by Chat and its preflight.
const ChatUpstreamAttemptsHeader = "X-UniRouter-Max-Upstream-Attempts"

type singleChatUpstreamAttemptKey struct{}

func SingleChatUpstreamAttemptRequested(ctx context.Context) bool {
	single, _ := ctx.Value(singleChatUpstreamAttemptKey{}).(bool)
	return single
}

// ChatUpstreamAttemptPolicy records the opt-in before the idempotency claim.
// Echoing the control lets a caller distinguish a supporting preflight from
// an older installation that ignores unknown request headers.
func ChatUpstreamAttemptPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values(ChatUpstreamAttemptsHeader)
		if len(values) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		if len(values) != 1 || values[0] != "1" {
			gatewayerror.WriteRequest(w, r, http.StatusBadRequest, "validation_error.invalid_request", ChatUpstreamAttemptsHeader+" must be 1")
			return
		}
		w.Header().Set(ChatUpstreamAttemptsHeader, "1")
		ctx := context.WithValue(r.Context(), singleChatUpstreamAttemptKey{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
