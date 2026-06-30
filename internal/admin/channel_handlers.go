package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, name, provider, base_url, models, priority, degradation_threshold, status, circuit_state, fail_count, created_at
		 FROM channels ORDER BY priority ASC, id ASC`,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	channels := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var name, provider, baseURL, models, status, circuitState string
		var priority, failCount, degradationThreshold int
		var createdAt time.Time
		rows.Scan(&id, &name, &provider, &baseURL, &models, &priority, &degradationThreshold, &status, &circuitState, &failCount, &createdAt)
		channels = append(channels, map[string]interface{}{
			"id": id, "name": name, "provider": provider, "base_url": baseURL,
			"models": models, "priority": priority, "degradation_threshold": degradationThreshold, "status": status,
			"circuit_state": circuitState, "fail_count": failCount, "created_at": createdAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": channels, "total": len(channels)})
}

func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name                 string `json:"name"`
		Provider             string `json:"provider"`
		BaseURL              string `json:"base_url"`
		Models               string `json:"models"`
		Priority             int    `json:"priority"`
		DegradationThreshold int    `json:"degradation_threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	req.Models = normalizeModels(req.Models)

	if req.Name == "" || req.Provider == "" || req.BaseURL == "" || req.Models == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name, provider, base_url, models are required"})
		return
	}

	if req.Priority == 0 {
		req.Priority = 100
	}

	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO channels (name, provider, base_url, models, priority, degradation_threshold, status, circuit_state, fail_count, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'active', 'healthy', 0, NOW(), NOW())
		 RETURNING id`,
		req.Name, req.Provider, req.BaseURL, req.Models, req.Priority, req.DegradationThreshold,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create channel"})
		return
	}

	h.recordAudit(r.Context(), "channel.create", "channel", fmt.Sprintf("%d", id),
		map[string]interface{}{"name": req.Name, "provider": req.Provider}, r.RemoteAddr)

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}

	var req struct {
		Name                 *string `json:"name"`
		Provider             *string `json:"provider"`
		BaseURL              *string `json:"base_url"`
		Models               *string `json:"models"`
		Priority             *int    `json:"priority"`
		Status               *string `json:"status"`
		DegradationThreshold *int    `json:"degradation_threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.Models != nil {
		normalized := normalizeModels(*req.Models)
		req.Models = &normalized
	}

	// Build dynamic SET clauses
	var sets []string
	var args []interface{}
	argIdx := 1

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Provider != nil {
		sets = append(sets, fmt.Sprintf("provider = $%d", argIdx))
		args = append(args, *req.Provider)
		argIdx++
	}
	if req.BaseURL != nil {
		sets = append(sets, fmt.Sprintf("base_url = $%d", argIdx))
		args = append(args, *req.BaseURL)
		argIdx++
	}
	if req.Models != nil {
		sets = append(sets, fmt.Sprintf("models = $%d", argIdx))
		args = append(args, *req.Models)
		argIdx++
	}
	if req.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, *req.Priority)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if req.DegradationThreshold != nil {
		sets = append(sets, fmt.Sprintf("degradation_threshold = $%d", argIdx))
		args = append(args, *req.DegradationThreshold)
		argIdx++
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE channels SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update channel"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}

	changedFields := []string{}
	if req.Name != nil {
		changedFields = append(changedFields, "name")
	}
	if req.Provider != nil {
		changedFields = append(changedFields, "provider")
	}
	if req.BaseURL != nil {
		changedFields = append(changedFields, "base_url")
	}
	if req.Models != nil {
		changedFields = append(changedFields, "models")
	}
	if req.Priority != nil {
		changedFields = append(changedFields, "priority")
	}
	if req.Status != nil {
		changedFields = append(changedFields, "status")
	}
	if req.DegradationThreshold != nil {
		changedFields = append(changedFields, "degradation_threshold")
	}
	h.recordAudit(r.Context(), "channel.update", "channel", idStr,
		map[string]interface{}{"fields": changedFields}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Channel updated"})
}

func (h *Handler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}

	tag, err := h.db.Exec(r.Context(), `DELETE FROM channels WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to delete channel"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}

	h.recordAudit(r.Context(), "channel.delete", "channel", idStr, map[string]interface{}{}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Channel deleted"})
}

func (h *Handler) GetChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}
	var ch struct {
		ID                   int64
		Name, Provider       string
		BaseURL, Models      string
		Priority             int
		DegradationThreshold int
		Status               string
		CircuitState         string
		FailCount            int
		CreatedAt            time.Time
	}
	err = h.db.QueryRow(r.Context(),
		`SELECT id, name, provider, base_url, models, priority, degradation_threshold, status, circuit_state, fail_count, created_at
		 FROM channels WHERE id = $1`, id).Scan(
		&ch.ID, &ch.Name, &ch.Provider, &ch.BaseURL, &ch.Models, &ch.Priority,
		&ch.DegradationThreshold, &ch.Status, &ch.CircuitState, &ch.FailCount, &ch.CreatedAt)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": ch.ID, "name": ch.Name, "provider": ch.Provider, "base_url": ch.BaseURL,
		"models": ch.Models, "priority": ch.Priority, "degradation_threshold": ch.DegradationThreshold,
		"status": ch.Status, "circuit_state": ch.CircuitState, "fail_count": ch.FailCount, "created_at": ch.CreatedAt,
	})
}
