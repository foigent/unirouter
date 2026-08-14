package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
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
		`SELECT ck.id, ck.channel_id, c.name as channel_name, c.status AS channel_status, ck.key_enc, ck.allowed_models, ck.status,
		        ck.fail_count, ck.last_used_at, ck.cooldown_until, ck.created_at, ck.updated_at
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
		var channelName, channelStatus, keyEnc, allowedModels, status string
		var failCount int
		var lastUsedAt, cooldownUntil *time.Time
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &channelID, &channelName, &channelStatus, &keyEnc, &allowedModels, &status, &failCount, &lastUsedAt, &cooldownUntil, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		keys = append(keys, map[string]interface{}{
			"id": id, "channel_id": channelID, "channel_name": channelName, "channel_status": channelStatus,
			"key_prefix": maskKey(keyEnc), "allowed_models": allowedModels, "status": status,
			"fail_count": failCount, "last_used_at": lastUsedAt, "cooldown_until": cooldownUntil,
			"created_at": createdAt, "updated_at": updatedAt,
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
	auditDetail := map[string]interface{}{"channel_id": req.ChannelID}
	if isUnauthenticatedUnitContext(r.Context()) {
		err = h.db.QueryRow(r.Context(),
			`INSERT INTO channel_keys (channel_id, key_enc, allowed_models, status, fail_count, created_at, updated_at)
			 SELECT $1, $2, $3, 'active', 0, NOW(), NOW()
			   FROM channels c
			  WHERE c.id=$1 AND (
			    c.status='disabled' OR (
			      c.status='staged' AND NOT EXISTS (
			        SELECT 1
			          FROM runtime_change_slots rcs
			          JOIN runtime_change_releases rcr ON rcr.id=rcs.stable_release_id AND rcr.stage='active'
			          CROSS JOIN LATERAL jsonb_array_elements(rcr.candidate->'overrides') AS runtime_override
			         WHERE (runtime_override->>'channel_id')::bigint=c.id
			           AND runtime_override->>'admission'='allow')))
			 RETURNING id`,
			req.ChannelID, keyEnc, req.AllowedModels,
		).Scan(&id)
		if err == nil {
			h.recordAudit(r.Context(), "channelkey.create", "channel_key", strconv.FormatInt(id, 10), auditDetail, r.RemoteAddr)
		}
	} else {
		var tx pgx.Tx
		tx, err = h.db.Begin(r.Context())
		if err == nil {
			defer tx.Rollback(r.Context())
			err = tx.QueryRow(r.Context(),
				`INSERT INTO channel_keys (channel_id, key_enc, allowed_models, status, fail_count, created_at, updated_at)
				 SELECT $1, $2, $3, 'active', 0, NOW(), NOW()
				   FROM channels c
				  WHERE c.id=$1 AND (
				    c.status='disabled' OR (
				      c.status='staged' AND NOT EXISTS (
				        SELECT 1
				          FROM runtime_change_slots rcs
				          JOIN runtime_change_releases rcr ON rcr.id=rcs.stable_release_id AND rcr.stage='active'
				          CROSS JOIN LATERAL jsonb_array_elements(rcr.candidate->'overrides') AS runtime_override
				         WHERE (runtime_override->>'channel_id')::bigint=c.id
				           AND runtime_override->>'admission'='allow')))
				 RETURNING id`,
				req.ChannelID, keyEnc, req.AllowedModels,
			).Scan(&id)
		}
		if err == nil {
			err = insertAuditEntry(r.Context(), tx, "channelkey.create", "channel_key", strconv.FormatInt(id, 10), auditDetail,
				requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), middleware.GetProjectIDFromContext(r.Context())))
		}
		if err == nil {
			err = tx.Commit(r.Context())
		}
	}
	if err != nil {
		if err == pgx.ErrNoRows {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Channel must be staged or disabled before adding credentials"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create channel key"})
		return
	}

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
	emergencyDisableOnly := req.Status != nil && *req.Status == "disabled" && req.ChannelID == nil && req.Key == nil && req.AllowedModels == nil
	if !emergencyDisableOnly {
		targetChannel := "channel_keys.channel_id"
		if req.ChannelID != nil {
			targetChannel = strconv.FormatInt(*req.ChannelID, 10)
		}
		query += " AND EXISTS (SELECT 1 FROM channels c WHERE c.id=" + targetChannel + " AND (c.status='disabled' OR (c.status='staged'" +
			" AND NOT EXISTS (SELECT 1 FROM runtime_change_slots rcs" +
			" JOIN runtime_change_releases rcr ON rcr.id=rcs.stable_release_id AND rcr.stage='active'" +
			" CROSS JOIN LATERAL jsonb_array_elements(rcr.candidate->'overrides') AS runtime_override" +
			" WHERE (runtime_override->>'channel_id')::bigint=c.id AND runtime_override->>'admission'='allow'))))"
	}
	auditDetail := map[string]interface{}{
		"channel_id_updated":     req.ChannelID != nil,
		"key_updated":            req.Key != nil,
		"status_updated":         req.Status != nil,
		"allowed_models_updated": req.AllowedModels != nil,
	}
	var tag pgconn.CommandTag
	if isUnauthenticatedUnitContext(r.Context()) {
		tag, err = h.db.Exec(r.Context(), query, args...)
		if err == nil && tag.RowsAffected() > 0 {
			h.recordAudit(r.Context(), "channelkey.update", "channel_key", idStr, auditDetail, r.RemoteAddr)
		}
	} else {
		var tx pgx.Tx
		tx, err = h.db.Begin(r.Context())
		if err == nil {
			defer tx.Rollback(r.Context())
			tag, err = tx.Exec(r.Context(), query, args...)
		}
		if err == nil && tag.RowsAffected() > 0 {
			err = insertAuditEntry(r.Context(), tx, "channelkey.update", "channel_key", idStr, auditDetail,
				requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), middleware.GetProjectIDFromContext(r.Context())))
		}
		if err == nil && tag.RowsAffected() > 0 {
			err = tx.Commit(r.Context())
		}
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		if !emergencyDisableOnly {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Channel must be staged or disabled before changing credentials"})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel key not found"})
		return
	}

	h.invalidateRoutingSnapshots()

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func normalizeChannelKeyAllowedModels(value string) string {
	return normalizeModels(value)
}

func (h *Handler) DeleteChannelKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}

	var tag pgconn.CommandTag
	if isUnauthenticatedUnitContext(r.Context()) {
		tag, err = h.db.Exec(r.Context(), `
