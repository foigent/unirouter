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
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}

	// Parse optional filters
	modelFilter := r.URL.Query().Get("model")
	providerFilter := r.URL.Query().Get("provider")
	statusCodeFilter := r.URL.Query().Get("status_code")
	startTime := r.URL.Query().Get("start_time")
	endTime := r.URL.Query().Get("end_time")
	requestIDFilter := r.URL.Query().Get("request_id")
	clientRequestIDFilter := r.URL.Query().Get("client_request_id")
	traceIDFilter := r.URL.Query().Get("trace_id")
	workflowIDFilter := r.URL.Query().Get("workflow_id")
	agentIDFilter := r.URL.Query().Get("agent_id")
	attemptIDFilter := r.URL.Query().Get("attempt_id")
	upstreamRequestIDFilter := r.URL.Query().Get("upstream_request_id")
	evidenceRefFilter := r.URL.Query().Get("evidence_ref")
	sourceEventHashFilter := r.URL.Query().Get("source_event_hash")
	projectIDFilter := r.URL.Query().Get("project_id")
	usageSourceFilter := r.URL.Query().Get("usage_source")

	// Build WHERE clause dynamically
	var conditions []string
	var args []interface{}
	argIdx := 1
	conditions = append(conditions, fmt.Sprintf("org_id = $%d", argIdx))
	args = append(args, orgID)
	argIdx++

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
		switch statusCodeFilter {
		case "2xx":
			conditions = append(conditions, fmt.Sprintf("status_code >= $%d AND status_code < $%d", argIdx, argIdx+1))
			args = append(args, 200, 300)
			argIdx += 2
		case "4xx":
			conditions = append(conditions, fmt.Sprintf("status_code >= $%d AND status_code < $%d", argIdx, argIdx+1))
			args = append(args, 400, 500)
			argIdx += 2
		case "5xx":
			conditions = append(conditions, fmt.Sprintf("status_code >= $%d AND status_code < $%d", argIdx, argIdx+1))
			args = append(args, 500, 600)
			argIdx += 2
		default:
			if sc, err := strconv.Atoi(statusCodeFilter); err == nil {
				conditions = append(conditions, fmt.Sprintf("status_code = $%d", argIdx))
				args = append(args, sc)
				argIdx++
			}
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
	for _, filter := range []struct {
		column string
		value  string
	}{
		{column: "trace_id", value: traceIDFilter},
		{column: "client_request_id", value: clientRequestIDFilter},
		{column: "workflow_id", value: workflowIDFilter},
		{column: "agent_id", value: agentIDFilter},
		{column: "upstream_request_id", value: upstreamRequestIDFilter},
		{column: "evidence_ref", value: evidenceRefFilter},
		{column: "source_event_hash", value: sourceEventHashFilter},
	} {
		if filter.value == "" {
			continue
		}
		conditions = append(conditions, fmt.Sprintf("%s = $%d", filter.column, argIdx))
		args = append(args, filter.value)
		argIdx++
	}
	if attemptIDFilter != "" {
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM request_trace_events identity_event
			WHERE identity_event.org_id = call_logs.org_id
			  AND identity_event.request_id = call_logs.request_id
			  AND COALESCE(identity_event.project_id, 0) = COALESCE(call_logs.project_id, 0)
			  AND identity_event.attempt_id = $%d
		)`, argIdx))
		args = append(args, attemptIDFilter)
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
	// Global search covers operator-visible request and structural evidence identities.
	search := r.URL.Query().Get("search")
	if search != "" {
		conditions = append(conditions, fmt.Sprintf(`(request_id ILIKE $%[1]d OR model ILIKE $%[1]d OR provider ILIKE $%[1]d
			OR client_request_id ILIKE $%[1]d OR workflow_id ILIKE $%[1]d OR agent_id ILIKE $%[1]d OR attempt_id ILIKE $%[1]d
			OR upstream_error ILIKE $%[1]d OR trace_id ILIKE $%[1]d OR upstream_request_id ILIKE $%[1]d
			OR evidence_ref ILIKE $%[1]d OR source_event_hash ILIKE $%[1]d
			OR EXISTS (
				SELECT 1 FROM request_trace_events search_event
				WHERE search_event.org_id = call_logs.org_id
				  AND search_event.request_id = call_logs.request_id
				  AND COALESCE(search_event.project_id, 0) = COALESCE(call_logs.project_id, 0)
				  AND search_event.attempt_id ILIKE $%[1]d
			))`, argIdx))
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
	if err := h.db.QueryRow(r.Context(), countQuery, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	selectQuery := fmt.Sprintf(
		`SELECT id, COALESCE(project_id, 0), request_id,
		        COALESCE(client_request_id, ''), COALESCE(workflow_id, ''), COALESCE(agent_id, ''), COALESCE(attempt_id, ''),
		        model, provider, channel, prompt_tokens,
		        CASE WHEN cache_usage_schema = '' AND uncached_prompt_tokens = 0 THEN prompt_tokens ELSE uncached_prompt_tokens END,
		        cache_read_tokens, cache_creation_tokens, completion_tokens, total_tokens,
		        cost_cents, status_code, latency_ms, ttft_ms, upstream_status_code, COALESCE(upstream_error, ''),
		        usage_source, cache_usage_schema, cache_usage_status, pricing_version,
		        COALESCE(
		          (SELECT f.cache_read_ratio::float8 FROM freeze_idempotency f
		           WHERE f.org_id = call_logs.org_id AND f.request_id = call_logs.request_id),
		          CASE WHEN effective_input_price_cents <> 0
		               THEN (effective_cache_read_price_cents / effective_input_price_cents)::float8
		               ELSE 1::float8 END
		        ),
		        COALESCE(
		          (SELECT f.cache_creation_ratio::float8 FROM freeze_idempotency f
		           WHERE f.org_id = call_logs.org_id AND f.request_id = call_logs.request_id),
		          CASE WHEN effective_input_price_cents <> 0
		               THEN (effective_cache_creation_price_cents / effective_input_price_cents)::float8
		               ELSE 1::float8 END
		        ),
		        effective_input_price_cents::float8, effective_cache_read_price_cents::float8,
		        effective_cache_creation_price_cents::float8, effective_output_price_cents::float8,
		        cache_read_cost_cents::float8, cache_creation_cost_cents::float8,
		        is_stream, created_at
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
		var requestID, clientRequestID, workflowID, agentID, attemptID, model, provider, channel string
		var promptTokens, uncachedPromptTokens, cacheReadTokens, cacheCreationTokens, completionTokens, totalTokens, costCents, statusCode, latencyMs, ttftMs int
		var upstreamStatusCode *int
		var upstreamError, usageSource, cacheUsageSchema, cacheUsageStatus, pricingVersion string
		var cacheReadRatio, cacheCreationRatio float64
		var effectiveInputPrice, effectiveCacheReadPrice, effectiveCacheCreationPrice, effectiveOutputPrice float64
		var cacheReadCost, cacheCreationCost float64
		var isStream bool
		var createdAt time.Time
		if err := rows.Scan(&id, &projectID, &requestID, &clientRequestID, &workflowID, &agentID, &attemptID,
			&model, &provider, &channel, &promptTokens, &uncachedPromptTokens, &cacheReadTokens, &cacheCreationTokens, &completionTokens, &totalTokens,
			&costCents, &statusCode, &latencyMs, &ttftMs, &upstreamStatusCode, &upstreamError,
			&usageSource, &cacheUsageSchema, &cacheUsageStatus, &pricingVersion,
			&cacheReadRatio, &cacheCreationRatio,
			&effectiveInputPrice, &effectiveCacheReadPrice, &effectiveCacheCreationPrice, &effectiveOutputPrice,
			&cacheReadCost, &cacheCreationCost, &isStream, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		logs = append(logs, map[string]interface{}{
			"id": id, "project_id": projectID, "request_id": requestID, "client_request_id": clientRequestID,
			"workflow_id": workflowID, "agent_id": agentID, "attempt_id": attemptID,
			"model": model, "provider": provider, "channel": channel,
			"prompt_tokens": promptTokens, "uncached_prompt_tokens": uncachedPromptTokens,
			"cache_read_tokens": cacheReadTokens, "cache_creation_tokens": cacheCreationTokens,
			"completion_tokens": completionTokens, "total_tokens": totalTokens,
			"cost_cents": costCents, "status_code": statusCode, "latency_ms": latencyMs, "ttft_ms": ttftMs,
			"upstream_status_code": upstreamStatusCode, "upstream_error": upstreamError,
			"usage_source": usageSource, "cache_usage_schema": cacheUsageSchema, "cache_usage_status": cacheUsageStatus,
			"pricing_version": pricingVersion, "cache_read_ratio": cacheReadRatio, "cache_creation_ratio": cacheCreationRatio,
			"effective_input_price_cents":          effectiveInputPrice,
			"effective_cache_read_price_cents":     effectiveCacheReadPrice,
			"effective_cache_creation_price_cents": effectiveCacheCreationPrice,
			"effective_output_price_cents":         effectiveOutputPrice,
			"cache_read_cost_cents":                cacheReadCost, "cache_creation_cost_cents": cacheCreationCost,
			"is_stream": isStream, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": logs, "total": total, "org_id": orgID})
}

// --- Log Detail ---

func (h *Handler) GetLogDetail(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid id"})
		return
	}
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	var requestID, clientRequestID, workflowID, agentID, attemptID, model, provider, channel string
	var recordOrgID, projectID int64
	var promptTokens, uncachedPromptTokens, cacheReadTokens, cacheCreationTokens, completionTokens, totalTokens, costCents, statusCode, latencyMs, ttftMs int
	var upstreamStatusCode *int
	var upstreamError, usageSource, ipAddress, requestedModel, cacheUsageSchema, cacheUsageStatus, pricingVersion string
	var cacheReadRatio, cacheCreationRatio float64
	var effectiveInputPrice, effectiveCacheReadPrice, effectiveCacheCreationPrice, effectiveOutputPrice float64
	var cacheReadCost, cacheCreationCost float64
	var isStream bool
	var createdAt time.Time
	err = h.db.QueryRow(r.Context(),
		`SELECT org_id, COALESCE(project_id, 0), request_id,
		        COALESCE(client_request_id, ''), COALESCE(workflow_id, ''), COALESCE(agent_id, ''), COALESCE(attempt_id, ''),
		        model, provider, channel, prompt_tokens,
		        CASE WHEN cache_usage_schema = '' AND uncached_prompt_tokens = 0 THEN prompt_tokens ELSE uncached_prompt_tokens END,
		        cache_read_tokens, cache_creation_tokens, completion_tokens, total_tokens,
		        cost_cents, status_code, latency_ms, ttft_ms, upstream_status_code, COALESCE(upstream_error, ''),
		        usage_source, cache_usage_schema, cache_usage_status, pricing_version,
		        COALESCE(
		          (SELECT f.cache_read_ratio::float8 FROM freeze_idempotency f
		           WHERE f.org_id = call_logs.org_id AND f.request_id = call_logs.request_id),
		          CASE WHEN effective_input_price_cents <> 0
		               THEN (effective_cache_read_price_cents / effective_input_price_cents)::float8
		               ELSE 1::float8 END
		        ),
		        COALESCE(
		          (SELECT f.cache_creation_ratio::float8 FROM freeze_idempotency f
		           WHERE f.org_id = call_logs.org_id AND f.request_id = call_logs.request_id),
		          CASE WHEN effective_input_price_cents <> 0
		               THEN (effective_cache_creation_price_cents / effective_input_price_cents)::float8
		               ELSE 1::float8 END
		        ),
		        effective_input_price_cents::float8, effective_cache_read_price_cents::float8,
		        effective_cache_creation_price_cents::float8, effective_output_price_cents::float8,
		        cache_read_cost_cents::float8, cache_creation_cost_cents::float8,
		        is_stream, ip_address, requested_model, created_at
		 FROM call_logs WHERE id = $1 AND org_id = $2`, id, orgID,
	).Scan(&recordOrgID, &projectID, &requestID, &clientRequestID, &workflowID, &agentID, &attemptID,
		&model, &provider, &channel, &promptTokens, &uncachedPromptTokens, &cacheReadTokens, &cacheCreationTokens, &completionTokens, &totalTokens,
		&costCents, &statusCode, &latencyMs, &ttftMs, &upstreamStatusCode, &upstreamError,
		&usageSource, &cacheUsageSchema, &cacheUsageStatus, &pricingVersion,
		&cacheReadRatio, &cacheCreationRatio,
		&effectiveInputPrice, &effectiveCacheReadPrice, &effectiveCacheCreationPrice, &effectiveOutputPrice,
		&cacheReadCost, &cacheCreationCost, &isStream, &ipAddress, &requestedModel, &createdAt)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "log not found"})
		return
	}
	if !h.canReadUsageRecord(r.Context(), recordOrgID, projectID) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "log not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":                                   id,
		"org_id":                               recordOrgID,
		"project_id":                           projectID,
		"request_id":                           requestID,
		"client_request_id":                    clientRequestID,
		"workflow_id":                          workflowID,
		"agent_id":                             agentID,
		"attempt_id":                           attemptID,
		"model":                                model,
		"provider":                             provider,
		"channel":                              channel,
		"prompt_tokens":                        promptTokens,
		"uncached_prompt_tokens":               uncachedPromptTokens,
		"cache_read_tokens":                    cacheReadTokens,
		"cache_creation_tokens":                cacheCreationTokens,
		"completion_tokens":                    completionTokens,
		"total_tokens":                         totalTokens,
		"cost_cents":                           costCents,
		"status_code":                          statusCode,
		"latency_ms":                           latencyMs,
		"ttft_ms":                              ttftMs,
		"upstream_status_code":                 upstreamStatusCode,
		"upstream_error":                       upstreamError,
		"usage_source":                         usageSource,
		"cache_usage_schema":                   cacheUsageSchema,
		"cache_usage_status":                   cacheUsageStatus,
		"pricing_version":                      pricingVersion,
		"cache_read_ratio":                     cacheReadRatio,
		"cache_creation_ratio":                 cacheCreationRatio,
		"effective_input_price_cents":          effectiveInputPrice,
		"effective_cache_read_price_cents":     effectiveCacheReadPrice,
		"effective_cache_creation_price_cents": effectiveCacheCreationPrice,
		"effective_output_price_cents":         effectiveOutputPrice,
		"cache_read_cost_cents":                cacheReadCost,
		"cache_creation_cost_cents":            cacheCreationCost,
		"is_stream":                            isStream,
		"ip_address":                           ipAddress,
		"requested_model":                      requestedModel,
		"created_at":                           createdAt,
	})
}

