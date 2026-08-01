package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/middleware"
)

func validChannelStatus(status string) bool {
	switch status {
	case "active", "staged", "disabled":
		return true
	default:
		return false
	}
}

type channelMutationStore interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
}

func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM channels`).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, name, provider, base_url, models, COALESCE(adapter_type, 'openai_compatible'), COALESCE(adapter_config, '{}'::jsonb),
		        priority, degradation_threshold, status, circuit_state, fail_count, created_at,
		        routing_weight, routing_epoch
		 FROM channels ORDER BY priority ASC, id ASC LIMIT $1 OFFSET $2`, pageSize, offset,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	channels := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, routingEpoch int64
		var name, provider, baseURL, models, adapterType, status, circuitState string
		var adapterConfig json.RawMessage
		var priority, failCount, degradationThreshold, routingWeight int
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &provider, &baseURL, &models, &adapterType, &adapterConfig, &priority, &degradationThreshold, &status, &circuitState, &failCount, &createdAt, &routingWeight, &routingEpoch); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read channels"})
			return
		}
		channels = append(channels, map[string]interface{}{
			"id": id, "name": name, "provider": provider, "base_url": baseURL,
			"models": models, "adapter_type": adapters.NormalizeType(adapterType), "adapter_config": adapterConfig,
			"priority": priority, "degradation_threshold": degradationThreshold, "status": status,
			"routing_weight": routingWeight, "routing_epoch": routingEpoch,
			"circuit_state": circuitState, "fail_count": failCount, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read channels"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": channels, "total": total})
}

