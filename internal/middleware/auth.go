package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/errdocs"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/traceutil"
)

// AuthError represents an authentication error with HTTP status.
type AuthError struct {
	Code    string
	Message string
	Status  int
}

func (e *AuthError) Error() string {
	return e.Message
}

// extractAPIKey extracts the API key from the Authorization header.
// Returns the raw key and an error if extraction fails.
func extractAPIKey(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", &AuthError{Code: "auth_error.invalid_api_key", Message: "Missing Authorization header", Status: http.StatusUnauthorized}
	}
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", &AuthError{Code: "auth_error.invalid_api_key", Message: "Invalid Authorization header format", Status: http.StatusUnauthorized}
	}
	rawKey := strings.TrimPrefix(authHeader, "Bearer ")
	if rawKey == "" {
		return "", &AuthError{Code: "auth_error.invalid_api_key", Message: "Empty API key", Status: http.StatusUnauthorized}
	}
	return rawKey, nil
}

// validateKeyAndOrgStatus checks if the API key and organization are active.
// Returns an error if either is not active.
func validateKeyAndOrgStatus(apiKey *models.ApiKey, orgStatus string) error {
	if apiKey.Status != "active" {
		return &AuthError{Code: "auth_error.api_key_disabled", Message: "API key is " + apiKey.Status, Status: http.StatusUnauthorized}
	}
	if orgStatus != "active" {
		return &AuthError{Code: "auth_error.org_" + orgStatus, Message: "Organization is " + orgStatus, Status: http.StatusForbidden}
	}
	return nil
}

type contextKey string

const (
	CtxAPIKey           contextKey = "api_key"
	CtxOrgID            contextKey = "org_id"
	CtxProjectID        contextKey = "project_id"
	CtxKeyID            contextKey = "key_id"
	CtxRequestID        contextKey = "request_id"
	CtxUserID           contextKey = "user_id"
	CtxUserRole         contextKey = "user_role"
	CtxAdminSessionHash contextKey = "admin_session_hash"
	CtxModelAlias       contextKey = "model_alias"
	CtxAuthKind         contextKey = "auth_kind"
	CtxAgentCapability  contextKey = "agent_capability"
)

const (
	AuthKindAPIKey          = "api_key"
	AuthKindAgentCapability = "agent_capability"
)

// GetRequestIDFromContext retrieves the request ID from context.
func GetRequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxRequestID).(string)
	return v
}

// GetAPIKeyFromContext retrieves the API key from context.
func GetAPIKeyFromContext(ctx context.Context) *models.ApiKey {
	v, _ := ctx.Value(CtxAPIKey).(*models.ApiKey)
	return v
}

// GetOrgIDFromContext retrieves the org ID from context.
func GetOrgIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(CtxOrgID).(int64)
	return v
}

// GetProjectIDFromContext retrieves the project ID from context.
func GetProjectIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(CtxProjectID).(int64)
	return v
}

// GetKeyIDFromContext retrieves the key ID from context.
func GetKeyIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(CtxKeyID).(int64)
	return v
}

// GetUserIDFromContext retrieves the user ID from context (set by AdminAuth).
func GetUserIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(CtxUserID).(int64)
	return v
}

// GetUserRoleFromContext retrieves the user role from context (set by AdminAuth).
func GetUserRoleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxUserRole).(string)
	return v
}

// GetAdminSessionHashFromContext returns a defensive copy of the current
// admin-session lookup key. The raw bearer sid is never placed in context.
func GetAdminSessionHashFromContext(ctx context.Context) []byte {
	v, _ := ctx.Value(CtxAdminSessionHash).([]byte)
	return append([]byte(nil), v...)
}

// GetModelAliasFromContext retrieves the original requested model when the gateway resolves an alias.
func GetModelAliasFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxModelAlias).(string)
	return v
}

func GetAuthKindFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxAuthKind).(string)
	return v
}

func GetAgentCapabilityFromContext(ctx context.Context) *agentcap.Claims {
	v, _ := ctx.Value(CtxAgentCapability).(*agentcap.Claims)
	return v
}

// checkIPWhitelist checks if the client IP is allowed by the whitelist.
// Empty whitelist means all IPs are allowed.
func checkIPWhitelist(clientIP, whitelist string) bool {
	if whitelist == "" {
		return true // no restriction
	}
	for _, entry := range strings.Split(whitelist, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// Support CIDR notation
		if strings.Contains(entry, "/") {
			_, cidr, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			ip := net.ParseIP(clientIP)
			if ip != nil && cidr.Contains(ip) {
				return true
			}
		} else {
			// Exact IP match
			if entry == clientIP {
				return true
			}
		}
	}
	return false
}

