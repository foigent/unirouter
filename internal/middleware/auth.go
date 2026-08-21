package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/errdocs"
	"github.com/uniroute/unirouter/internal/gatewayerror"
	"github.com/uniroute/unirouter/internal/ipallowlist"
	"github.com/uniroute/unirouter/internal/modeloperation"
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

const anthropicMessagesVersion = "2023-06-01"

// extractGatewayCredential applies only the public model-operation credential
// contract. Admin and completed-feed surfaces deliberately keep their existing
// Authorization parser.
func extractGatewayCredential(r *http.Request) (string, error) {
	definition, known := modeloperation.Lookup(modeloperation.CanonicalPath(r.URL.Path))
	if !known {
		return extractAPIKey(r)
	}
	switch definition.ProtocolFamily {
	case "anthropic":
		return extractAnthropicCredential(r)
	case "openai":
		if headerPresent(r.Header, "x-api-key") {
			return "", &AuthError{Code: "auth_error.invalid_api_key", Message: "Conflicting API credential headers", Status: http.StatusUnauthorized}
		}
		if definition.ID == modeloperation.IDResponses &&
			(headerPresent(r.Header, "anthropic-version") || headerPresent(r.Header, "anthropic-beta")) {
			return "", &AuthError{Code: "validation_error.invalid_request", Message: "Anthropic protocol headers are not supported", Status: http.StatusBadRequest}
		}
		return extractUniqueBearerCredential(r)
	default:
		return extractAPIKey(r)
	}
}

func extractAnthropicCredential(r *http.Request) (string, error) {
	if headerPresent(r.Header, "Authorization") {
		return "", &AuthError{Code: "auth_error.invalid_api_key", Message: "Conflicting API credential headers", Status: http.StatusUnauthorized}
	}
	values := exactHeaderValues(r.Header, "x-api-key")
	if len(values) != 1 || strings.Contains(values[0], ",") || strings.TrimSpace(values[0]) == "" ||
		values[0] != strings.TrimSpace(values[0]) {
		return "", &AuthError{Code: "auth_error.invalid_api_key", Message: "Missing or invalid x-api-key header", Status: http.StatusUnauthorized}
	}
	versions := exactHeaderValues(r.Header, "anthropic-version")
	if len(versions) != 1 || versions[0] != anthropicMessagesVersion {
		return "", &AuthError{Code: "validation_error.invalid_request", Message: "Invalid anthropic-version header", Status: http.StatusBadRequest}
	}
	if headerPresent(r.Header, "anthropic-beta") {
		return "", &AuthError{Code: "validation_error.invalid_request", Message: "anthropic-beta is not supported", Status: http.StatusBadRequest}
	}
	return values[0], nil
}

func extractUniqueBearerCredential(r *http.Request) (string, error) {
	values := exactHeaderValues(r.Header, "Authorization")
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return "", &AuthError{Code: "auth_error.invalid_api_key", Message: "Missing or invalid Authorization header", Status: http.StatusUnauthorized}
	}
	authHeader := values[0]
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", &AuthError{Code: "auth_error.invalid_api_key", Message: "Invalid Authorization header format", Status: http.StatusUnauthorized}
	}
	rawKey := strings.TrimPrefix(authHeader, "Bearer ")
	if strings.TrimSpace(rawKey) == "" || rawKey != strings.TrimSpace(rawKey) {
		return "", &AuthError{Code: "auth_error.invalid_api_key", Message: "Empty API key", Status: http.StatusUnauthorized}
	}
	return rawKey, nil
}

func exactHeaderValues(header http.Header, name string) []string {
	values := make([]string, 0, 1)
	for key, current := range header {
		if strings.EqualFold(key, name) {
			values = append(values, current...)
		}
	}
	return values
}

func headerPresent(header http.Header, name string) bool {
	for key := range header {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
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
	CtxAPIKey             contextKey = "api_key"
	CtxOrgID              contextKey = "org_id"
	CtxProjectID          contextKey = "project_id"
	CtxKeyID              contextKey = "key_id"
	CtxAccessCredentialID contextKey = "access_credential_id"
	CtxRequestID          contextKey = "request_id"
	CtxUserID             contextKey = "user_id"
	CtxUserRole           contextKey = "user_role"
	CtxOrganizationRole   contextKey = "organization_role"
	CtxAuthEpoch          contextKey = "auth_epoch"
	CtxAdminSessionHash   contextKey = "admin_session_hash"
	CtxModelAlias         contextKey = "model_alias"
	CtxAuthKind           contextKey = "auth_kind"
	CtxAgentCapability    contextKey = "agent_capability"
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

// GetAccessCredentialIDFromContext returns the stable logical credential
// identity. It is zero for legacy-unbound and purpose-scoped API keys.
func GetAccessCredentialIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(CtxAccessCredentialID).(int64)
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

// GetOrganizationRoleFromContext returns the canonical account-level role.
// Project authority must be read from project_members, never inferred here.
func GetOrganizationRoleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxOrganizationRole).(string)
	return v
}

