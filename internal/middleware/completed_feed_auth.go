package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/db"
)

const (
	APIKeyPurposeXInsightFeedRead = "xinsight.feed.read"
	APIKeyStatusFeedActive        = "feed_active"
	APIKeyStatusFeedStaged        = "feed_staged"
	APIKeyStatusFeedRevoked       = "feed_revoked"
)

var completedFeedAuthDenials = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "unirouter_completed_execution_feed_auth_denials_total",
	Help: "Completed-execution feed credential denials by fixed reason.",
}, []string{"reason"})

func init() {
	prometheus.MustRegister(completedFeedAuthDenials)
}

// CompletedExecutionFeedAuth is a purpose-specific service credential gate.
// It accepts no human JWT or Agent capability and fails closed unless the key
// is active, project-bound, network-bound, and unusable for model calls.
func CompletedExecutionFeedAuth(
	q db.DBQuerier,
	apiKeyHashSecret string,
	clientIPResolver ClientIPResolver,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawCredential, err := extractAPIKey(r)
			if err != nil {
				completedFeedAuthDenials.WithLabelValues("missing_or_malformed").Inc()
				var authErr *AuthError
				if errors.As(err, &authErr) {
					writeGatewayAuthError(w, authErr.Status, authErr.Code, authErr.Message)
				}
				return
			}
			if q == nil || strings.HasPrefix(rawCredential, agentcap.TokenPrefix) {
				completedFeedAuthDenials.WithLabelValues("invalid_credential").Inc()
				writeGatewayAuthError(w, http.StatusUnauthorized, "auth_error.invalid_api_key", "Invalid API key")
				return
			}
			apiKey, orgStatus, err := resolveAPIKey(
				r.Context(),
				q,
				hashKey(rawCredential, apiKeyHashSecret),
			)
			if err != nil {
				completedFeedAuthDenials.WithLabelValues("invalid_credential").Inc()
				writeGatewayAuthError(w, http.StatusUnauthorized, "auth_error.invalid_api_key", "Invalid API key")
				return
			}
			if (apiKey.Status != APIKeyStatusFeedActive && apiKey.Status != APIKeyStatusFeedStaged) || orgStatus != "active" {
				completedFeedAuthDenials.WithLabelValues("inactive").Inc()
				if orgStatus != "active" {
					writeGatewayAuthError(w, http.StatusForbidden, "auth_error.org_"+orgStatus, "Organization is "+orgStatus)
				} else {
					writeGatewayAuthError(w, http.StatusUnauthorized, "auth_error.api_key_disabled", "API key is not active for feed access")
				}
				return
			}
			if apiKey.KeyType != "service" ||
				apiKey.Purpose != APIKeyPurposeXInsightFeedRead ||
				apiKey.ProjectID == nil || *apiKey.ProjectID <= 0 ||
				strings.TrimSpace(apiKey.IPWhitelist) == "" ||
				strings.TrimSpace(apiKey.AllowedModels) != "" {
				completedFeedAuthDenials.WithLabelValues("purpose_or_scope").Inc()
				writeGatewayAuthError(w, http.StatusForbidden, "auth_error.feed_scope_required", "API key is not authorized for the completed-execution feed")
				return
			}
			if !checkIPWhitelist(clientIPResolver.Resolve(r), apiKey.IPWhitelist) {
				completedFeedAuthDenials.WithLabelValues("network").Inc()
				writeGatewayAuthError(w, http.StatusForbidden, "auth_error.ip_not_allowed", "Client IP is not in the API key's allowed list")
				return
			}

			identifiedRequest, ok := ensureRequestIdentity(w, r, apiKeyHashSecret)
			if !ok {
				completedFeedAuthDenials.WithLabelValues("request_identity").Inc()
				return
			}
			ctx := context.WithValue(identifiedRequest.Context(), CtxAPIKey, apiKey)
			ctx = context.WithValue(ctx, CtxOrgID, apiKey.OrgID)
			ctx = context.WithValue(ctx, CtxProjectID, *apiKey.ProjectID)
			ctx = context.WithValue(ctx, CtxKeyID, apiKey.ID)
			ctx = context.WithValue(ctx, CtxAuthKind, AuthKindAPIKey)
			next.ServeHTTP(w, identifiedRequest.WithContext(ctx))
		})
	}
}
