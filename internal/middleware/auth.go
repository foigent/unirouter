package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/errdocs"
	"github.com/uniroute/unirouter/internal/models"
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
	CtxAPIKey     contextKey = "api_key"
	CtxOrgID      contextKey = "org_id"
	CtxProjectID  contextKey = "project_id"
	CtxKeyID      contextKey = "key_id"
	CtxRequestID  contextKey = "request_id"
	CtxUserID     contextKey = "user_id"
	CtxUserRole   contextKey = "user_role"
	CtxModelAlias contextKey = "model_alias"
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

// GetModelAliasFromContext retrieves the original requested model when the gateway resolves an alias.
func GetModelAliasFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxModelAlias).(string)
	return v
}

// getClientIP extracts the client IP from the request.
// Checks X-Forwarded-For, X-Real-IP, then RemoteAddr.
func getClientIP(r *http.Request) string {
	// X-Forwarded-For may contain multiple IPs: "client, proxy1, proxy2"
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Fallback to RemoteAddr (host:port)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
	err := q.QueryRow(ctx,
		`SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), ak.status, ak.allowed_ips, ak.allowed_models,
		        ak.rpm_limit, ak.qps_limit, ak.budget_limit, ak.key_prefix, ak.name,
		        o.status
		 FROM api_keys ak
		 JOIN orgs o ON o.id = ak.org_id
		 WHERE ak.key_hash = $1`,
		keyHash,
	).Scan(
		&apiKey.ID, &apiKey.OrgID, &projectID, &apiKey.Status, &apiKey.IPWhitelist,
		&apiKey.AllowedModels, &apiKey.RPMLimit, &apiKey.QPSLimit,
		&apiKey.BudgetLimit, &apiKey.KeyPrefix, &apiKey.Name, &orgStatus,
	)
	if err != nil {
		return nil, "", err
	}
	if projectID > 0 {
		apiKey.ProjectID = &projectID
	}
	return &apiKey, orgStatus, nil
}

// GatewayAuth returns middleware that authenticates API requests via API Key.
func GatewayAuth(q db.DBQuerier, jwtSecret string) func(http.Handler) http.Handler {
	return GatewayAuthWithAPIKeyHashSecret(q, jwtSecret)
}

// GatewayAuthWithAPIKeyHashSecret returns middleware that authenticates API
// requests via API Key using an API-key-specific HMAC secret.
func GatewayAuthWithAPIKeyHashSecret(q db.DBQuerier, apiKeyHashSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract API Key from Authorization header
			rawKey, err := extractAPIKey(r)
			if err != nil {
				var authErr *AuthError
				if errors.As(err, &authErr) {
					writeGatewayAuthError(w, authErr.Status, authErr.Code, authErr.Message)
				}
				return
			}

			// Hash the key and look up in DB
			keyHash := hashKey(rawKey, apiKeyHashSecret)
			apiKey, orgStatus, err := resolveAPIKey(r.Context(), q, keyHash)
			if err != nil {
				writeGatewayAuthError(w, http.StatusUnauthorized, "auth_error.invalid_api_key", "Invalid API key")
				return
			}

			// Check API Key and Org status
			if err := validateKeyAndOrgStatus(apiKey, orgStatus); err != nil {
				var authErr *AuthError
				if errors.As(err, &authErr) {
					writeGatewayAuthError(w, authErr.Status, authErr.Code, authErr.Message)
				}
				return
			}

			// Check IP whitelist
			clientIP := getClientIP(r)
			if !checkIPWhitelist(clientIP, apiKey.IPWhitelist) {
				writeGatewayAuthError(w, http.StatusForbidden, "auth_error.ip_not_allowed", "Client IP is not in the API key's allowed list")
				return
			}

			// Inject into context
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = generateRequestID()
			}
			ctx := context.WithValue(r.Context(), CtxAPIKey, apiKey)
			ctx = context.WithValue(ctx, CtxOrgID, apiKey.OrgID)
			if apiKey.ProjectID != nil {
				ctx = context.WithValue(ctx, CtxProjectID, *apiKey.ProjectID)
			}
			ctx = context.WithValue(ctx, CtxKeyID, apiKey.ID)
			ctx = context.WithValue(ctx, CtxRequestID, requestID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeGatewayAuthError(w http.ResponseWriter, status int, code, message string) {
	errType := "authentication_error"
	if status == http.StatusForbidden {
		errType = "permission_error"
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

// AdminAuth returns middleware that authenticates admin JWT tokens.
func AdminAuth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			// Inject org ID and user info into context
			ctx := context.WithValue(r.Context(), CtxOrgID, claims.OrgID)
			ctx = context.WithValue(ctx, CtxUserID, claims.UserID)
			ctx = context.WithValue(ctx, CtxUserRole, claims.Role)

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

// generateRequestID generates a simple request ID.
func generateRequestID() string {
	return fmt.Sprintf("req_%016x", time.Now().UnixNano())
}
