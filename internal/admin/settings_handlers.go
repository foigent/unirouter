package admin

import (
	"encoding/json"
	"net/http"
	"time"
)

func (h *Handler) ListSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT key, value, description, updated_at FROM settings ORDER BY key`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	settings := make([]map[string]interface{}, 0)
	for rows.Next() {
		var key, description string
		var value []byte
		var updatedAt time.Time
		rows.Scan(&key, &value, &description, &updatedAt)
		settings = append(settings, map[string]interface{}{
			"key": key, "value": json.RawMessage(value), "description": description, "updated_at": updatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": settings})
}

func (h *Handler) UpdateSetting(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "key is required"})
		return
	}

	var req struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	_, err := h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()`,
		key, req.Value)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update setting"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Setting updated"})
}
