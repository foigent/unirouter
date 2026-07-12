package middleware

import (
	"net/http"
	"strings"
)

type CORSOptions struct {
	AllowedOrigins []string
}

// CORS returns strict CORS middleware with no cross-origin allowlist.
func CORS(next http.Handler) http.Handler {
	return CORSWithOptions(CORSOptions{})(next)
}

func CORSWithOptions(options CORSOptions) func(http.Handler) http.Handler {
	allowed := normalizeAllowedOrigins(options.AllowedOrigins)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				w.Header().Add("Vary", "Origin")
				if allowOrigin(origin, allowed) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, X-UniRouter-Client-Request-ID, Idempotency-Key, Traceparent")
					w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-UniRouter-Client-Request-ID, X-UniRouter-Original-Request-ID, X-UniRouter-Limit-Policy-Version")
					w.Header().Set("Access-Control-Max-Age", "86400")
				} else if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func normalizeAllowedOrigins(origins []string) map[string]bool {
	allowed := map[string]bool{}
	for _, origin := range origins {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin == "" {
			continue
		}
		allowed[origin] = true
	}
	return allowed
}

func allowOrigin(origin string, allowed map[string]bool) bool {
	return allowed[strings.TrimRight(origin, "/")]
}