// GetAuthEpochFromContext returns the authorization epoch admitted by
// AdminAuth. The boolean distinguishes the valid zero epoch from a context
// that did not pass the production authentication middleware.
func GetAuthEpochFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(CtxAuthEpoch).(int64)
	return v, ok
}

// GetAdminSessionHashFromContext returns a defensive copy of the current
// admin-session lookup key. The raw bearer sid is never placed in context.
func GetAdminSessionHashFromContext(ctx context.Context) []byte {
	v, _ := ctx.Value(CtxAdminSessionHash).([]byte)
	return append([]byte(nil), v...)
}

// GetModelAliasFromContext retrieves the normalized public model identity after
// Gateway scope, governance, and enterprise authorization have accepted it.
// The value may equal the canonical model when no alias rewrite was needed.
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
	return ipallowlist.Matches(clientIP, whitelist)
}

// resolveAPIKey looks up and validates an API key + org status from the database.
// Extracted from GatewayAuth for unit testability.
func resolveAPIKey(ctx context.Context, q db.DBQuerier, keyHash string) (*models.ApiKey, string, error) {
	var apiKey models.ApiKey
	var orgStatus string
	var projectID int64
	var ownerUserID int64
	var accessGrantID sql.NullInt64
	err := q.QueryRow(ctx,
		`SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), ak.status, ak.allowed_ips, ak.allowed_models,
		        ak.rpm_limit, ak.qps_limit, ak.budget_limit, ak.key_prefix, ak.name,
		        COALESCE(ak.owner_user_id, 0), COALESCE(NULLIF(ak.key_type, ''), 'shared'),
		        COALESCE(ak.purpose, ''), ak.auth_epoch,
		        o.status,
		        COALESCE(ak.allowed_endpoints,''),ak.access_grant_id
		 FROM api_keys ak
		 JOIN orgs o ON o.id = ak.org_id
		  LEFT JOIN projects p ON p.id = ak.project_id AND p.org_id = ak.org_id
		  LEFT JOIN users u ON u.id = ak.owner_user_id AND u.org_id = ak.org_id
		  LEFT JOIN project_members pm ON pm.project_id = ak.project_id AND pm.user_id = ak.owner_user_id
		  LEFT JOIN workload_applications app
		    ON app.id = ak.workload_application_id AND app.org_id = ak.org_id
		   AND (ak.project_id IS NULL OR app.project_id=ak.project_id)
		  WHERE ak.key_hash = $1
		    AND ak.verifier_scheme = $2
		    AND (ak.project_id IS NULL OR p.status = 'active')
		    AND (ak.workload_application_id IS NULL OR app.status = 'active')
		    AND (
		       COALESCE(NULLIF(ak.key_type, ''), 'shared') <> 'personal'
		       OR (
		           u.status = 'active'
		           AND u.organization_role <> 'org_viewer'
		           AND (ak.project_id IS NULL OR pm.role IN ('project_admin','project_user','project_developer','developer'))
		       )
		   )`,
		keyHash,
		auth.LegacyAPIVerifierScheme,
	).Scan(
		&apiKey.ID, &apiKey.OrgID, &projectID, &apiKey.Status, &apiKey.IPWhitelist,
		&apiKey.AllowedModels, &apiKey.RPMLimit, &apiKey.QPSLimit,
		&apiKey.BudgetLimit, &apiKey.KeyPrefix, &apiKey.Name, &ownerUserID, &apiKey.KeyType, &apiKey.Purpose, &apiKey.AuthEpoch, &orgStatus,
		&apiKey.AllowedEndpoints, &accessGrantID,
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
	if accessGrantID.Valid {
		apiKey.GrantBound = true
		value := accessGrantID.Int64
		apiKey.AccessGrantID = &value
		if err := applyLiveAccessGrantBoundary(ctx, q, &apiKey); err != nil {
			return nil, "", err
		}
	}
	return &apiKey, orgStatus, nil
}

func splitScope(raw string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func isPaidGatewayEndpoint(method, path string) bool {
	return modeloperation.IsPaidSubmission(method, path)
}

func providerCostAdmissionBlocked(ctx context.Context, q db.DBQuerier, orgID, apiKeyID, accessCredentialID int64) (bool, error) {
	var blocked bool
	selector := "access_credential_id = $2"
	selectorID := accessCredentialID
	if accessCredentialID <= 0 {
		selector = "access_credential_id IS NULL AND api_key_id = $2"
		selectorID = apiKeyID
	}
	err := q.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM billing_provider_attempts
		WHERE org_id = $1 AND `+selector+`
		  AND state IN (
		      'outcome_unknown','response_unusable','funding_pending',
		      'delivery_pending','delivery_failed','output_withheld'
		  )
	)`, orgID, selectorID).Scan(&blocked)
	return blocked, err
}

func endpointAllowedForGrant(apiKey *models.ApiKey, path string) bool {
	if apiKey == nil {
		return false
	}
	return accessscope.ExecutionEndpointAllowed(
		apiKey.GrantBound,
		apiKey.GrantScopeMode,
		apiKey.EndpointDenied,
		strings.Split(apiKey.AllowedEndpoints, ","),
		path,
	)
}

func applyLiveAccessGrantBoundary(ctx context.Context, q db.DBQuerier, apiKey *models.ApiKey) error {
	if apiKey == nil || apiKey.ID <= 0 {
		return errors.New("missing root key")
	}
	snapshot, err := accessgrantboundary.Resolve(ctx, q, apiKey.ID)
	if err != nil {
		return err
	}
	*apiKey = snapshot.APIKey
	return nil
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

// GatewayAuthAcrossBufferedBody composes one authentication pass before body
// retention and the same live authentication pass immediately after it. The
// first pass supplies the trusted tenant used for byte accounting; the second
// replaces the request context with current authority before paid admission.
// Callers must pass the exact same authenticator on both sides so API keys and
// Agent capabilities retain identical attenuation semantics.
func GatewayAuthAcrossBufferedBody(
	authenticate func(http.Handler) http.Handler,
	retainBody func(http.Handler) http.Handler,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if authenticate == nil || retainBody == nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeGatewayRequestError(w, r, http.StatusInternalServerError, "server_error.internal_error", "Gateway authorization is unavailable")
			})
		}
		return authenticate(retainBody(authenticate(next)))
	}
}

// GatewayAuthWithAgentCapabilitiesAndClientIP applies the same explicit
// trusted-proxy client-IP contract used by login throttling to API-key IP
// allowlists. The zero resolver ignores forwarding headers.
func GatewayAuthWithAgentCapabilitiesAndClientIP(q db.DBQuerier, apiKeyHashSecret string, capabilities *agentcap.Service, clientIPResolver ClientIPResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawCredential, err := extractGatewayCredential(r)
			if err != nil {
				var authErr *AuthError
				if errors.As(err, &authErr) {
					writeGatewayRequestError(w, r, authErr.Status, authErr.Code, authErr.Message)
				}
				return
			}

			var apiKey *models.ApiKey
			var capabilityClaims *agentcap.Claims
			authKind := AuthKindAPIKey
			if strings.HasPrefix(rawCredential, agentcap.TokenPrefix) {
				if capabilities == nil {
					writeGatewayRequestError(w, r, http.StatusUnauthorized, "auth_error.invalid_agent_capability", "Invalid Agent capability")
					return
				}
				authentication, authErr := capabilities.Authenticate(r.Context(), rawCredential)
				if authErr != nil {
					writeAgentCapabilityAuthError(w, r, authErr)
					return
				}
				apiKey = authentication.APIKey
				capabilityClaims = authentication.Claims
				authKind = AuthKindAgentCapability
				// Authenticate already resolves the root key and live grant once for
				// this request. Reusing that exact snapshot avoids a second query
				// whose later read could disagree inside one admission decision.
				if !agentcap.IsControlEndpoint(r.URL.Path) && !capabilityClaims.AllowsEndpoint(r.URL.Path) {
					writeGatewayRequestError(w, r, http.StatusForbidden, "auth_error.agent_capability_endpoint_not_allowed", "Agent capability does not allow this model-gateway endpoint")
					return
				}
			} else {
				keyHash := hashKey(rawCredential, apiKeyHashSecret)
				var orgStatus string
				apiKey, orgStatus, err = resolveAPIKey(r.Context(), q, keyHash)
				if err != nil {
					writeGatewayRequestError(w, r, http.StatusUnauthorized, "auth_error.invalid_api_key", "Invalid API key")
					return
				}
				if apiKey.Purpose != "" {
					completedFeedAuthDenials.WithLabelValues("non_feed_route").Inc()
					writeGatewayRequestError(w, r, http.StatusForbidden, "auth_error.api_key_purpose_not_allowed", "API key purpose does not allow this model-gateway endpoint")
					return
				}
				if err := validateKeyAndOrgStatus(apiKey, orgStatus); err != nil {
					var typed *AuthError
					if errors.As(err, &typed) {
						writeGatewayRequestError(w, r, typed.Status, typed.Code, typed.Message)
					}
					return
				}
			}

			if capabilityClaims != nil && apiKey.Purpose != "" {
				completedFeedAuthDenials.WithLabelValues("non_feed_route").Inc()
				writeGatewayRequestError(w, r, http.StatusForbidden, "auth_error.api_key_purpose_not_allowed", "API key purpose does not allow this model-gateway endpoint")
				return
			}
			if !accessscope.IsCapabilityManagementEndpoint(r.URL.Path) && !endpointAllowedForGrant(apiKey, r.URL.Path) {
				writeGatewayRequestError(w, r, http.StatusForbidden, "auth_error.access_grant_endpoint_not_allowed", "Access grant does not allow this endpoint")
				return
			}

			// Resolve the caller network boundary before querying any paid-admission
			// state. A valid credential presented from a disallowed address must not
			// be able to distinguish pending provider liability from a healthy key.
			clientIP := clientIPResolver.Resolve(r)
			if !checkIPWhitelist(clientIP, apiKey.IPWhitelist) {
				writeGatewayRequestError(w, r, http.StatusForbidden, "auth_error.ip_not_allowed", "Client IP is not in the API key's allowed list")
				return
			}

			if isPaidGatewayEndpoint(r.Method, r.URL.Path) {
				accessCredentialID := int64(0)
				if apiKey.AccessCredentialID != nil {
					accessCredentialID = *apiKey.AccessCredentialID
				}
				blocked, blockErr := providerCostAdmissionBlocked(r.Context(), q, apiKey.OrgID, apiKey.ID, accessCredentialID)
				if blockErr != nil {
					writeGatewayRequestError(w, r, http.StatusServiceUnavailable, "billing_error.provider_cost_state_unavailable", "Provider cost admission state is temporarily unavailable")
					return
				}
				if blocked {
					writeGatewayRequestError(w, r, http.StatusConflict, "billing_error.provider_cost_pending", "This API key has an unresolved provider cost and is temporarily blocked from new paid requests")
					return
				}
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
			if apiKey.AccessCredentialID != nil {
				ctx = context.WithValue(ctx, CtxAccessCredentialID, *apiKey.AccessCredentialID)
			}
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

func writeAgentCapabilityAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, agentcap.ErrUnavailable):
		writeGatewayRequestError(w, r, http.StatusServiceUnavailable, "auth_error.agent_capability_state_unavailable", "Agent capability state is temporarily unavailable")
	case errors.Is(err, agentcap.ErrRevoked), errors.Is(err, agentcap.ErrSuperseded), errors.Is(err, agentcap.ErrAuthorityChanged), errors.Is(err, agentcap.ErrInactiveRoot):
		writeGatewayRequestError(w, r, http.StatusUnauthorized, "auth_error.agent_capability_revoked", "Agent capability is expired, superseded, or revoked")
	default:
		writeGatewayRequestError(w, r, http.StatusUnauthorized, "auth_error.invalid_agent_capability", "Invalid Agent capability")
	}
}

func writeGatewayRequestError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	requestID := ""
	path := ""
	if r != nil {
		requestID = GetRequestIDFromContext(r.Context())
		path = r.URL.Path
	}
	if requestID == "" {
		requestID, _ = newOperationID()
	}
	gatewayerror.Write(w, path, requestID, status, code, message)
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
			organizationRole := claims.EffectiveOrganizationRole()
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
				       AND ($6 = '' OR u.organization_role = $6)
				       AND s.revoked_at IS NULL
				       AND s.issued_at <= NOW()
				       AND s.expires_at > NOW()
				       AND u.status = 'active'
				       AND o.status = 'active'
				)`,
				claims.UserID, claims.OrgID, claims.Role, claims.AuthEpoch, sessionHash, claims.OrganizationRole,
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
			ctx = context.WithValue(ctx, CtxOrganizationRole, organizationRole)
			ctx = context.WithValue(ctx, CtxAuthEpoch, claims.AuthEpoch)
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
