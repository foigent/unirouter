package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
)

func (h *Handler) ListChannelKeys(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	// Count total
	var total int
	err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM channel_keys`).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT ck.id, ck.channel_id, c.name as channel_name, ck.key_enc, ck.allowed_models, ck.status,
		        ck.fail_count, ck.last_used_at, ck.cooldown_until, ck.created_at
		 FROM channel_keys ck
		 JOIN channels c ON c.id = ck.channel_id
		 ORDER BY ck.id ASC
		 LIMIT $1 OFFSET $2`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	keys := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, channelID int64
		var channelName, keyEnc, allowedModels, status string
		var failCount int
		var lastUsedAt, cooldownUntil *time.Time
		var createdAt time.Time
		if err := rows.Scan(&id, &channelID, &channelName, &keyEnc, &allowedModels, &status, &failCount, &lastUsedAt, &cooldownUntil, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		keys = append(keys, map[string]interface{}{
			"id": id, "channel_id": channelID, "channel_name": channelName,
			"key_prefix": maskKey(keyEnc), "allowed_models": allowedModels, "status": status,
			"fail_count": failCount, "last_used_at": lastUsedAt, "cooldown_until": cooldownUntil, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys, "total": total})
}

func (h *Handler) CreateChannelKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelID     int64  `json:"channel_id"`
		Key           string `json:"key"`
		AllowedModels string `json:"allowed_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.ChannelID <= 0 || strings.TrimSpace(req.Key) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "channel_id and key are required"})
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	req.AllowedModels = normalizeChannelKeyAllowedModels(req.AllowedModels)

	// Encrypt the key
	keyEnc, err := auth.EncryptKey([]byte(req.Key), h.encKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to encrypt key"})
		return
	}

	var id int64
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO channel_keys (channel_id, key_enc, allowed_models, status, fail_count, created_at, updated_at)
		 VALUES ($1, $2, $3, 'active', 0, NOW(), NOW())
		 RETURNING id`,
		req.ChannelID, keyEnc, req.AllowedModels,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create channel key"})
		return
	}

	h.recordAudit(r.Context(), "channelkey.create", "channel_key", fmt.Sprintf("%d", id),
		map[string]interface{}{"channel_id": req.ChannelID}, r.RemoteAddr)
	h.invalidateRoutingSnapshots()

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateChannelKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}

	var req struct {
		ChannelID     *int64  `json:"channel_id"`
		Key           *string `json:"key"`
		Status        *string `json:"status"`
		AllowedModels *string `json:"allowed_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.ChannelID != nil && *req.ChannelID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "channel_id must be positive"})
		return
	}
	if req.Status != nil && *req.Status != "active" && *req.Status != "disabled" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "status must be active or disabled"})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1

	if req.ChannelID != nil {
		sets = append(sets, fmt.Sprintf("channel_id = $%d", argIdx))
		args = append(args, *req.ChannelID)
		argIdx++
	}
	if req.Key != nil {
		key := strings.TrimSpace(*req.Key)
		if key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "key cannot be empty"})
			return
		}
		keyEnc, err := auth.EncryptKey([]byte(key), h.encKey)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to encrypt key"})
			return
		}
		sets = append(sets, fmt.Sprintf("key_enc = $%d", argIdx))
		args = append(args, keyEnc)
		argIdx++
		sets = append(sets, "fail_count = 0", "cooldown_until = NULL", "last_used_at = NULL")
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if req.AllowedModels != nil {
		normalized := normalizeChannelKeyAllowedModels(*req.AllowedModels)
		sets = append(sets, fmt.Sprintf("allowed_models = $%d", argIdx))
		args = append(args, normalized)
		argIdx++
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "state_epoch = state_epoch + 1", "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE channel_keys SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel key not found"})
		return
	}

	h.recordAudit(r.Context(), "channelkey.update", "channel_key", idStr, map[string]interface{}{
		"channel_id_updated":     req.ChannelID != nil,
		"key_updated":            req.Key != nil,
		"status_updated":         req.Status != nil,
		"allowed_models_updated": req.AllowedModels != nil,
	}, r.RemoteAddr)
	h.invalidateRoutingSnapshots()

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func normalizeChannelKeyAllowedModels(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n' || r == '\r'
	})
	seen := make(map[string]struct{}, len(parts))
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		model := strings.TrimSpace(part)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
	}
	return strings.Join(normalized, ",")
}

func (h *Handler) DeleteChannelKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}

	tag, err := h.db.Exec(r.Context(), `DELETE FROM channel_keys WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel key not found"})
		return
	}

	h.recordAudit(r.Context(), "channelkey.delete", "channel_key", idStr, map[string]interface{}{}, r.RemoteAddr)
	h.invalidateRoutingSnapshots()

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// maskKey returns a record-distinguishing suffix of the ciphertext. It is not a
// prefix or any other portion of the plaintext supplier credential.
func (h *Handler) GetModelNames(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT DISTINCT model_name FROM model_prices ORDER BY model_name`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		rows.Scan(&name)
		names = append(names, name)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": names})
}

func maskKey(keyEnc string) string {
	if len(keyEnc) <= 8 {
		return "****"
	}
	return "****" + keyEnc[len(keyEnc)-4:]
}
