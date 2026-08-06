package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/uniroute/unirouter/internal/auth"
)

// Deprecated: compatibility-only test helper for pre-v3 route harnesses.
// Production routes must use RequireOrganizationRoles for organization scope
// or AdminAuth plus a live project_members check for project scope.
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

// RequireOrganizationRoles gates an authenticated admin route by the
// canonical account-level organization role. Project authority is deliberately
// excluded: project handlers must authorize against the live project_members
// row for the requested project.
func RequireOrganizationRoles(allowed ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := GetOrganizationRoleFromContext(r.Context())
			if role == "" {
				// Compatibility for direct middleware tests and accepted v2 JWTs.
				role, _ = auth.NormalizeOrganizationRole(GetUserRoleFromContext(r.Context()))
			}
			if !auth.RoleIn(role, allowed...) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, `{"error":{"type":"auth_error.forbidden","message":"Organization role %s is not allowed","allowed_roles":"%s"}}`, role, strings.Join(allowed, ","))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Deprecated: compatibility-only test helper for frozen stored-role fixtures.
// Production authorization must not use users.role as an authority source.
func RequireExactRoles(allowed ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := GetUserRoleFromContext(r.Context())
			for _, candidate := range allowed {
				if role == candidate {
					next.ServeHTTP(w, r)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, `{"error":{"type":"auth_error.forbidden","message":"Role %s is not allowed","allowed_roles":"%s"}}`, role, strings.Join(allowed, ","))
		})
	}
}
