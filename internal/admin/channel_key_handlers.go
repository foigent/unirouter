package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) ListChannelKeys(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	var channelID int64
	if raw := strings.TrimSpace(r.URL.Query().Get("channel_id")); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "channel_id must be a positive integer"})
			return
		}
		channelID = parsed
	}

	// Count total
	var total int
	err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM channel_keys WHERE ($1::bigint=0 OR channel_id=$1)`, channelID).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT ck.id, ck.channel_id, c.name as channel_name, c.status AS channel_status, ck.key_enc, ck.allowed_models, ck.status,
		        ck.fail_count, ck.last_used_at, ck.cooldown_until, ck.state_epoch, ck.created_at, ck.updated_at
		 FROM channel_keys ck
		 JOIN channels c ON c.id = ck.channel_id
		 WHERE ($3::bigint=0 OR ck.channel_id=$3)
		 ORDER BY ck.id ASC
		 LIMIT $1 OFFSET $2`, pageSize, offset, channelID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	keys := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, channelID, stateEpoch int64
		var channelName, channelStatus, keyEnc, allowedModels, status string
		var failCount int
		var lastUsedAt, cooldownUntil *time.Time
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &channelID, &channelName, &channelStatus, &keyEnc, &allowedModels, &status, &failCount, &lastUsedAt, &cooldownUntil, &stateEpoch, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		keys = append(keys, map[string]interface{}{
			"id": id, "channel_id": channelID, "channel_name": channelName, "channel_status": channelStatus,
			"key_prefix": maskKey(keyEnc), "allowed_models": allowedModels, "status": status,
			"fail_count": failCount, "last_used_at": lastUsedAt, "cooldown_until": cooldownUntil,
			"state_epoch": stateEpoch,
			"created_at":  createdAt, "updated_at": updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys, "total": total})
}

func (h *Handler) CreateChannelKey(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) UpdateChannelKey(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) DeleteChannelKey(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

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

// maskKey returns a record-distinguishing suffix of the ciphertext. It is not a
// prefix or any other portion of the plaintext supplier credential.
func maskKey(keyEnc string) string {
	if len(keyEnc) <= 8 {
		return "****"
	}
	return "****" + keyEnc[len(keyEnc)-4:]
}
