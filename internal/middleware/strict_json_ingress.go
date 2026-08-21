package middleware

import (
	"context"
	"net/http"

	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/strictjson"
)

type strictJSONCanonicalContextKey struct{}

// StrictJSONIngress validates the retained request document after live
// authentication and before idempotency, governance, billing, routing, or
// provider dispatch. It deliberately does not decode a protocol DTO: unknown
// fields and the original RawMessage bytes remain owned by the endpoint.
//
// The body must already have passed payloadpolicy.RequestMiddleware. Failing
// closed here prevents future wiring from silently parsing unaccounted bytes.
func StrictJSONIngress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := payloadpolicy.BodyFromContext(r.Context())
		if !ok {
			writeRequestIdentityError(w, r, http.StatusInternalServerError, "server_error.internal_error", "JSON request admission is unavailable")
			return
		}
		var canonical []byte
		var err error
		if GetRequestIdentityFromContext(r.Context()).IdempotencyKeyPresent {
			canonical, err = strictjson.Canonicalize(body)
		} else {
			err = strictjson.Validate(body)
		}
		if err != nil {
			writeRequestIdentityError(w, r, http.StatusBadRequest, "validation_error.invalid_request", "Invalid JSON body")
			return
		}
		ctx := r.Context()
		if len(canonical) > 0 {
			ctx = context.WithValue(ctx, strictJSONCanonicalContextKey{}, canonical)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func strictJSONCanonicalBodyFromContext(ctx context.Context) ([]byte, bool) {
	if ctx == nil {
		return nil, false
	}
	canonical, ok := ctx.Value(strictJSONCanonicalContextKey{}).([]byte)
	if !ok || len(canonical) == 0 {
		return nil, false
	}
	return canonical, true
}
