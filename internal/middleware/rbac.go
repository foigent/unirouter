package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/uniroute/unirouter/internal/auth"
)

// RequireRoles gates an authenticated admin route by normalized PRD2 role.
// It assumes AdminAuth has already injected CtxUserRole into the request context.
func RequireRoles(allowed ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := GetUserRoleFromContext(r.Context())
			if !auth.RoleIn(role, allowed...) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, `{"error":{"type":"auth_error.forbidden","message":"Role %s is not allowed","allowed_roles":"%s"}}`, role, strings.Join(allowed, ","))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
