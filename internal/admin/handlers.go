package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"

	"github.com/uniroute/unirouter/internal/db"

	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/oidc"
)

// Handler contains all admin API handlers.
type Handler struct {
	sensitiveFilter  *filter.DFAFilter
	db               db.DBQuerier
	jwtSecret        string
	apiKeyHashSecret string
	licenseVerifier  *licensing.Verifier
	eventService     *billing.EventService
	oidcService      *oidc.Service
	encKey           []byte
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
	return &Handler{db: db, jwtSecret: jwtSecret, apiKeyHashSecret: apiKeyHashSecret, encKey: encKey, sensitiveFilter: sf}
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

func (h *Handler) emitAPIKeyDisabledEvent(ctx context.Context, orgID, projectID, keyID int64, name, keyPrefix string) {
	if h.eventService == nil || orgID <= 0 || keyID <= 0 {
		return
	}
	_, _ = h.eventService.Emit(ctx, billing.EventInput{
		OrgID:      orgID,
		ProjectID:  projectID,
		EventType:  billing.EventAPIKeyDisabled,
		Severity:   billing.NotificationSeverityWarning,
		Title:      "API key disabled",
		Message:    "API key " + name + " was disabled",
		Resource:   "api_key",
		ResourceID: strconv.FormatInt(keyID, 10),
		DedupeKey:  "api_key.disabled:" + strconv.FormatInt(keyID, 10),
		Payload: map[string]interface{}{
			"org_id":     orgID,
			"project_id": nullableInt64(projectID),
			"key_id":     keyID,
			"name":       name,
			"key_prefix": keyPrefix,
		},
	})
}

func (h *Handler) emitModelLifecycleEvent(ctx context.Context, orgID, modelID int64, modelName, lifecycle, recommendation string) {
	if h.eventService == nil || orgID <= 0 || modelID <= 0 {
		return
	}
	eventType := ""
	severity := billing.NotificationSeverityInfo
	title := ""
	switch lifecycle {
	case "deprecated":
		eventType = billing.EventModelDeprecated
		severity = billing.NotificationSeverityWarning
		title = "Model deprecated"
	case "retired":
		eventType = billing.EventModelRetired
		severity = billing.NotificationSeverityCritical
		title = "Model retired"
	default:
		return
	}
	_, _ = h.eventService.Emit(ctx, billing.EventInput{
		OrgID:      orgID,
		EventType:  eventType,
		Severity:   severity,
		Title:      title,
		Message:    "Model " + modelName + " lifecycle changed to " + lifecycle,
		Resource:   "model_registry",
		ResourceID: strconv.FormatInt(modelID, 10),
		DedupeKey:  "model.lifecycle:" + strconv.FormatInt(modelID, 10) + ":" + lifecycle,
		Payload: map[string]interface{}{
			"org_id":                   orgID,
			"model_id":                 modelID,
			"model_name":               modelName,
			"lifecycle":                lifecycle,
			"migration_recommendation": recommendation,
		},
	})
}

func (h *Handler) emitLicenseWarningEvent(ctx context.Context, status licensing.Status) {
	if h.eventService == nil || status.License == nil || !status.Warning {
		return
	}
	_, _ = h.eventService.Emit(ctx, billing.EventInput{
		OrgID:      middleware.GetOrgIDFromContext(ctx),
		EventType:  billing.EventLicenseWarning,
		Severity:   billing.NotificationSeverityWarning,
		Title:      "License expiry warning",
		Message:    "License for " + status.License.Customer + " is expiring",
		Resource:   "license",
		ResourceID: licenseResourceID(status),
		DedupeKey:  "license.warning:" + licenseResourceID(status) + ":" + status.License.ExpiresAt,
		Payload: map[string]interface{}{
			"status":            status.Status,
			"customer":          status.License.Customer,
			"deployment_id":     status.License.DeploymentID,
			"expires_at":        status.License.ExpiresAt,
			"days_until_expiry": status.DaysUntilExpiry,
		},
	})
}

// --- Helpers ---

func parsePagination(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// normalizeModels normalizes the models string:
// 1. Replace full-width commas (，) with ASCII commas (,)
// 2. Split by comma, trim whitespace, remove empty entries
// 3. Deduplicate while preserving order
func normalizeModels(models string) string {
	models = strings.ReplaceAll(models, "，", ",")
	parts := strings.Split(models, ",")
	seen := make(map[string]bool)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return strings.Join(result, ",")
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
