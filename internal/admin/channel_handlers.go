package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
)

func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM channels`).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, name, provider, base_url, models, COALESCE(model_mapping, '{}'::jsonb), COALESCE(adapter_type, 'openai_compatible'), COALESCE(adapter_config, '{}'::jsonb),
		        priority, degradation_threshold, status, circuit_state, fail_count, created_at,
		        routing_weight, routing_epoch,
		        COALESCE((
		          SELECT string_agg(DISTINCT assignment.model_name, ',' ORDER BY assignment.model_name)
		            FROM runtime_channel_assignments_v1 assignment
		           WHERE assignment.channel_id=channels.id
		             AND assignment.assignment_kind='stable'
		             AND assignment.release_stage='active'
		             AND assignment.admission='allow'
		        ), '') AS published_models
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
		var name, provider, baseURL, models, adapterType, status, circuitState, publishedModels string
		var modelMapping, adapterConfig json.RawMessage
		var priority, failCount, degradationThreshold, routingWeight int
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &provider, &baseURL, &models, &modelMapping, &adapterType, &adapterConfig, &priority, &degradationThreshold, &status, &circuitState, &failCount, &createdAt, &routingWeight, &routingEpoch, &publishedModels); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read channels"})
			return
		}
		effectiveStatus := status
		if status == "staged" && publishedModels != "" {
			effectiveStatus = "active"
		}
		channels = append(channels, map[string]interface{}{
			"id": id, "name": name, "provider": provider, "base_url": baseURL,
			"models": models, "model_mapping": modelMapping, "adapter_type": adapters.NormalizeType(adapterType), "adapter_config": adapterConfig,
			"priority": priority, "degradation_threshold": degradationThreshold, "status": status,
			"effective_status": effectiveStatus, "published_models": publishedModels,
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

func (h *Handler) CreateChannel(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) UpdateChannel(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) DeleteChannel(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
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
		ModelMapping         json.RawMessage
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
		`SELECT id, name, provider, base_url, models, COALESCE(model_mapping, '{}'::jsonb), COALESCE(adapter_type, 'openai_compatible'), COALESCE(adapter_config, '{}'::jsonb),
		        priority, degradation_threshold, status, circuit_state, fail_count, created_at,
		        routing_weight, routing_epoch
		 FROM channels WHERE id = $1`, id).Scan(
		&ch.ID, &ch.Name, &ch.Provider, &ch.BaseURL, &ch.Models, &ch.ModelMapping, &ch.AdapterType, &ch.AdapterConfig, &ch.Priority,
		&ch.DegradationThreshold, &ch.Status, &ch.CircuitState, &ch.FailCount, &ch.CreatedAt, &ch.RoutingWeight, &ch.RoutingEpoch)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": ch.ID, "name": ch.Name, "provider": ch.Provider, "base_url": ch.BaseURL,
		"models": ch.Models, "model_mapping": ch.ModelMapping, "adapter_type": adapters.NormalizeType(ch.AdapterType), "adapter_config": ch.AdapterConfig,
		"priority": ch.Priority, "degradation_threshold": ch.DegradationThreshold,
		"routing_weight": ch.RoutingWeight, "routing_epoch": ch.RoutingEpoch,
		"status": ch.Status, "circuit_state": ch.CircuitState, "fail_count": ch.FailCount, "created_at": ch.CreatedAt,
	})
}
