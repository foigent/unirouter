package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/orgdirectory"
)

const directoryFreshnessWindow = 24 * time.Hour

// ListEnterpriseDirectorySources returns source metadata and orthogonal health
// only. It never returns connector credentials or provider-specific config.
func (h *Handler) ListEnterpriseDirectorySources(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	store, err := orgdirectory.NewPostgresStore(h.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Directory store unavailable"})
		return
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sources, err := store.ListSources(r.Context(), orgID, now, directoryFreshnessWindow)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Directory sources query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": sources, "total": len(sources), "org_id": orgID,
		"freshness_window_seconds": int64(directoryFreshnessWindow / time.Second),
	})
}

// ListEnterpriseDirectoryIAMActions returns durable, tenant-scoped local IAM
// convergence evidence. It is read-only and does not expose an ingestion or
// privilege-granting endpoint.
func (h *Handler) ListEnterpriseDirectoryIAMActions(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 || parsed > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "limit must be between 1 and 500"})
			return
		}
		limit = parsed
	}
	store, err := orgdirectory.NewPostgresStore(h.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Directory store unavailable"})
		return
	}
	actions, err := store.ListIAMActions(r.Context(), orgID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Directory convergence evidence query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": actions, "total": len(actions), "org_id": orgID, "limit": limit,
		"mutation_authority": "none",
	})
}

// GetEnterpriseOrganizationTree reads the current local projection at an
// effective instant. External HCM availability is never consulted here.
func (h *Handler) GetEnterpriseOrganizationTree(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	sourceRef := strings.TrimSpace(r.URL.Query().Get("source_ref"))
	if sourceRef == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "source_ref is required"})
		return
	}
	effectiveAt := time.Now().UTC().Truncate(time.Microsecond)
	if raw := strings.TrimSpace(r.URL.Query().Get("effective_at")); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil || parsed.Nanosecond()%1_000 != 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "effective_at must be RFC3339 with at most microsecond precision"})
			return
		}
		effectiveAt = parsed.UTC()
	}
	store, err := orgdirectory.NewPostgresStore(h.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Directory store unavailable"})
		return
	}
	units, err := store.ListOrganizationTree(r.Context(), orgID, sourceRef, effectiveAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Organization tree query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": units, "total": len(units), "org_id": orgID,
		"source_ref": sourceRef, "effective_at": effectiveAt,
	})
}