func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name                 string          `json:"name"`
		Provider             string          `json:"provider"`
		BaseURL              string          `json:"base_url"`
		Models               string          `json:"models"`
		AdapterType          string          `json:"adapter_type"`
		AdapterConfig        json.RawMessage `json:"adapter_config"`
		Priority             int             `json:"priority"`
		RoutingWeight        int             `json:"routing_weight"`
		DegradationThreshold int             `json:"degradation_threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	req.Models = normalizeModels(req.Models)
	req.AdapterType = adapters.NormalizeType(req.AdapterType)
	if !adapters.DefaultRegistry().Has(req.AdapterType) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid adapter_type"})
		return
	}
	if len(req.AdapterConfig) == 0 || string(req.AdapterConfig) == "null" {
		req.AdapterConfig = json.RawMessage(`{}`)
	}
	if err := adapters.DefaultRegistry().ValidateConfig(req.AdapterType, req.AdapterConfig); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid adapter_config", "message": err.Error()})
		return
	}

	if req.Name == "" || req.Provider == "" || req.BaseURL == "" || req.Models == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name, provider, base_url, models are required"})
		return
	}

	if req.Priority == 0 {
		req.Priority = 100
	}
	if req.RoutingWeight == 0 {
		req.RoutingWeight = 100
	}
	if req.DegradationThreshold == 0 {
		req.DegradationThreshold = 10
	}
	if req.Priority < 1 || req.Priority > 10000 || req.RoutingWeight < 1 || req.RoutingWeight > 1000 || req.DegradationThreshold < 1 || req.DegradationThreshold > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "priority must be 1-10000, routing_weight 1-1000, and degradation_threshold 1-100"})
		return
	}

	var (
		id    int64
		tx    pgx.Tx
		store channelMutationStore = h.db
	)
	if !isUnauthenticatedUnitContext(r.Context()) {
		var err error
		tx, err = h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create channel"})
			return
		}
		defer tx.Rollback(r.Context())
		store = tx
	}

	err := store.QueryRow(r.Context(),
		`INSERT INTO channels (name, provider, base_url, models, adapter_type, adapter_config, priority, degradation_threshold, routing_weight, status, circuit_state, fail_count, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, 'staged', 'healthy', 0, NOW(), NOW())
		 RETURNING id`,
		req.Name, req.Provider, req.BaseURL, req.Models, req.AdapterType, string(req.AdapterConfig), req.Priority, req.DegradationThreshold, req.RoutingWeight,
	).Scan(&id)
	if err != nil {
		if writeCommercialLimitViolation(w, err) {
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create channel"})
		return
	}

	after := channelAuditState(id, req.Name, req.Provider, req.BaseURL, req.Models, req.AdapterType, req.Priority, req.DegradationThreshold, "staged", "healthy", 0)
	after["routing_weight"] = req.RoutingWeight
	after["routing_epoch"] = int64(1)
	detail := map[string]interface{}{"after": after}
	if tx != nil {
		if err := insertAuditEntry(r.Context(), tx, "channel.create", "channel", fmt.Sprintf("%d", id), detail,
			requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), middleware.GetProjectIDFromContext(r.Context()))); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create channel"})
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create channel"})
			return
		}
	} else {
		h.recordAudit(r.Context(), "channel.create", "channel", fmt.Sprintf("%d", id), detail, r.RemoteAddr)
	}
	h.invalidateRoutingSnapshots()

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
		Name                 *string          `json:"name"`
		Provider             *string          `json:"provider"`
		BaseURL              *string          `json:"base_url"`
		Models               *string          `json:"models"`
		AdapterType          *string          `json:"adapter_type"`
		AdapterConfig        *json.RawMessage `json:"adapter_config"`
		Priority             *int             `json:"priority"`
		RoutingWeight        *int             `json:"routing_weight"`
		ExpectedRoutingEpoch *int64           `json:"expected_routing_epoch"`
		Status               *string          `json:"status"`
		DegradationThreshold *int             `json:"degradation_threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.Models != nil {
		normalized := normalizeModels(*req.Models)
		req.Models = &normalized
	}
	if req.AdapterType != nil {
		normalized := adapters.NormalizeType(*req.AdapterType)
		if !adapters.DefaultRegistry().Has(normalized) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid adapter_type"})
			return
		}
		req.AdapterType = &normalized
	}
	if req.AdapterConfig != nil && (len(*req.AdapterConfig) == 0 || string(*req.AdapterConfig) == "null") {
		empty := json.RawMessage(`{}`)
		req.AdapterConfig = &empty
	}
	if req.Status != nil && !validChannelStatus(*req.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "status must be active, staged, or disabled"})
		return
	}
	if (req.Priority != nil && (*req.Priority < 1 || *req.Priority > 10000)) ||
		(req.RoutingWeight != nil && (*req.RoutingWeight < 1 || *req.RoutingWeight > 1000)) ||
		(req.DegradationThreshold != nil && (*req.DegradationThreshold < 1 || *req.DegradationThreshold > 100)) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "priority must be 1-10000, routing_weight 1-1000, and degradation_threshold 1-100"})
		return
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
	if req.AdapterType != nil {
		sets = append(sets, fmt.Sprintf("adapter_type = $%d", argIdx))
		args = append(args, *req.AdapterType)
		argIdx++
	}
	if req.AdapterConfig != nil {
		sets = append(sets, fmt.Sprintf("adapter_config = $%d::jsonb", argIdx))
		args = append(args, string(*req.AdapterConfig))
		argIdx++
	}
	if req.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, *req.Priority)
		argIdx++
	}
	if req.RoutingWeight != nil {
		sets = append(sets, fmt.Sprintf("routing_weight = $%d", argIdx))
		args = append(args, *req.RoutingWeight)
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

	var (
		tx    pgx.Tx
		store channelMutationStore = h.db
	)
	if !isUnauthenticatedUnitContext(r.Context()) {
		tx, err = h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update channel"})
			return
		}
		defer tx.Rollback(r.Context())
		store = tx
	}

	// adapter_type and adapter_config form one versioned configuration unit.
	// When a caller updates only one half, read and lock the other half so the
	// resulting pair is validated atomically instead of persisting a latent
	// runtime failure.
	if req.AdapterType != nil || req.AdapterConfig != nil {
		effectiveType := ""
		var effectiveConfig json.RawMessage
		if req.AdapterType != nil {
			effectiveType = *req.AdapterType
		}
		if req.AdapterConfig != nil {
			effectiveConfig = *req.AdapterConfig
		}
		if req.AdapterType == nil || req.AdapterConfig == nil {
			var existingType string
			var existingConfig json.RawMessage
			query := `SELECT COALESCE(adapter_type, 'openai_compatible'), COALESCE(adapter_config, '{}'::jsonb)
			          FROM channels WHERE id = $1`
			if tx != nil {
				query += " FOR UPDATE"
			}
			if err := store.QueryRow(r.Context(), query, id).Scan(&existingType, &existingConfig); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
				} else {
					writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to validate channel adapter configuration"})
				}
				return
			}
			if req.AdapterType == nil {
				effectiveType = adapters.NormalizeType(existingType)
			}
			if req.AdapterConfig == nil {
				effectiveConfig = existingConfig
			}
		}
		if err := adapters.DefaultRegistry().ValidateConfig(effectiveType, effectiveConfig); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid adapter_config", "message": err.Error()})
			return
		}
	}

	before, beforeFound := h.channelAuditSnapshot(r.Context(), store, id, tx != nil)
	if beforeFound {
		beforeStatus, _ := before["status"].(string)
		runtimeFieldsChanged := req.Provider != nil || req.BaseURL != nil || req.Models != nil ||
			req.AdapterType != nil || req.AdapterConfig != nil || req.Priority != nil ||
			req.RoutingWeight != nil || req.DegradationThreshold != nil
		leavesActive := req.Status == nil || *req.Status == "active"
		if beforeStatus == "active" && runtimeFieldsChanged && leavesActive {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":     "Active channel runtime fields require staging before change",
				"next_step": "set status to staged in the same update, then use Runtime Change Control for activation",
			})
			return
		}
		if req.Status != nil && *req.Status == "active" && beforeStatus != "active" {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":     "Direct channel activation is disabled",
				"next_step": "validate and promote a Runtime Change Control release",
			})
			return
		}
	}

	sets = append(sets, "routing_epoch = routing_epoch + 1", "updated_at = NOW()")
	args = append(args, id)
	where := fmt.Sprintf("id = $%d", argIdx)
	if req.ExpectedRoutingEpoch != nil {
		argIdx++
		where += fmt.Sprintf(" AND routing_epoch = $%d", argIdx)
		args = append(args, *req.ExpectedRoutingEpoch)
	}
	emergencyDisableOnly := req.Status != nil && *req.Status == "disabled" &&
		req.Name == nil && req.Provider == nil && req.BaseURL == nil && req.Models == nil &&
		req.AdapterType == nil && req.AdapterConfig == nil && req.Priority == nil &&
		req.RoutingWeight == nil && req.DegradationThreshold == nil
	if !emergencyDisableOnly {
		where += ` AND NOT EXISTS (
            SELECT 1
              FROM runtime_change_slots rcs
              JOIN runtime_change_releases rcr ON rcr.id = rcs.stable_release_id AND rcr.stage = 'active'
              CROSS JOIN LATERAL jsonb_array_elements(rcr.candidate->'overrides') AS runtime_override
             WHERE (runtime_override->>'channel_id')::bigint = channels.id
               AND runtime_override->>'admission' = 'allow'
        )`
	}

	query := fmt.Sprintf("UPDATE channels SET %s WHERE %s", joinStrings(sets, ", "), where)
	tag, err := store.Exec(r.Context(), query, args...)
	if err != nil {
		if writeCommercialLimitViolation(w, err) {
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update channel"})
		return
	}
	if tag.RowsAffected() == 0 {
		if req.ExpectedRoutingEpoch != nil {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Channel changed since it was loaded; refresh and retry"})
			return
		}
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
	if req.AdapterType != nil {
		changedFields = append(changedFields, "adapter_type")
	}
	if req.AdapterConfig != nil {
		changedFields = append(changedFields, "adapter_config")
	}
	if req.Priority != nil {
		changedFields = append(changedFields, "priority")
	}
	if req.RoutingWeight != nil {
		changedFields = append(changedFields, "routing_weight")
	}
	if req.Status != nil {
		changedFields = append(changedFields, "status")
	}
	if req.DegradationThreshold != nil {
		changedFields = append(changedFields, "degradation_threshold")
	}
	detail := map[string]interface{}{"fields": changedFields}
	if beforeFound {
		detail["before"] = before
	}
	after, afterFound := h.channelAuditSnapshot(r.Context(), store, id, false)
	if afterFound {
		detail["after"] = after
	}
	if tx != nil {
		if !beforeFound || !afterFound {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update channel"})
			return
		}
		if err := insertAuditEntry(r.Context(), tx, "channel.update", "channel", idStr, detail,
			requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), middleware.GetProjectIDFromContext(r.Context()))); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update channel"})
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update channel"})
			return
		}
	} else {
		h.recordAudit(r.Context(), "channel.update", "channel", idStr, detail, r.RemoteAddr)
	}
	h.invalidateRoutingSnapshots()

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Channel updated"})
}

