package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
)

type reconciliationSummary struct {
	EstimatedTokens          int64   `json:"estimated_tokens"`
	UpstreamTokens           int64   `json:"upstream_tokens"`
	PreflightEstimatedTokens int64   `json:"preflight_estimated_tokens"`
	ActualTokens             int64   `json:"actual_tokens"`
	VarianceTokens           int64   `json:"variance_tokens"`
	VarianceRatio            float64 `json:"variance_ratio"`
	AverageSourceConfidence  float64 `json:"average_source_confidence"`
	EstimatedRatio           float64 `json:"estimated_ratio"`
	AlertThreshold           float64 `json:"estimated_ratio_alert_threshold"`
	EstimatedUsageAlert      bool    `json:"estimated_usage_alert"`
	ThresholdSetting         string  `json:"estimated_ratio_alert_threshold_setting"`
	RatioScope               string  `json:"estimated_ratio_scope"`
}

func (h *Handler) ListReconciliation(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)

	modelFilter := r.URL.Query().Get("model")
	statusFilter := r.URL.Query().Get("status")
	usageSourceFilter := r.URL.Query().Get("usage_source")

	var conditions []string
	var args []interface{}
	argIdx := 1

	if modelFilter != "" {
		conditions = append(conditions, fmt.Sprintf("model = $%d", argIdx))
		args = append(args, modelFilter)
		argIdx++
	}

	// Move statusFilter into SQL WHERE clause for correct pagination
	// "error" = statusCode >= 400, "reconciled" = statusCode < 400
	if statusFilter == "error" {
		conditions = append(conditions, "status_code >= 400")
	} else if statusFilter == "reconciled" {
		conditions = append(conditions, "status_code < 400")
	}
	if usageSourceFilter != "" {
		conditions = append(conditions, fmt.Sprintf("usage_source = $%d", argIdx))
		args = append(args, metering.NormalizeUsageSource(usageSourceFilter))
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	filterArgs := append([]interface{}(nil), args...)

	var total int
	if err := h.db.QueryRow(r.Context(), fmt.Sprintf(`SELECT COUNT(*) FROM call_logs %s`, where), filterArgs...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	summary, err := h.reconciliationSummary(r.Context(), where, filterArgs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Summary failed"})
		return
	}

	offset := paginationOffset(page, pageSize)
	query := fmt.Sprintf(
		`SELECT id, request_id, model, total_tokens, prompt_tokens, completion_tokens,
		        COALESCE(NULLIF(estimated_tokens, 0), prompt_tokens + completion_tokens),
		        status_code, usage_source, usage_confidence, created_at
		 FROM call_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		where, argIdx, argIdx+1)
	args = append(args, pageSize, offset)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	type ReconRecord struct {
		ID           int64   `json:"id"`
		RequestID    string  `json:"request_id"`
		Model        string  `json:"model"`
		EstTokens    int     `json:"estimated_tokens"`
		ActualTokens int     `json:"actual_tokens"`
		Delta        int     `json:"delta"`
		UsageSource  string  `json:"usage_source"`
		Variance     int     `json:"variance_tokens"`
		VarianceRate float64 `json:"variance_ratio"`
		Confidence   float64 `json:"source_confidence"`
		Status       string  `json:"status"`
		CreatedAt    string  `json:"created_at"`
	}

	records := make([]ReconRecord, 0)
	for rows.Next() {
		var id int64
		var requestID, model, usageSource string
		var totalTokens, promptTokens, completionTokens, estimatedTokens, statusCode int
		var usageConfidence float64
		var createdAt time.Time
		if err := rows.Scan(&id, &requestID, &model, &totalTokens, &promptTokens, &completionTokens, &estimatedTokens, &statusCode, &usageSource, &usageConfidence, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}

		usageSource = metering.NormalizeUsageSource(usageSource)
		status := "reconciled"
		if statusCode >= 400 {
			status = "error"
		}
		variance := metering.VarianceTokens(estimatedTokens, totalTokens)

		records = append(records, ReconRecord{
			ID:           id,
			RequestID:    requestID,
			Model:        model,
			EstTokens:    estimatedTokens,
			ActualTokens: totalTokens,
			Delta:        variance,
			UsageSource:  usageSource,
			Variance:     variance,
			VarianceRate: metering.VarianceRatio(estimatedTokens, totalTokens),
			Confidence:   usageConfidence,
			Status:       status,
			CreatedAt:    createdAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	threshold := h.meteringEstimatedRatioThreshold(r.Context())
	summary.AlertThreshold = threshold
	summary.EstimatedRatio = metering.EstimatedTokenRatio(int(summary.EstimatedTokens), int(summary.UpstreamTokens))
	summary.EstimatedUsageAlert = summary.EstimatedRatio > threshold
	summary.ThresholdSetting = "token_metering.estimated_ratio_alert_threshold"
	summary.RatioScope = "filtered_query"
	if summary.EstimatedUsageAlert {
		h.emitEstimatedUsageRatioEvent(r, summary, map[string]string{
			"model":        modelFilter,
			"status":       statusFilter,
			"usage_source": metering.NormalizeUsageSource(usageSourceFilter),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": records, "total": total, "page": page, "page_size": pageSize,
		"summary": summary,
	})
}

func (h *Handler) reconciliationSummary(ctx context.Context, where string, args []interface{}) (reconciliationSummary, error) {
	query := fmt.Sprintf(
		`SELECT
		    COALESCE(SUM(CASE WHEN status_code < 400 AND usage_source IN ('estimated', 'adapter_estimated', 'counter_v2') THEN total_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 AND usage_source IN ('upstream', 'api', '') THEN total_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 THEN COALESCE(NULLIF(estimated_tokens, 0), prompt_tokens + completion_tokens) ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 THEN total_tokens ELSE 0 END), 0),
		    COALESCE(AVG(CASE WHEN status_code < 400 THEN usage_confidence END), 0)::float8
		 FROM call_logs %s`,
		where,
	)

	var summary reconciliationSummary
	if err := h.db.QueryRow(ctx, query, args...).Scan(
		&summary.EstimatedTokens,
		&summary.UpstreamTokens,
		&summary.PreflightEstimatedTokens,
		&summary.ActualTokens,
		&summary.AverageSourceConfidence,
	); err != nil {
		return summary, err
	}
	summary.VarianceTokens = summary.ActualTokens - summary.PreflightEstimatedTokens
	summary.VarianceRatio = metering.VarianceRatio(int(summary.PreflightEstimatedTokens), int(summary.ActualTokens))
	return summary, nil
}

func (h *Handler) emitEstimatedUsageRatioEvent(r *http.Request, summary reconciliationSummary, filters map[string]string) {
	if h.eventService == nil {
		return
	}
	orgID := middleware.GetOrgIDFromContext(r.Context())
	if orgID <= 0 {
		return
	}
	for k, v := range filters {
		if v == "" || (k == "usage_source" && v == metering.UsageSourceUpstream && r.URL.Query().Get("usage_source") == "") {
			delete(filters, k)
		}
	}
	dedupeKey := fmt.Sprintf("token_metering.estimated_ratio.high:%d:%s:%s:%s:%s",
		orgID,
		filters["model"],
		filters["status"],
		filters["usage_source"],
		time.Now().UTC().Format("20060102"),
	)
	_, _ = h.eventService.Emit(r.Context(), billing.EventInput{
		OrgID:     orgID,
		EventType: billing.EventTokenMeteringEstimatedRatioHigh,
		Severity:  billing.NotificationSeverityWarning,
		Title:     "High estimated usage ratio",
		Message: fmt.Sprintf(
			"Estimated usage ratio %.1f%% exceeds threshold %.1f%% for reconciliation filters",
			summary.EstimatedRatio*100,
			summary.AlertThreshold*100,
		),
		Resource:   "token_metering",
		ResourceID: "estimated_ratio",
		DedupeKey:  dedupeKey,
		Payload: map[string]interface{}{
			"org_id":                            orgID,
			"estimated_tokens":                  summary.EstimatedTokens,
			"upstream_tokens":                   summary.UpstreamTokens,
			"preflight_estimated_tokens":        summary.PreflightEstimatedTokens,
			"actual_tokens":                     summary.ActualTokens,
			"variance_tokens":                   summary.VarianceTokens,
			"variance_ratio":                    summary.VarianceRatio,
			"average_source_confidence":         summary.AverageSourceConfidence,
			"estimated_ratio":                   summary.EstimatedRatio,
			"estimated_ratio_alert_threshold":   summary.AlertThreshold,
			"estimated_ratio_scope":             summary.RatioScope,
			"estimated_ratio_threshold_setting": summary.ThresholdSetting,
			"filters":                           filters,
		},
	})
}

func (h *Handler) meteringEstimatedRatioThreshold(ctx context.Context) float64 {
	var raw string
	err := h.db.QueryRow(ctx,
		`SELECT value::text FROM settings WHERE key = $1`,
		"token_metering.estimated_ratio_alert_threshold",
	).Scan(&raw)
	if err != nil {
		return metering.DefaultEstimatedRatioAlertThreshold
	}
	raw = strings.Trim(strings.TrimSpace(raw), `"`)
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || value >= 1 {
		return metering.DefaultEstimatedRatioAlertThreshold
	}
	return value
}

func (h *Handler) ListModelPrices(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)

	var total int
	err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM model_prices`).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	offset := paginationOffset(page, pageSize)
	rows, err := h.db.Query(r.Context(),
		`SELECT id, model_name, input_price_cents::float8, output_price_cents::float8 FROM model_prices ORDER BY model_name LIMIT $1 OFFSET $2`,
		pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	prices := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var modelName string
		var inputPrice, outputPrice float64
		if err := rows.Scan(&id, &modelName, &inputPrice, &outputPrice); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		prices = append(prices, map[string]interface{}{
			"id": id, "model_name": modelName, "input_price_cents": inputPrice,
			"output_price_cents": outputPrice, "currency": "CNY",
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": prices, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateModelPrice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelName        string  `json:"model_name"`
		InputPriceCents  float64 `json:"input_price_cents"`
		OutputPriceCents float64 `json:"output_price_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.ModelName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model_name required"})
		return
	}
	if req.InputPriceCents < 0 || req.OutputPriceCents < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "price must be >= 0"})
		return
	}
	var newID int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO model_prices (model_name, input_price_cents, output_price_cents) VALUES ($1, $2, $3)
		 ON CONFLICT (model_name) DO UPDATE SET input_price_cents = $2, output_price_cents = $3, updated_at = NOW()
		 RETURNING id`,
		req.ModelName, req.InputPriceCents, req.OutputPriceCents).Scan(&newID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Insert failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": newID, "model_name": req.ModelName, "input_price_cents": req.InputPriceCents, "output_price_cents": req.OutputPriceCents})
}

func (h *Handler) UpdateModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		InputPriceCents  *float64 `json:"input_price_cents"`
		OutputPriceCents *float64 `json:"output_price_cents"`
		ModelName        *string  `json:"model_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	var sets []string
	var args []interface{}
	argIdx := 1
	if req.ModelName != nil {
		sets = append(sets, fmt.Sprintf("model_name = $%d", argIdx))
		args = append(args, *req.ModelName)
		argIdx++
	}
	if req.InputPriceCents != nil {
		if *req.InputPriceCents < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "input_price_cents must be >= 0"})
			return
		}
		sets = append(sets, fmt.Sprintf("input_price_cents = $%d", argIdx))
		args = append(args, *req.InputPriceCents)
		argIdx++
	}
	if req.OutputPriceCents != nil {
		if *req.OutputPriceCents < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "output_price_cents must be >= 0"})
			return
		}
		sets = append(sets, fmt.Sprintf("output_price_cents = $%d", argIdx))
		args = append(args, *req.OutputPriceCents)
		argIdx++
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE model_prices SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tag, err := h.db.Exec(r.Context(), `DELETE FROM model_prices WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) ListOrgModelPrices(w http.ResponseWriter, r *http.Request) {
	orgIDStr := r.URL.Query().Get("org_id")
	if orgIDStr == "" {
		orgIDStr = "1"
	}
	orgID, err := strconv.ParseInt(orgIDStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
		return
	}

	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	// Count total
	var total int
	err = h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM org_model_prices WHERE org_id = $1`, orgID).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, org_id, model_name, multiplier, created_at, updated_at
		 FROM org_model_prices WHERE org_id = $1 ORDER BY model_name
		 LIMIT $2 OFFSET $3`, orgID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	var items []models.OrgModelPrice
	for rows.Next() {
		var p models.OrgModelPrice
		if err := rows.Scan(&p.ID, &p.OrgID, &p.ModelName, &p.Multiplier, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		items = append(items, p)
	}
	if items == nil {
		items = []models.OrgModelPrice{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items, "total": total})
}

func (h *Handler) CreateOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID      int64   `json:"org_id"`
		ModelName  string  `json:"model_name"`
		Multiplier float64 `json:"multiplier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.OrgID == 0 || req.ModelName == "" || req.Multiplier <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "org_id, model_name, multiplier > 0 required"})
		return
	}
	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO org_model_prices (org_id, model_name, multiplier)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, model_name) DO UPDATE SET multiplier = $3, updated_at = NOW()
		 RETURNING id`,
		req.OrgID, req.ModelName, req.Multiplier).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "success": true})
}

func (h *Handler) UpdateOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		Multiplier *float64 `json:"multiplier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.Multiplier == nil || *req.Multiplier <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "multiplier > 0 required"})
		return
	}
	tag, err := h.db.Exec(r.Context(),
		`UPDATE org_model_prices SET multiplier = $1, updated_at = NOW() WHERE id = $2`,
		*req.Multiplier, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tag, err := h.db.Exec(r.Context(), `DELETE FROM org_model_prices WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
