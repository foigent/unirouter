package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (h *Handler) ListSettings(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformGlobalControl(w, r) {
		return
	}
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
		if err := rows.Scan(&key, &value, &description, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		item := map[string]interface{}{
			"key": key, "value": json.RawMessage(value), "description": description, "updated_at": updatedAt,
		}
		if endpoint, reserved := dedicatedGlobalSettingEndpoint(key); reserved {
			item["value"] = nil
			item["redacted"] = true
			item["dedicated_endpoint"] = endpoint
		} else if redactGlobalSettingValue(key) {
			item["value"] = nil
			item["redacted"] = true
		}
		settings = append(settings, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": settings})
}

func (h *Handler) UpdateSetting(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformGlobalControl(w, r) {
		return
	}
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "key is required"})
		return
	}
	if len(key) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "key is too long"})
		return
	}
	if endpoint, reserved := dedicatedGlobalSettingEndpoint(key); reserved {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Setting must be changed through its dedicated endpoint", "dedicated_endpoint": endpoint,
		})
		return
	}

	var req struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if len(req.Value) == 0 || !json.Valid(req.Value) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "value must be valid JSON"})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := lockGlobalControlKey(r.Context(), tx, key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lock failed"})
		return
	}
	beforeRaw, beforeFound, err := readGlobalSetting(r.Context(), tx, key, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read setting"})
		return
	}
	var afterRaw string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2::jsonb, NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
		 RETURNING value::text`,
		key, string(req.Value)).Scan(&afterRaw)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update setting"})
		return
	}
	auditResourceID, resourceIDHashed := globalControlAuditResourceID(key)
	if err := insertAuditEntry(r.Context(), tx, "setting.update", "setting", auditResourceID, map[string]interface{}{
		"scope":              "platform_global",
		"setting_key":        key,
		"resource_id_hashed": resourceIDHashed,
		"before":             globalSettingSnapshot(beforeRaw, beforeFound),
		"after":              globalSettingSnapshot([]byte(afterRaw), true),
	}, globalControlAuditMeta(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Setting updated"})
}