func (h *Handler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}

	var (
		tx    pgx.Tx
		store channelMutationStore = h.db
	)
	if !isUnauthenticatedUnitContext(r.Context()) {
		tx, err = h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to delete channel"})
			return
		}
		defer tx.Rollback(r.Context())
		store = tx
	}

	before, beforeFound := h.channelAuditSnapshot(r.Context(), store, id, tx != nil)

	tag, err := store.Exec(r.Context(), `
DELETE FROM channels
 WHERE id = $1
   AND status <> 'active'
   AND NOT EXISTS (
       SELECT 1
         FROM runtime_change_slots rcs
         JOIN runtime_change_releases rcr ON rcr.id = rcs.stable_release_id AND rcr.stage = 'active'
         CROSS JOIN LATERAL jsonb_array_elements(rcr.candidate->'overrides') AS runtime_override
        WHERE (runtime_override->>'channel_id')::bigint = channels.id
          AND runtime_override->>'admission' = 'allow'
   )`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to delete channel"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Active channel must be staged or disabled before deletion"})
		return
	}

	detail := map[string]interface{}{}
	if beforeFound {
		detail["before"] = before
	}
	if tx != nil {
		if !beforeFound {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to delete channel"})
			return
		}
		if err := insertAuditEntry(r.Context(), tx, "channel.delete", "channel", idStr, detail,
			requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), middleware.GetProjectIDFromContext(r.Context()))); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to delete channel"})
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to delete channel"})
			return
		}
	} else {
		h.recordAudit(r.Context(), "channel.delete", "channel", idStr, detail, r.RemoteAddr)
	}
	h.invalidateRoutingSnapshots()

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Channel deleted"})
}