// resolveAPIKey looks up and validates an API key + org status from the database.
// Extracted from GatewayAuth for unit testability.
func resolveAPIKey(ctx context.Context, q db.DBQuerier, keyHash string) (*models.ApiKey, string, error) {
	var apiKey models.ApiKey
	var orgStatus string
	var projectID int64
	var ownerUserID int64
	err := q.QueryRow(ctx,
		`SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), ak.status, ak.allowed_ips, ak.allowed_models,
		        ak.rpm_limit, ak.qps_limit, ak.budget_limit, ak.key_prefix, ak.name,
		        COALESCE(ak.owner_user_id, 0), COALESCE(NULLIF(ak.key_type, ''), 'shared'),
		        COALESCE(ak.purpose, ''),
		        o.status
		 FROM api_keys ak
		 JOIN orgs o ON o.id = ak.org_id
		 LEFT JOIN projects p ON p.id = ak.project_id AND p.org_id = ak.org_id
		 LEFT JOIN users u ON u.id = ak.owner_user_id AND u.org_id = ak.org_id
		 LEFT JOIN project_members pm ON pm.project_id = ak.project_id AND pm.user_id = ak.owner_user_id
		 WHERE ak.key_hash = $1
		   AND ak.verifier_scheme = $2
		   AND (ak.project_id IS NULL OR p.status = 'active')
		   AND (
		       COALESCE(NULLIF(ak.key_type, ''), 'shared') <> 'personal'
		       OR (
		           u.status = 'active'
		           AND (ak.project_id IS NULL OR pm.user_id IS NOT NULL)
		       )
		   )`,
		keyHash,
		auth.LegacyAPIVerifierScheme,
	).Scan(
		&apiKey.ID, &apiKey.OrgID, &projectID, &apiKey.Status, &apiKey.IPWhitelist,
		&apiKey.AllowedModels, &apiKey.RPMLimit, &apiKey.QPSLimit,
		&apiKey.BudgetLimit, &apiKey.KeyPrefix, &apiKey.Name, &ownerUserID, &apiKey.KeyType, &apiKey.Purpose, &orgStatus,
	)
	if err != nil {
		return nil, "", err
	}
	if projectID > 0 {
		apiKey.ProjectID = &projectID
	}
	if ownerUserID > 0 {
		apiKey.OwnerUserID = &ownerUserID
	}
	return &apiKey, orgStatus, nil
}

func isPaidGatewayEndpoint(path string) bool {
	return path == "/v1/chat/completions" ||
		path == "/v1/embeddings" ||
		path == "/v1/media/jobs"
}

