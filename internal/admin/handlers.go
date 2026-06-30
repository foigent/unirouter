package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/middleware"

	"github.com/uniroute/unirouter/internal/db"

	"github.com/uniroute/unirouter/internal/filter"
)

// Handler contains all admin API handlers.
type Handler struct {
	sensitiveFilter *filter.DFAFilter
	db              db.DBQuerier
	jwtSecret       string
	encKey          []byte
}

// NewHandler creates a new admin Handler.
func NewHandler(db db.DBQuerier, jwtSecret string, encKey []byte, sf *filter.DFAFilter) *Handler {
	return &Handler{db: db, jwtSecret: jwtSecret, encKey: encKey, sensitiveFilter: sf}
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