DELETE FROM channel_keys ck USING channels c
 WHERE ck.id=$1 AND c.id=ck.channel_id AND (
   c.status='disabled' OR (
     c.status='staged' AND NOT EXISTS (
       SELECT 1 FROM runtime_change_slots rcs
       JOIN runtime_change_releases rcr ON rcr.id=rcs.stable_release_id AND rcr.stage='active'
       CROSS JOIN LATERAL jsonb_array_elements(rcr.candidate->'overrides') AS runtime_override
       WHERE (runtime_override->>'channel_id')::bigint=c.id AND runtime_override->>'admission'='allow')))`, id)
		if err == nil && tag.RowsAffected() > 0 {
			h.recordAudit(r.Context(), "channelkey.delete", "channel_key", idStr, map[string]interface{}{}, r.RemoteAddr)
		}
	} else {
		var tx pgx.Tx
		tx, err = h.db.Begin(r.Context())
		if err == nil {
			defer tx.Rollback(r.Context())
			tag, err = tx.Exec(r.Context(), `
DELETE FROM channel_keys ck USING channels c
 WHERE ck.id=$1 AND c.id=ck.channel_id AND (
   c.status='disabled' OR (
     c.status='staged' AND NOT EXISTS (
       SELECT 1 FROM runtime_change_slots rcs
       JOIN runtime_change_releases rcr ON rcr.id=rcs.stable_release_id AND rcr.stage='active'
       CROSS JOIN LATERAL jsonb_array_elements(rcr.candidate->'overrides') AS runtime_override
       WHERE (runtime_override->>'channel_id')::bigint=c.id AND runtime_override->>'admission'='allow')))`, id)
		}
		if err == nil && tag.RowsAffected() > 0 {
			err = insertAuditEntry(r.Context(), tx, "channelkey.delete", "channel_key", idStr, map[string]interface{}{},
				requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), middleware.GetProjectIDFromContext(r.Context())))
		}
		if err == nil && tag.RowsAffected() > 0 {
			err = tx.Commit(r.Context())
		}
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Channel must be staged or disabled before deleting credentials"})
		return
	}

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