func providerCostAdmissionBlocked(ctx context.Context, q db.DBQuerier, orgID, apiKeyID int64) (bool, error) {
	var blocked bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM billing_provider_attempts
		    WHERE org_id = $1 AND api_key_id = $2
		      AND state IN (
		          'outcome_unknown', 'response_unusable', 'funding_pending',
		          'delivery_pending', 'delivery_failed', 'output_withheld'
		      )
		)`, orgID, apiKeyID,
	).Scan(&blocked)
	return blocked, err
}

// GatewayAuth returns middleware that authenticates API requests via API Key.
func GatewayAuth(q db.DBQuerier, jwtSecret string) func(http.Handler) http.Handler {
	return GatewayAuthWithAPIKeyHashSecret(q, jwtSecret)
}

// GatewayAuthWithAPIKeyHashSecret returns middleware that authenticates API
// requests via API Key using an API-key-specific HMAC secret.
func GatewayAuthWithAPIKeyHashSecret(q db.DBQuerier, apiKeyHashSecret string) func(http.Handler) http.Handler {
	return GatewayAuthWithAgentCapabilities(q, apiKeyHashSecret, nil)
}

// GatewayAuthWithAgentCapabilities authenticates either a root API key or a
// short-lived Agent capability. Capability state and root authority are
// validated by agentcap.Service; the resulting context retains the root key ID
// so existing aggregate rate and billing boundaries are not multiplied by the
// number of child Agents.
func GatewayAuthWithAgentCapabilities(q db.DBQuerier, apiKeyHashSecret string, capabilities *agentcap.Service) func(http.Handler) http.Handler {
	return GatewayAuthWithAgentCapabilitiesAndClientIP(q, apiKeyHashSecret, capabilities, ClientIPResolver{})
}

// GatewayAuthWithAgentCapabilitiesAndClientIP applies the same explicit
// trusted-proxy client-IP contract used by login throttling to API-key IP
// allowlists. The zero resolver ignores forwarding headers.
func GatewayAuthWithAgentCapabilitiesAndClientIP(q db.DBQuerier, apiKeyHashSecret string, capabilities *agentcap.Service, clientIPResolver ClientIPResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawCredential, err := extractAPIKey(r)
			if err != nil {
				var authErr *AuthError
				if errors.As(err, &authErr) {
					writeGatewayAuthError(w, authErr.Status, authErr.Code, authErr.Message)
				}
				return
			}

			var apiKey *models.ApiKey
			var capabilityClaims *agentcap.Claims
			authKind := AuthKindAPIKey
			if strings.HasPrefix(rawCredential, agentcap.TokenPrefix) {
				if capabilities == nil {
					writeGatewayAuthError(w, http.StatusUnauthorized, "auth_error.invalid_agent_capability", "Invalid Agent capability")
					return
				}
				authentication, authErr := capabilities.Authenticate(r.Context(), rawCredential)
				if authErr != nil {
					writeAgentCapabilityAuthError(w, authErr)
					return
				}
				apiKey = authentication.APIKey
				capabilityClaims = authentication.Claims
				authKind = AuthKindAgentCapability
				if !agentcap.IsControlEndpoint(r.URL.Path) && !capabilityClaims.AllowsEndpoint(r.URL.Path) {
					writeGatewayAuthError(w, http.StatusForbidden, "auth_error.agent_capability_endpoint_not_allowed", "Agent capability does not allow this model-gateway endpoint")
					return
				}
			} else {
				keyHash := hashKey(rawCredential, apiKeyHashSecret)
				var orgStatus string
				apiKey, orgStatus, err = resolveAPIKey(r.Context(), q, keyHash)
				if err != nil {
					writeGatewayAuthError(w, http.StatusUnauthorized, "auth_error.invalid_api_key", "Invalid API key")
					return
				}
				if apiKey.Purpose != "" {
					completedFeedAuthDenials.WithLabelValues("non_feed_route").Inc()
					writeGatewayAuthError(w, http.StatusForbidden, "auth_error.api_key_purpose_not_allowed", "API key purpose does not allow this model-gateway endpoint")
					return
				}
				if err := validateKeyAndOrgStatus(apiKey, orgStatus); err != nil {
					var typed *AuthError
					if errors.As(err, &typed) {
						writeGatewayAuthError(w, typed.Status, typed.Code, typed.Message)
					}
					return
				}
			}

			if capabilityClaims != nil && apiKey.Purpose != "" {
				completedFeedAuthDenials.WithLabelValues("non_feed_route").Inc()
				writeGatewayAuthError(w, http.StatusForbidden, "auth_error.api_key_purpose_not_allowed", "API key purpose does not allow this model-gateway endpoint")
				return
			}

			if isPaidGatewayEndpoint(r.URL.Path) {
				blocked, blockErr := providerCostAdmissionBlocked(r.Context(), q, apiKey.OrgID, apiKey.ID)
				if blockErr != nil {
					writeGatewayAuthError(w, http.StatusServiceUnavailable, "billing_error.provider_cost_state_unavailable", "Provider cost admission state is temporarily unavailable")
					return
				}
				if blocked {
					writeGatewayAuthError(w, http.StatusConflict, "billing_error.provider_cost_pending", "This API key has an unresolved provider cost and is temporarily blocked from new paid requests")
					return
				}
			}

			// Check IP whitelist
			clientIP := clientIPResolver.Resolve(r)
			if !checkIPWhitelist(clientIP, apiKey.IPWhitelist) {
				writeGatewayAuthError(w, http.StatusForbidden, "auth_error.ip_not_allowed", "Client IP is not in the API key's allowed list")
				return
			}

			// Ensure the server-owned operation identity exists even when this
			// middleware is exercised without the production outer wrapper.
			identifiedRequest, ok := ensureRequestIdentity(w, r, apiKeyHashSecret)
			if !ok {
				return
			}
			r = identifiedRequest
			ctx := context.WithValue(r.Context(), CtxAPIKey, apiKey)
			ctx = context.WithValue(ctx, CtxOrgID, apiKey.OrgID)
			if apiKey.ProjectID != nil {
				ctx = context.WithValue(ctx, CtxProjectID, *apiKey.ProjectID)
			}
			ctx = context.WithValue(ctx, CtxKeyID, apiKey.ID)
			ctx = context.WithValue(ctx, CtxAuthKind, authKind)
			if capabilityClaims != nil {
				ctx = context.WithValue(ctx, CtxAgentCapability, capabilityClaims)
				ctx = traceutil.ContextWithEvidence(ctx, traceutil.EvidenceContext{
					WorkflowID: capabilityClaims.WorkflowID,
					AgentID:    capabilityClaims.AgentID,
					ActorType:  "agent",
					ActorRef:   "agent:" + capabilityClaims.AgentID,
					OnBehalfOf: "api_key:" + strconv.FormatInt(capabilityClaims.RootKeyID, 10),
				})
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAgentCapabilityAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentcap.ErrUnavailable):
		writeGatewayAuthError(w, http.StatusServiceUnavailable, "auth_error.agent_capability_state_unavailable", "Agent capability state is temporarily unavailable")
	case errors.Is(err, agentcap.ErrRevoked), errors.Is(err, agentcap.ErrSuperseded), errors.Is(err, agentcap.ErrAuthorityChanged), errors.Is(err, agentcap.ErrInactiveRoot):
		writeGatewayAuthError(w, http.StatusUnauthorized, "auth_error.agent_capability_revoked", "Agent capability is expired, superseded, or revoked")
	default:
		writeGatewayAuthError(w, http.StatusUnauthorized, "auth_error.invalid_agent_capability", "Invalid Agent capability")
	}
}

func writeGatewayAuthError(w http.ResponseWriter, status int, code, message string) {
	errType := "authentication_error"
	if status == http.StatusForbidden {
		errType = "permission_error"
	} else if status >= http.StatusInternalServerError {
		errType = "server_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"type":    errType,
			"code":    code,
			"message": message,
			"doc_url": errdocs.URLForCode(code),
			"param":   nil,
		},
	})
}

// AdminAuth returns middleware that authenticates admin JWT tokens and verifies
// that the signed principal still matches an active user, active organization,
// current auth epoch, and live server-side session. State failures fail closed
// so legacy/stateless, revoked, expired, disabled, moved, and stale-epoch
// tokens cannot authorize requests.
func AdminAuth(q db.DBQuerier, jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identifiedRequest, ok := ensureRequestIdentity(w, r, jwtSecret)
			if !ok {
				return
			}
			r = identifiedRequest

			// Extract JWT from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":{"type":"auth_error.missing_token","message":"Missing or invalid Authorization header"}}`, http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			// Validate JWT
			claims, err := auth.ParseJWT(tokenStr, jwtSecret)
			if err != nil {
				http.Error(w, `{"error":{"type":"auth_error.invalid_token","message":"Invalid or expired token"}}`, http.StatusUnauthorized)
				return
			}

			if q == nil {
				http.Error(w, `{"error":{"type":"auth_error.state_unavailable","message":"Authentication state is temporarily unavailable"}}`, http.StatusServiceUnavailable)
				return
			}
			sessionHash := auth.HashAdminSessionID(claims.SessionID)
			var principalActive bool
			err = q.QueryRow(r.Context(),
				`SELECT EXISTS (
				    SELECT 1
				      FROM users u
				      JOIN orgs o ON o.id = u.org_id
				      JOIN admin_sessions s ON s.user_id = u.id
				     WHERE u.id = $1
				       AND u.org_id = $2
				       AND u.role = $3
				       AND u.auth_epoch = $4
				       AND s.sid_hash = $5
				       AND s.revoked_at IS NULL
				       AND s.issued_at <= NOW()
				       AND s.expires_at > NOW()
				       AND u.status = 'active'
				       AND o.status = 'active'
				)`,
				claims.UserID, claims.OrgID, claims.Role, claims.AuthEpoch, sessionHash,
			).Scan(&principalActive)
			if err != nil {
				http.Error(w, `{"error":{"type":"auth_error.state_unavailable","message":"Authentication state is temporarily unavailable"}}`, http.StatusServiceUnavailable)
				return
			}
			if !principalActive {
				http.Error(w, `{"error":{"type":"auth_error.invalid_token","message":"Invalid or expired token"}}`, http.StatusUnauthorized)
				return
			}

			// Inject org ID and user info into context
			ctx := context.WithValue(r.Context(), CtxOrgID, claims.OrgID)
			ctx = context.WithValue(ctx, CtxUserID, claims.UserID)
			ctx = context.WithValue(ctx, CtxUserRole, claims.Role)
			ctx = context.WithValue(ctx, CtxAdminSessionHash, sessionHash)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// hashKey computes the HMAC-SHA256 hash of an API key using the JWT secret as the HMAC key.
func hashKey(rawKey, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(rawKey))
	return hex.EncodeToString(mac.Sum(nil))
}

// generateRequestID is retained for internal test compatibility. Production
// requests use OperationIdentity/ensureRequestIdentity and fail closed if secure
// randomness is unavailable.
func generateRequestID() string {
	requestID, err := newOperationID()
	if err != nil {
		return ""
	}
	return requestID
}
