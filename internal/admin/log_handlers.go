package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (h *Handler) ListLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// Parse optional filters
	modelFilter := r.URL.Query().Get("model")
	providerFilter := r.URL.Query().Get("provider")
	statusCodeFilter := r.URL.Query().Get("status_code")
	startTime := r.URL.Query().Get("start_time")
	endTime := r.URL.Query().Get("end_time")
	requestIDFilter := r.URL.Query().Get("request_id")

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
		`SELECT id, request_id, model, provider, channel, prompt_tokens, completion_tokens, total_tokens,
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
		var id int64
		var requestID, model, provider, channel string
		var promptTokens, completionTokens, totalTokens, costCents, statusCode, latencyMs, ttftMs int
		var upstreamStatusCode *int
		var upstreamError, usageSource string
		var isStream bool
		var createdAt time.Time
		if err := rows.Scan(&id, &requestID, &model, &provider, &channel, &promptTokens, &completionTokens, &totalTokens,
			&costCents, &statusCode, &latencyMs, &ttftMs, &upstreamStatusCode, &upstreamError,
			&usageSource, &isStream, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		logs = append(logs, map[string]interface{}{
			"id": id, "request_id": requestID, "model": model, "provider": provider, "channel": channel,
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
	var promptTokens, completionTokens, totalTokens, costCents, statusCode, latencyMs, ttftMs int
	var upstreamStatusCode *int
	var upstreamError, usageSource, ipAddress, requestedModel string
	var isStream bool
	var createdAt time.Time
	err = h.db.QueryRow(r.Context(),
		`SELECT request_id, model, provider, channel, prompt_tokens, completion_tokens, total_tokens,
		        cost_cents, status_code, latency_ms, ttft_ms, upstream_status_code, upstream_error,
		        usage_source, is_stream, ip_address, requested_model, created_at
		 FROM call_logs WHERE id = $1`, id,
	).Scan(&requestID, &model, &provider, &channel, &promptTokens, &completionTokens, &totalTokens,
		&costCents, &statusCode, &latencyMs, &ttftMs, &upstreamStatusCode, &upstreamError,
		&usageSource, &isStream, &ipAddress, &requestedModel, &createdAt)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "log not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":                   id,
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