func (h *Handler) GetRequestTrace(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid id"})
		return
	}
	requestedOrgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}

	var projectID, orgID int64
	var requestID string
	err = h.db.QueryRow(r.Context(),
		`SELECT COALESCE(project_id, 0), org_id, request_id
		 FROM call_logs WHERE id = $1 AND org_id = $2`, id, requestedOrgID,
	).Scan(&projectID, &orgID, &requestID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "log not found"})
		return
	}
	if !h.canReadUsageRecord(r.Context(), orgID, projectID) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "log not found"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, event_type, stage, severity, model, requested_model, provider, channel,
		        status_code, latency_ms, metadata, created_at,
		        trace_id, span_id, parent_span_id, schema_version, source_component, capture_mode,
		        capture_gap, evidence_ref, upstream_request_id,
		        decision_kind, decision_result, decision_reason,
		        actor_type, actor_ref, on_behalf_of, session_id,
		        COALESCE(client_request_id, ''), COALESCE(workflow_id, ''), COALESCE(agent_id, ''), COALESCE(attempt_id, ''),
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
		var clientRequestID, workflowID, agentID, attemptID string
		var payloadRef, sourceEventHash *string
		var schemaVersion, sourceComponent, captureMode, payloadCaptureMode string
		if err := rows.Scan(
			&eventID, &eventType, &stage, &severity, &model, &requestedModel, &provider, &channel,
			&statusCode, &latencyMs, &metadataBytes, &createdAt,
			&traceID, &spanID, &parentSpanID, &schemaVersion, &sourceComponent, &captureMode,
			&captureGap, &evidenceRef, &upstreamRequestID,
			&decisionKind, &decisionResult, &decisionReason,
			&actorType, &actorRef, &onBehalfOf, &sessionID,
			&clientRequestID, &workflowID, &agentID, &attemptID,
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
			"client_request_id":    clientRequestID,
			"workflow_id":          workflowID,
			"agent_id":             agentID,
			"attempt_id":           attemptID,
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