func channelAuditState(id int64, name, provider, baseURL, models, adapterType string, priority, degradationThreshold int, status, circuitState string, failCount int) map[string]interface{} {
	return map[string]interface{}{
		"id":                    id,
		"name":                  name,
		"provider":              provider,
		"base_url":              baseURL,
		"models":                models,
		"adapter_type":          adapters.NormalizeType(adapterType),
		"priority":              priority,
		"degradation_threshold": degradationThreshold,
		"status":                status,
		"circuit_state":         circuitState,
		"fail_count":            failCount,
	}
}

func (h *Handler) channelAuditSnapshot(ctx context.Context, store channelMutationStore, id int64, lock bool) (map[string]interface{}, bool) {
	var name, provider, baseURL, models, adapterType, status, circuitState string
	var priority, degradationThreshold, routingWeight, failCount int
	var routingEpoch int64
	query := `SELECT name, provider, base_url, models, COALESCE(adapter_type, 'openai_compatible'),
		        priority, degradation_threshold, status, circuit_state, fail_count,
		        routing_weight, routing_epoch
		 FROM channels WHERE id = $1`
	if lock {
		query += " FOR UPDATE"
	}
	err := store.QueryRow(ctx,
		query,
		id,
	).Scan(&name, &provider, &baseURL, &models, &adapterType, &priority, &degradationThreshold, &status, &circuitState, &failCount, &routingWeight, &routingEpoch)
	if err != nil {
		return nil, false
	}
	state := channelAuditState(id, name, provider, baseURL, models, adapterType, priority, degradationThreshold, status, circuitState, failCount)
	state["routing_weight"] = routingWeight
	state["routing_epoch"] = routingEpoch
	return state, true
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
		AdapterType          string
		AdapterConfig        json.RawMessage
		Priority             int
		DegradationThreshold int
		Status               string
		CircuitState         string
		FailCount            int
		CreatedAt            time.Time
		RoutingWeight        int
		RoutingEpoch         int64
	}
	err = h.db.QueryRow(r.Context(),
		`SELECT id, name, provider, base_url, models, COALESCE(adapter_type, 'openai_compatible'), COALESCE(adapter_config, '{}'::jsonb),
		        priority, degradation_threshold, status, circuit_state, fail_count, created_at,
		        routing_weight, routing_epoch
		 FROM channels WHERE id = $1`, id).Scan(
		&ch.ID, &ch.Name, &ch.Provider, &ch.BaseURL, &ch.Models, &ch.AdapterType, &ch.AdapterConfig, &ch.Priority,
		&ch.DegradationThreshold, &ch.Status, &ch.CircuitState, &ch.FailCount, &ch.CreatedAt, &ch.RoutingWeight, &ch.RoutingEpoch)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": ch.ID, "name": ch.Name, "provider": ch.Provider, "base_url": ch.BaseURL,
		"models": ch.Models, "adapter_type": adapters.NormalizeType(ch.AdapterType), "adapter_config": ch.AdapterConfig,
		"priority": ch.Priority, "degradation_threshold": ch.DegradationThreshold,
		"routing_weight": ch.RoutingWeight, "routing_epoch": ch.RoutingEpoch,
		"status": ch.Status, "circuit_state": ch.CircuitState, "fail_count": ch.FailCount, "created_at": ch.CreatedAt,
	})
}
