package admin

import (
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/oidc"
)

// Handler contains all admin API handlers.
type Handler struct {
	sensitiveFilter              *filter.DFAFilter
	db                           db.DBQuerier
	jwtSecret                    string
	apiKeyHashSecret             string
	licenseVerifier              *licensing.Verifier
	eventService                 *billing.EventService
	oidcService                  *oidc.Service
	encKey                       []byte
	redisClient                  *redis.Client
	startedAt                    time.Time
	runtimeOptions               RuntimeStatusOptions
	runtimeHostTelemetryProvider func() runtimeHostTelemetry
}

// NewHandler creates a new admin Handler.
func NewHandler(db db.DBQuerier, jwtSecret string, encKey []byte, sf *filter.DFAFilter) *Handler {
	return NewHandlerWithAPIKeyHashSecret(db, jwtSecret, jwtSecret, encKey, sf)
}

// NewHandlerWithAPIKeyHashSecret creates a Handler with separate JWT signing
// and API-key HMAC secrets. Use this in production wiring.
func NewHandlerWithAPIKeyHashSecret(db db.DBQuerier, jwtSecret, apiKeyHashSecret string, encKey []byte, sf *filter.DFAFilter) *Handler {
	if apiKeyHashSecret == "" {
		apiKeyHashSecret = jwtSecret
	}
	return &Handler{db: db, jwtSecret: jwtSecret, apiKeyHashSecret: apiKeyHashSecret, encKey: encKey, sensitiveFilter: sf, startedAt: time.Now()}
}

func (h *Handler) SetLicenseVerifier(verifier *licensing.Verifier) {
	h.licenseVerifier = verifier
}

func (h *Handler) SetEventService(svc *billing.EventService) {
	h.eventService = svc
}

func (h *Handler) SetOIDCService(svc *oidc.Service) {
	h.oidcService = svc
}

func (h *Handler) SetRedisClient(client *redis.Client) {
	h.redisClient = client
}

func (h *Handler) SetRuntimeStatusOptions(options RuntimeStatusOptions) {
	h.runtimeOptions = options
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Not authenticated"})
		return
	}

	var username, role, status string
	var orgID int64
	var orgName string
	var createdAt time.Time
	err := h.db.QueryRow(r.Context(),
		`SELECT u.username, u.role, u.status, u.org_id, u.created_at, COALESCE(o.name, '') as org_name
		 FROM users u LEFT JOIN orgs o ON o.id = u.org_id
		 WHERE u.id = $1`, userID).Scan(&username, &role, &status, &orgID, &createdAt, &orgName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         userID,
		"username":   username,
		"role":       role,
		"status":     status,
		"org_id":     orgID,
		"org_name":   orgName,
		"created_at": createdAt,
	})
}
