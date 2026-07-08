package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/traceutil"
)

func (h *Handler) ListLogs(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	// Parse optional filters
	modelFilter := r.URL.Query().Get("model")
	providerFilter := r.URL.Query().Get("provider")
	statusCodeFilter := r.URL.Query().Get("status_code")
	startTime := r.URL.Query().Get("start_time")
	endTime := r.URL.Query().Get("end_time")
	requestIDFilter := r.URL.Query().Get("request_id")
	projectIDFilter := r.URL.Query().Get("project_id")
	usageSourceFilter := r.URL.Query().Get("usage_source")

	// Build WHERE clause dynamically
	var conditions []string
	var args []interface{}
	argIdx := 1

	if modelFilter != "" {
		conditions = append(conditions, fmt.Sprintf("model ILIKE $%d", argIdx))
		args = append(args, "%"+modelFilter+"%")
		argIdx++
	}
	if providerFilter != "" {
		conditions = append(conditions, fmt.Sprintf("provider = $%d", argIdx))
		args = append(args, providerFilter)
		argIdx++
	}
	if statusCodeFilter != "" {
		if sc, err := strconv.Atoi(statusCodeFilter); err == nil {
			conditions = append(conditions, fmt.Sprintf("status_code = $%d", argIdx))
			args = append(args, sc)
			argIdx++
		}
	}
	if startTime != "" {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, startTime)
		argIdx++
	}
	if endTime != "" {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, endTime)
		argIdx++
	}
	if requestIDFilter != "" {
		conditions = append(conditions, fmt.Sprintf("request_id = $%d", argIdx))
		args = append(args, requestIDFilter)
		argIdx++
	}
	if usageSourceFilter != "" {
		conditions = append(conditions, fmt.Sprintf("usage_source = $%d", argIdx))
		args = append(args, metering.NormalizeUsageSource(usageSourceFilter))
		argIdx++
	}
	if projectIDFilter != "" {
		projectID, err := strconv.ParseInt(projectIDFilter, 10, 64)
		if err != nil || projectID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, projectID)
		argIdx++
		if !h.canReadProjectUsage(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
	} else if auth.IsProjectScopedRole(actorRole(r.Context())) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "project_id is required"})
		return
	}
	latencyMin := r.URL.Query().Get("latency_min")
	latencyMax := r.URL.Query().Get("latency_max")
	if latencyMin != "" {
		if lm, err := strconv.Atoi(latencyMin); err == nil {
			conditions = append(conditions, fmt.Sprintf("latency_ms >= $%d", argIdx))
			args = append(args, lm)
			argIdx++
		}
	}
	if latencyMax != "" {
		if lx, err := strconv.Atoi(latencyMax); err == nil {
			conditions = append(conditions, fmt.Sprintf("latency_ms <= $%d", argIdx))
			args = append(args, lx)
			argIdx++
		}
	}
	// Global search: search across request_id, model, provider, upstream_error
	search := r.URL.Query().Get("search")
	if search != "" {
		conditions = append(conditions, fmt.Sprintf("(request_id ILIKE $%d OR model ILIKE $%d OR provider ILIKE $%d OR upstream_error ILIKE $%d)", argIdx, argIdx, argIdx, argIdx))
		args = append(args, "%"+search+"%")
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + conditions[0]
		for i := 1; i < len(conditions); i++ {
			whereClause += " AND " + conditions[i]
		}
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM call_logs" + whereClause
	h.db.QueryRow(r.Context(), countQuery, args...).Scan(&total)

	selectQuery := fmt.Sprintf(
		`SELECT id, COALESCE(project_id, 0), request_id, model, provider, channel, prompt_tokens, completion_tokens, total_tokens,
		        cost_cents, status_code, latency_ms, ttft_ms, upstream_status_code, COALESCE(upstream_error, ''),
		        usage_source, is_stream, created_at
		 FROM call_logs%s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1)
	args = append(args, pageSize, offset)

	rows, err := h.db.Query(r.Context(), selectQuery, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	logs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, projectID int64
		var requestID, model, provider, channel string
		var promptTokens, completionTokens, totalTokens, costCents, statusCode, latencyMs, ttftMs int
		var upstreamStatusCode *int
		var upstreamError, usageSource string
		var isStream bool
		var createdAt time.Time
		if err := rows.Scan(&id, &projectID, &requestID, &model, &provider, &channel, &promptTokens, &completionTokens, &totalTokens,
			&costCents, &statusCode, &latencyMs, &ttftMs, &upstreamStatusCode, &upstreamError,
			&usageSource, &isStream, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		logs = append(logs, map[string]interface{}{
			"id": id, "project_id": projectID, "request_id": requestID, "model": model, "provider": provider, "channel": channel,
			"prompt_tokens": promptTokens, "completion_tokens": completionTokens, "total_tokens": totalTokens,
			"cost_cents": costCents, "status_code": statusCode, "latency_ms": latencyMs, "ttft_ms": ttftMs,
			"upstream_status_code": upstreamStatusCode, "upstream_error": upstreamError,
			"usage_source": usageSource, "is_stream": isStream, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": logs, "total": total})
}

// --- Log Detail ---

func (h *Handler) GetLogDetail(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid id"})
		return
	}
	var requestID, model, provider, channel string
	var projectID int64
	var promptTokens, completionTokens, totalTokens, costCents, statusCode, latencyMs, ttftMs int
	var upstreamStatusCode *int
	var upstreamError, usageSource, ipAddress, requestedModel string
	var isStream bool
	var createdAt time.Time
	err = h.db.QueryRow(r.Context(),
		`SELECT COALESCE(project_id, 0), request_id, model, provider, channel, prompt_tokens, completion_tokens, total_tokens,
		        cost_cents, status_code, latency_ms, ttft_ms, upstream_status_code, COALESCE(upstream_error, ''),
		        usage_source, is_stream, ip_address, requested_model, created_at
		 FROM call_logs WHERE id = $1`, id,
	).Scan(&projectID, &requestID, &model, &provider, &channel, &promptTokens, &completionTokens, &totalTokens,
		&costCents, &statusCode, &latencyMs, &ttftMs, &upstreamStatusCode, &upstreamError,
		&usageSource, &isStream, &ipAddress, &requestedModel, &createdAt)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "log not found"})
		return
	}
	if !h.canReadProjectUsage(r.Context(), projectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":                   id,
		"project_id":           projectID,
		"request_id":           requestID,
		"model":                model,
		"provider":             provider,
		"channel":              channel,
		"prompt_tokens":        promptTokens,
		"completion_tokens":    completionTokens,
		"total_tokens":         totalTokens,
		"cost_cents":           costCents,
		"status_code":          statusCode,
		"latency_ms":           latencyMs,
		"ttft_ms":              ttftMs,
		"upstream_status_code": upstreamStatusCode,
		"upstream_error":       upstreamError,
		"usage_source":         usageSource,
		"is_stream":            isStream,
		"ip_address":           ipAddress,
		"requested_model":      requestedModel,
		"created_at":           createdAt,
	})
}

func (h *Handler) GetRequestTrace(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid id"})
		return
	}

	var projectID, orgID int64
	var requestID string
	err = h.db.QueryRow(r.Context(),
		`SELECT COALESCE(project_id, 0), org_id, request_id
		 FROM call_logs WHERE id = $1`, id,
	).Scan(&projectID, &orgID, &requestID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "log not found"})
		return
	}
	if !h.canReadProjectUsage(r.Context(), projectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, event_type, stage, severity, model, requested_model, provider, channel,
		        status_code, latency_ms, metadata, created_at,
		        trace_id, span_id, parent_span_id, schema_version, source_component, capture_mode,
		        capture_gap, evidence_ref, upstream_request_id,
		        decision_kind, decision_result, decision_reason,
		        actor_type, actor_ref, on_behalf_of, session_id,
		        payload_capture_mode, payload_ref, source_event_hash
		 FROM request_trace_events
		 WHERE request_id = $1
		   AND org_id = $2
		   AND COALESCE(project_id, 0) = $3
		 ORDER BY created_at ASC, id ASC`,
		requestID, orgID, projectID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	events := make([]map[string]interface{}, 0)
	for rows.Next() {
		var eventID int64
		var eventType, stage, severity, model, requestedModel, provider, channel string
		var statusCode *int
		var latencyMs int
		var metadataBytes []byte
		var createdAt time.Time
		var traceID, spanID, parentSpanID, captureGap, evidenceRef, upstreamRequestID *string
		var decisionKind, decisionResult, decisionReason, actorType, actorRef, onBehalfOf, sessionID *string
		var payloadRef, sourceEventHash *string
		var schemaVersion, sourceComponent, captureMode, payloadCaptureMode string
		if err := rows.Scan(
			&eventID, &eventType, &stage, &severity, &model, &requestedModel, &provider, &channel,
			&statusCode, &latencyMs, &metadataBytes, &createdAt,
			&traceID, &spanID, &parentSpanID, &schemaVersion, &sourceComponent, &captureMode,
			&captureGap, &evidenceRef, &upstreamRequestID,
			&decisionKind, &decisionResult, &decisionReason,
			&actorType, &actorRef, &onBehalfOf, &sessionID,
			&payloadCaptureMode, &payloadRef, &sourceEventHash,
		); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		metadata := map[string]interface{}{}
		if len(metadataBytes) > 0 {
			_ = json.Unmarshal(metadataBytes, &metadata)
		}
		metadata = sanitizeTraceResponseMetadata(metadata)
		events = append(events, map[string]interface{}{
			"id":                   eventID,
			"event_type":           eventType,
			"stage":                stage,
			"severity":             severity,
			"model":                model,
			"requested_model":      requestedModel,
			"provider":             provider,
			"channel":              channel,
			"status_code":          statusCode,
			"latency_ms":           latencyMs,
			"metadata":             metadata,
			"created_at":           createdAt,
			"trace_id":             traceID,
			"span_id":              spanID,
			"parent_span_id":       parentSpanID,
			"schema_version":       schemaVersion,
			"source_component":     sourceComponent,
			"capture_mode":         captureMode,
			"capture_gap":          captureGap,
			"evidence_ref":         evidenceRef,
			"upstream_request_id":  upstreamRequestID,
			"decision_kind":        decisionKind,
			"decision_result":      decisionResult,
			"decision_reason":      decisionReason,
			"actor_type":           actorType,
			"actor_ref":            actorRef,
			"on_behalf_of":         onBehalfOf,
			"session_id":           sessionID,
			"payload_capture_mode": payloadCaptureMode,
			"payload_ref":          payloadRef,
			"source_event_hash":    sourceEventHash,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"request_id": requestID,
		"events":     events,
	})
}

func sanitizeTraceResponseMetadata(input map[string]interface{}) map[string]interface{} {
	return traceutil.SanitizeMetadata(input)
}
