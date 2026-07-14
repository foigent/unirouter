package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
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
	CacheReadTokens          int64   `json:"cache_read_tokens"`
	CacheCreationTokens      int64   `json:"cache_creation_tokens"`
	CacheReadCostMicros      int64   `json:"cache_read_cost_micros,string"`
	CacheCreationCostMicros  int64   `json:"cache_creation_cost_micros,string"`
	Currency                 string  `json:"currency"`
	AmountScale              int64   `json:"amount_scale"`
	AuthoritativeCacheRows   int64   `json:"authoritative_cache_records"`
	UnreportedCacheRows      int64   `json:"unreported_cache_records"`
}

func (h *Handler) ListReconciliation(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}

	modelFilter := r.URL.Query().Get("model")
	statusFilter := r.URL.Query().Get("status")
	usageSourceFilter := r.URL.Query().Get("usage_source")

	var conditions []string
	var args []interface{}
	argIdx := 1
	conditions = append(conditions, fmt.Sprintf("org_id = $%d", argIdx))
	args = append(args, orgID)
	argIdx++

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
		        status_code, usage_source, usage_confidence,
		        CASE WHEN cache_usage_schema = '' AND uncached_prompt_tokens = 0 THEN prompt_tokens ELSE uncached_prompt_tokens END,
		        cache_read_tokens, cache_creation_tokens, cache_usage_schema, cache_usage_status, pricing_version,
		        cache_read_cost_micros, cache_creation_cost_micros,
		        created_at
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
		ID                      int64   `json:"id"`
		RequestID               string  `json:"request_id"`
		Model                   string  `json:"model"`
		EstTokens               int     `json:"estimated_tokens"`
		ActualTokens            int     `json:"actual_tokens"`
		Delta                   int     `json:"delta"`
		UsageSource             string  `json:"usage_source"`
		Variance                int     `json:"variance_tokens"`
		VarianceRate            float64 `json:"variance_ratio"`
		Confidence              float64 `json:"source_confidence"`
		UncachedPromptTokens    int     `json:"uncached_prompt_tokens"`
		CacheReadTokens         int     `json:"cache_read_tokens"`
		CacheCreationTokens     int     `json:"cache_creation_tokens"`
		CacheUsageSchema        string  `json:"cache_usage_schema"`
		CacheUsageStatus        string  `json:"cache_usage_status"`
		PricingVersion          string  `json:"pricing_version"`
		CacheReadCostMicros     int64   `json:"cache_read_cost_micros,string"`
		CacheCreationCostMicros int64   `json:"cache_creation_cost_micros,string"`
		Currency                string  `json:"currency"`
		AmountScale             int64   `json:"amount_scale"`
		Status                  string  `json:"status"`
		CreatedAt               string  `json:"created_at"`
	}

	records := make([]ReconRecord, 0)
	for rows.Next() {
		var id int64
		var requestID, model, usageSource, cacheUsageSchema, cacheUsageStatus, pricingVersion string
		var totalTokens, promptTokens, completionTokens, estimatedTokens, statusCode int
		var uncachedPromptTokens, cacheReadTokens, cacheCreationTokens int
		var usageConfidence float64
		var cacheReadCost, cacheCreationCost int64
		var createdAt time.Time
		if err := rows.Scan(
			&id, &requestID, &model, &totalTokens, &promptTokens, &completionTokens,
			&estimatedTokens, &statusCode, &usageSource, &usageConfidence,
			&uncachedPromptTokens, &cacheReadTokens, &cacheCreationTokens,
			&cacheUsageSchema, &cacheUsageStatus, &pricingVersion,
			&cacheReadCost, &cacheCreationCost, &createdAt,
		); err != nil {
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
			ID:                      id,
			RequestID:               requestID,
			Model:                   model,
			EstTokens:               estimatedTokens,
			ActualTokens:            totalTokens,
			Delta:                   variance,
			UsageSource:             usageSource,
			Variance:                variance,
			VarianceRate:            metering.VarianceRatio(estimatedTokens, totalTokens),
			Confidence:              usageConfidence,
			UncachedPromptTokens:    uncachedPromptTokens,
			CacheReadTokens:         cacheReadTokens,
			CacheCreationTokens:     cacheCreationTokens,
			CacheUsageSchema:        cacheUsageSchema,
			CacheUsageStatus:        cacheUsageStatus,
			PricingVersion:          pricingVersion,
			CacheReadCostMicros:     cacheReadCost,
			CacheCreationCostMicros: cacheCreationCost,
			Currency:                "CNY", AmountScale: 1_000_000,
			Status:    status,
			CreatedAt: createdAt.Format(time.RFC3339),
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
	summary.Currency, summary.AmountScale = "CNY", 1_000_000
	if summary.EstimatedUsageAlert {
		h.emitEstimatedUsageRatioEvent(r, orgID, summary, map[string]string{
			"model":        modelFilter,
			"status":       statusFilter,
			"usage_source": metering.NormalizeUsageSource(usageSourceFilter),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": records, "total": total, "page": page, "page_size": pageSize,
		"summary": summary, "org_id": orgID,
	})
}

func (h *Handler) reconciliationSummary(ctx context.Context, where string, args []interface{}) (reconciliationSummary, error) {
	query := fmt.Sprintf(
		`SELECT
		    COALESCE(SUM(CASE WHEN status_code < 400 AND usage_source IN ('estimated', 'adapter_estimated', 'counter_v2') THEN total_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 AND usage_source IN ('upstream', 'api', '') THEN total_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 THEN COALESCE(NULLIF(estimated_tokens, 0), prompt_tokens + completion_tokens) ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 THEN total_tokens ELSE 0 END), 0),
		    COALESCE(AVG(CASE WHEN status_code < 400 THEN usage_confidence END), 0)::float8,
		    COALESCE(SUM(CASE WHEN status_code < 400 THEN cache_read_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 THEN cache_creation_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 THEN cache_read_cost_micros ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 THEN cache_creation_cost_micros ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 AND cache_usage_schema <> '' THEN 1 ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status_code < 400 AND cache_usage_schema = '' THEN 1 ELSE 0 END), 0)
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
		&summary.CacheReadTokens,
		&summary.CacheCreationTokens,
		&summary.CacheReadCostMicros,
		&summary.CacheCreationCostMicros,
		&summary.AuthoritativeCacheRows,
		&summary.UnreportedCacheRows,
	); err != nil {
		return summary, err
	}
	summary.VarianceTokens = summary.ActualTokens - summary.PreflightEstimatedTokens
	summary.VarianceRatio = metering.VarianceRatio(int(summary.PreflightEstimatedTokens), int(summary.ActualTokens))
	return summary, nil
}

func (h *Handler) emitEstimatedUsageRatioEvent(r *http.Request, orgID int64, summary reconciliationSummary, filters map[string]string) {
	if h.eventService == nil {
		return
	}
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
		`SELECT id, model_name, input_price_cents::text, output_price_cents::text,
		        cache_read_ratio::text, cache_creation_ratio::text, updated_at
		 FROM model_prices ORDER BY model_name LIMIT $1 OFFSET $2`,
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
		var inputPrice, outputPrice, cacheReadRatio, cacheCreationRatio string
		var updatedAt time.Time
		if err := rows.Scan(&id, &modelName, &inputPrice, &outputPrice, &cacheReadRatio, &cacheCreationRatio, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		inputPublic, inputOK := billing.InternalPriceCentsToCNYPerMillion(inputPrice)
		outputPublic, outputOK := billing.InternalPriceCentsToCNYPerMillion(outputPrice)
		cacheReadPublic, cacheReadOK := billing.NormalizeCachePriceRatio(cacheReadRatio)
		cacheCreationPublic, cacheCreationOK := billing.NormalizeCachePriceRatio(cacheCreationRatio)
		if !inputOK || !outputOK || !cacheReadOK || !cacheCreationOK {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact model price source"})
			return
		}
		prices = append(prices, map[string]interface{}{
			"id": id, "model_name": modelName,
			"input_price_cny_per_million_tokens":  inputPublic,
			"output_price_cny_per_million_tokens": outputPublic,
			"cache_read_ratio":                    cacheReadPublic, "cache_creation_ratio": cacheCreationPublic,
			"currency": "CNY", "price_basis_tokens": billing.PublicPriceBasisTokens, "updated_at": updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": prices, "total": total, "page": page, "page_size": pageSize,
		"can_mutate": auth.IsPlatformAdmin(actorRole(r.Context())),
	})
}

type exactDecimalInput string

func (d *exactDecimalInput) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return errors.New("exact decimal is required")
	}
	var value string
	if data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	} else {
		value = string(data)
	}
	*d = exactDecimalInput(value)
	return nil
}

func (d exactDecimalInput) String() string { return string(d) }

type modelPriceMutation struct {
	ModelName                *string            `json:"model_name"`
	InputPriceCNYPerMillion  *exactDecimalInput `json:"input_price_cny_per_million_tokens"`
	OutputPriceCNYPerMillion *exactDecimalInput `json:"output_price_cny_per_million_tokens"`
	CacheReadRatio           *exactDecimalInput `json:"cache_read_ratio"`
	CacheCreationRatio       *exactDecimalInput `json:"cache_creation_ratio"`
}

func decodeModelPriceMutation(w http.ResponseWriter, r *http.Request) (modelPriceMutation, bool) {
	var req modelPriceMutation
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model price payload"})
		return modelPriceMutation{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Only one model price object is allowed"})
		return modelPriceMutation{}, false
	}
	return req, true
}

func normalizePublicModelPrice(value *exactDecimalInput) (string, bool) {
	if value == nil {
		return "", false
	}
	return billing.CNYPerMillionToInternalPriceCents(value.String())
}

func (h *Handler) CreateModelPrice(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformPriceMutation(w, r) {
		return
	}
	req, ok := decodeModelPriceMutation(w, r)
	if !ok {
		return
	}
	if req.ModelName == nil || strings.TrimSpace(*req.ModelName) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model_name required"})
		return
	}
	modelName := strings.TrimSpace(*req.ModelName)
	inputPriceCents, inputOK := normalizePublicModelPrice(req.InputPriceCNYPerMillion)
	outputPriceCents, outputOK := normalizePublicModelPrice(req.OutputPriceCNYPerMillion)
	if !inputOK || !outputOK {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "CNY-per-million prices are required, nonnegative, and support at most 5 decimal places"})
		return
	}
	cacheReadRatio, cacheReadOK := normalizeOptionalCachePriceRatio(req.CacheReadRatio)
	cacheCreationRatio, cacheCreationOK := normalizeOptionalCachePriceRatio(req.CacheCreationRatio)
	if !cacheReadOK || !cacheCreationOK {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "cache ratios must be between 0 and 100"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	if err := lockPriceMutationKeys(r.Context(), tx,
		effectiveModelPriceLockKey(modelName),
		"model_price:"+modelName,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lock failed"})
		return
	}
	before, beforeFound, err := loadModelPriceByName(r.Context(), tx, modelName, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lookup failed"})
		return
	}

	var after modelPriceAuditRecord
	err = tx.QueryRow(r.Context(),
		`INSERT INTO model_prices (
			model_name, input_price_cents, output_price_cents,
			cache_read_ratio, cache_creation_ratio
		 ) VALUES ($1, $2::numeric, $3::numeric, COALESCE($4::numeric, 1), COALESCE($5::numeric, 1))
		 ON CONFLICT (model_name) DO UPDATE SET
			input_price_cents = $2,
			output_price_cents = $3,
			cache_read_ratio = COALESCE($4::numeric, model_prices.cache_read_ratio),
			cache_creation_ratio = COALESCE($5::numeric, model_prices.cache_creation_ratio),
			updated_at = NOW()
		 RETURNING id, model_name, input_price_cents::text, output_price_cents::text,
		           cache_read_ratio::text, cache_creation_ratio::text`,
		modelName, inputPriceCents, outputPriceCents, cacheReadRatio, cacheCreationRatio,
	).Scan(&after.ID, &after.ModelName, &after.InputPriceCents, &after.OutputPriceCents, &after.CacheReadRatio, &after.CacheCreationRatio)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Insert failed"})
		return
	}
	if ok, err := effectiveModelPriceFits(r.Context(), tx, after.ModelName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Effective price validation failed"})
		return
	} else if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "effective price exceeds the supported micro-CNY range after cache and organization multipliers"})
		return
	}
	afterState, err := after.auditState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact price readback"})
		return
	}
	detail := map[string]interface{}{"scope": "platform_global", "after": afterState}
	if beforeFound {
		beforeState, stateErr := before.auditState()
		if stateErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact existing price"})
			return
		}
		detail["before"] = beforeState
	}
	if err := insertAuditEntry(r.Context(), tx, "model_price.upsert", "model_price", strconv.FormatInt(after.ID, 10), detail, pricingAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()))); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	writeJSON(w, http.StatusCreated, afterState)
}

func (h *Handler) UpdateModelPrice(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformPriceMutation(w, r) {
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	req, ok := decodeModelPriceMutation(w, r)
	if !ok {
		return
	}
	var sets []string
	var args []interface{}
	argIdx := 1
	if req.ModelName != nil {
		modelName := strings.TrimSpace(*req.ModelName)
		if modelName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model_name must not be empty"})
			return
		}
		sets = append(sets, fmt.Sprintf("model_name = $%d", argIdx))
		args = append(args, modelName)
		argIdx++
	}
	if req.InputPriceCNYPerMillion != nil {
		inputPriceCents, valid := normalizePublicModelPrice(req.InputPriceCNYPerMillion)
		if !valid {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "input_price_cny_per_million_tokens must be nonnegative with at most 5 decimal places"})
			return
		}
		sets = append(sets, fmt.Sprintf("input_price_cents = $%d::numeric", argIdx))
		args = append(args, inputPriceCents)
		argIdx++
	}
	if req.OutputPriceCNYPerMillion != nil {
		outputPriceCents, valid := normalizePublicModelPrice(req.OutputPriceCNYPerMillion)
		if !valid {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "output_price_cny_per_million_tokens must be nonnegative with at most 5 decimal places"})
			return
		}
		sets = append(sets, fmt.Sprintf("output_price_cents = $%d::numeric", argIdx))
		args = append(args, outputPriceCents)
		argIdx++
	}
	if req.CacheReadRatio != nil {
		cacheReadRatio, valid := billing.NormalizeCachePriceRatio(req.CacheReadRatio.String())
		if !valid {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "cache_read_ratio must be between 0 and 100"})
			return
		}
		sets = append(sets, fmt.Sprintf("cache_read_ratio = $%d::numeric", argIdx))
		args = append(args, cacheReadRatio)
		argIdx++
	}
	if req.CacheCreationRatio != nil {
		cacheCreationRatio, valid := billing.NormalizeCachePriceRatio(req.CacheCreationRatio.String())
		if !valid {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "cache_creation_ratio must be between 0 and 100"})
			return
		}
		sets = append(sets, fmt.Sprintf("cache_creation_ratio = $%d::numeric", argIdx))
		args = append(args, cacheCreationRatio)
		argIdx++
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	if err := lockPriceMutationKey(r.Context(), tx, fmt.Sprintf("model_price_id:%d", id)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lock failed"})
		return
	}
	probe, found, err := loadModelPriceByID(r.Context(), tx, id, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lookup failed"})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}
	modelNames := []string{probe.ModelName}
	if req.ModelName != nil {
		modelNames = append(modelNames, strings.TrimSpace(*req.ModelName))
	}
	lockKeys := make([]string, 0, len(modelNames)*2)
	for _, modelName := range modelNames {
		lockKeys = append(lockKeys, effectiveModelPriceLockKey(modelName), "model_price:"+modelName)
	}
	if err := lockPriceMutationKeys(r.Context(), tx, lockKeys...); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lock failed"})
		return
	}
	before, found, err := loadModelPriceByID(r.Context(), tx, id, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lookup failed"})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE model_prices SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := tx.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}
	after, found, err := loadModelPriceByID(r.Context(), tx, id, false)
	if err != nil || !found {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Readback failed"})
		return
	}
	if ok, err := effectiveModelPriceFits(r.Context(), tx, after.ModelName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Effective price validation failed"})
		return
	} else if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "effective price exceeds the supported micro-CNY range after cache and organization multipliers"})
		return
	}
	beforeState, err := before.auditState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact existing price"})
		return
	}
	afterState, err := after.auditState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact price readback"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "model_price.update", "model_price", idStr, map[string]interface{}{
		"scope": "platform_global", "before": beforeState, "after": afterState,
	}, pricingAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()))); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteModelPrice(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformPriceMutation(w, r) {
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var before modelPriceAuditRecord
	err = tx.QueryRow(r.Context(),
		`DELETE FROM model_prices WHERE id = $1
		 RETURNING id, model_name, input_price_cents::text, output_price_cents::text,
		           cache_read_ratio::text, cache_creation_ratio::text`, id,
	).Scan(&before.ID, &before.ModelName, &before.InputPriceCents, &before.OutputPriceCents, &before.CacheReadRatio, &before.CacheCreationRatio)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	beforeState, err := before.auditState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact existing price"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "model_price.delete", "model_price", idStr, map[string]interface{}{
		"scope": "platform_global", "before": beforeState,
	}, pricingAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()))); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) ListOrgModelPrices(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}

	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	var total int
	err = h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM org_model_prices WHERE org_id = $1`, orgID).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT omp.id, omp.org_id, o.name, omp.model_name, omp.multiplier::text, omp.created_at, omp.updated_at
		 FROM org_model_prices omp
		 JOIN orgs o ON o.id = omp.org_id
		 WHERE omp.org_id = $1 ORDER BY omp.model_name
		 LIMIT $2 OFFSET $3`, orgID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, itemOrgID int64
		var orgName, modelName string
		var multiplier string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &itemOrgID, &orgName, &modelName, &multiplier, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		multiplier, valid := billing.NormalizeOrgModelPriceMultiplier(multiplier)
		if !valid {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact organization multiplier source"})
			return
		}
		items = append(items, map[string]interface{}{
			"id": id, "org_id": itemOrgID, "org_name": orgName, "model_name": modelName,
			"multiplier": multiplier, "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": items, "items": items, "total": total, "page": page, "page_size": pageSize,
		"org_id": orgID, "can_mutate": auth.IsPlatformAdmin(actorRole(r.Context())),
	})
}

func (h *Handler) CreateOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformPriceMutation(w, r) {
		return
	}
	var req struct {
		OrgID      int64             `json:"org_id"`
		ModelName  string            `json:"model_name"`
		Multiplier exactDecimalInput `json:"multiplier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.OrgID <= 0 || req.ModelName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "org_id and model_name required"})
		return
	}
	multiplier, valid := billing.NormalizeOrgModelPriceMultiplier(req.Multiplier.String())
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "multiplier must be between 0.001 and 99.999"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	if err := lockPriceMutationKeys(r.Context(), tx,
		effectiveModelPriceLockKey(req.ModelName),
		fmt.Sprintf("org_model_price:%d:%s", req.OrgID, req.ModelName),
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lock failed"})
		return
	}
	before, beforeFound, err := loadOrgModelPriceByOrgModel(r.Context(), tx, req.OrgID, req.ModelName, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lookup failed"})
		return
	}
	var after orgModelPriceAuditRecord
	err = tx.QueryRow(r.Context(),
		`INSERT INTO org_model_prices (org_id, model_name, multiplier)
		 VALUES ($1, $2, $3::numeric)
		 ON CONFLICT (org_id, model_name) DO UPDATE SET multiplier = $3::numeric, updated_at = NOW()
		 RETURNING id, org_id, model_name, multiplier::text`,
		req.OrgID, req.ModelName, multiplier,
	).Scan(&after.ID, &after.OrgID, &after.ModelName, &after.Multiplier)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	if ok, err := effectiveModelPriceFits(r.Context(), tx, after.ModelName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Effective price validation failed"})
		return
	} else if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "effective price exceeds the supported micro-CNY range after cache and organization multipliers"})
		return
	}
	afterState, err := after.auditState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact multiplier readback"})
		return
	}
	detail := map[string]interface{}{"scope": "organization", "after": afterState}
	if beforeFound {
		beforeState, stateErr := before.auditState()
		if stateErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact existing multiplier"})
			return
		}
		detail["before"] = beforeState
	}
	if err := insertAuditEntry(r.Context(), tx, "org_model_price.upsert", "org_model_price", strconv.FormatInt(after.ID, 10), detail, pricingAuditMeta(r, after.OrgID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	afterState["success"] = true
	writeJSON(w, http.StatusCreated, afterState)
}

func (h *Handler) UpdateOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformPriceMutation(w, r) {
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		Multiplier *exactDecimalInput `json:"multiplier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.Multiplier == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "multiplier must be between 0.001 and 99.999"})
		return
	}
	multiplier, valid := billing.NormalizeOrgModelPriceMultiplier(req.Multiplier.String())
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "multiplier must be between 0.001 and 99.999"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := lockPriceMutationKey(r.Context(), tx, fmt.Sprintf("org_model_price_id:%d", id)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lock failed"})
		return
	}
	probe, found, err := loadOrgModelPriceByID(r.Context(), tx, id, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lookup failed"})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	if err := lockPriceMutationKeys(r.Context(), tx,
		effectiveModelPriceLockKey(probe.ModelName),
		fmt.Sprintf("org_model_price:%d:%s", probe.OrgID, probe.ModelName),
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lock failed"})
		return
	}
	before, found, err := loadOrgModelPriceByID(r.Context(), tx, id, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lookup failed"})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	var after orgModelPriceAuditRecord
	err = tx.QueryRow(r.Context(),
		`UPDATE org_model_prices SET multiplier = $1::numeric, updated_at = NOW() WHERE id = $2
		 RETURNING id, org_id, model_name, multiplier::text`,
		multiplier, id,
	).Scan(&after.ID, &after.OrgID, &after.ModelName, &after.Multiplier)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if ok, err := effectiveModelPriceFits(r.Context(), tx, after.ModelName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Effective price validation failed"})
		return
	} else if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "effective price exceeds the supported micro-CNY range after cache and organization multipliers"})
		return
	}
	beforeState, err := before.auditState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact existing multiplier"})
		return
	}
	afterState, err := after.auditState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact multiplier readback"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "org_model_price.update", "org_model_price", idStr, map[string]interface{}{
		"scope": "organization", "before": beforeState, "after": afterState,
	}, pricingAuditMeta(r, after.OrgID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformPriceMutation(w, r) {
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var before orgModelPriceAuditRecord
	err = tx.QueryRow(r.Context(),
		`DELETE FROM org_model_prices WHERE id = $1
		 RETURNING id, org_id, model_name, multiplier::text`, id,
	).Scan(&before.ID, &before.OrgID, &before.ModelName, &before.Multiplier)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	beforeState, err := before.auditState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Invalid exact existing multiplier"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "org_model_price.delete", "org_model_price", idStr, map[string]interface{}{
		"scope": "organization", "before": beforeState,
	}, pricingAuditMeta(r, before.OrgID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

type modelPriceAuditRecord struct {
	ID                 int64
	ModelName          string
	InputPriceCents    string
	OutputPriceCents   string
	CacheReadRatio     string
	CacheCreationRatio string
}

func (p modelPriceAuditRecord) auditState() (map[string]interface{}, error) {
	inputPrice, inputOK := billing.InternalPriceCentsToCNYPerMillion(p.InputPriceCents)
	outputPrice, outputOK := billing.InternalPriceCentsToCNYPerMillion(p.OutputPriceCents)
	cacheReadRatio, cacheReadOK := billing.NormalizeCachePriceRatio(p.CacheReadRatio)
	cacheCreationRatio, cacheCreationOK := billing.NormalizeCachePriceRatio(p.CacheCreationRatio)
	if !inputOK || !outputOK || !cacheReadOK || !cacheCreationOK {
		return nil, errors.New("invalid exact model price source")
	}
	return map[string]interface{}{
		"id": p.ID, "model_name": p.ModelName,
		"input_price_cny_per_million_tokens":  inputPrice,
		"output_price_cny_per_million_tokens": outputPrice,
		"currency":                            "CNY", "price_basis_tokens": billing.PublicPriceBasisTokens,
		"cache_read_ratio": cacheReadRatio, "cache_creation_ratio": cacheCreationRatio,
	}, nil
}

type orgModelPriceAuditRecord struct {
	ID         int64
	OrgID      int64
	ModelName  string
	Multiplier string
}

func (p orgModelPriceAuditRecord) auditState() (map[string]interface{}, error) {
	multiplier, ok := billing.NormalizeOrgModelPriceMultiplier(p.Multiplier)
	if !ok {
		return nil, errors.New("invalid exact organization multiplier source")
	}
	return map[string]interface{}{
		"id": p.ID, "org_id": p.OrgID, "model_name": p.ModelName, "multiplier": multiplier,
	}, nil
}

type pricingQueryRower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func loadModelPriceByID(ctx context.Context, q pricingQueryRower, id int64, forUpdate bool) (modelPriceAuditRecord, bool, error) {
	query := `SELECT id, model_name, input_price_cents::text, output_price_cents::text,
	                 cache_read_ratio::text, cache_creation_ratio::text
	          FROM model_prices WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var price modelPriceAuditRecord
	err := q.QueryRow(ctx, query, id).Scan(&price.ID, &price.ModelName, &price.InputPriceCents, &price.OutputPriceCents, &price.CacheReadRatio, &price.CacheCreationRatio)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelPriceAuditRecord{}, false, nil
	}
	return price, err == nil, err
}

func loadModelPriceByName(ctx context.Context, q pricingQueryRower, modelName string, forUpdate bool) (modelPriceAuditRecord, bool, error) {
	query := `SELECT id, model_name, input_price_cents::text, output_price_cents::text,
	                 cache_read_ratio::text, cache_creation_ratio::text
	          FROM model_prices WHERE model_name = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var price modelPriceAuditRecord
	err := q.QueryRow(ctx, query, modelName).Scan(&price.ID, &price.ModelName, &price.InputPriceCents, &price.OutputPriceCents, &price.CacheReadRatio, &price.CacheCreationRatio)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelPriceAuditRecord{}, false, nil
	}
	return price, err == nil, err
}

func loadOrgModelPriceByID(ctx context.Context, q pricingQueryRower, id int64, forUpdate bool) (orgModelPriceAuditRecord, bool, error) {
	query := `SELECT id, org_id, model_name, multiplier::text FROM org_model_prices WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var price orgModelPriceAuditRecord
	err := q.QueryRow(ctx, query, id).Scan(&price.ID, &price.OrgID, &price.ModelName, &price.Multiplier)
	if errors.Is(err, pgx.ErrNoRows) {
		return orgModelPriceAuditRecord{}, false, nil
	}
	return price, err == nil, err
}

func loadOrgModelPriceByOrgModel(ctx context.Context, q pricingQueryRower, orgID int64, modelName string, forUpdate bool) (orgModelPriceAuditRecord, bool, error) {
	query := `SELECT id, org_id, model_name, multiplier::text FROM org_model_prices WHERE org_id = $1 AND model_name = $2`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var price orgModelPriceAuditRecord
	err := q.QueryRow(ctx, query, orgID, modelName).Scan(&price.ID, &price.OrgID, &price.ModelName, &price.Multiplier)
	if errors.Is(err, pgx.ErrNoRows) {
		return orgModelPriceAuditRecord{}, false, nil
	}
	return price, err == nil, err
}

func requirePlatformPriceMutation(w http.ResponseWriter, r *http.Request) bool {
	if auth.IsPlatformAdmin(actorRole(r.Context())) {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
	return false
}

func validOrgModelPriceMultiplier(multiplier string) bool {
	_, ok := billing.NormalizeOrgModelPriceMultiplier(multiplier)
	return ok
}

func validCachePriceRatio(ratio string) bool {
	_, ok := billing.NormalizeCachePriceRatio(ratio)
	return ok
}

func normalizeOptionalCachePriceRatio(value *exactDecimalInput) (interface{}, bool) {
	if value == nil {
		return nil, true
	}
	normalized, ok := billing.NormalizeCachePriceRatio(value.String())
	if !ok {
		return nil, false
	}
	return normalized, true
}

func lockPriceMutationKey(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key)
	return err
}

func lockPriceMutationKeys(ctx context.Context, tx pgx.Tx, keys ...string) error {
	unique := make(map[string]struct{}, len(keys))
	ordered := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, exists := unique[key]; exists {
			continue
		}
		unique[key] = struct{}{}
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		if err := lockPriceMutationKey(ctx, tx, key); err != nil {
			return err
		}
	}
	return nil
}

func effectiveModelPriceLockKey(modelName string) string {
	return "effective_model_price:" + modelName
}

// effectiveModelPriceFits mirrors the exact half-up price conversion in
// money.PriceMicrosPerMillionFromInternal. Every base/cache price must fit an
// int64 both at the default multiplier and at every organization override.
func effectiveModelPriceFits(ctx context.Context, q pricingQueryRower, modelName string) (bool, error) {
	var overflow bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM model_prices mp
			CROSS JOIN LATERAL (
				SELECT 1::numeric AS multiplier
				UNION ALL
				SELECT omp.multiplier
				FROM org_model_prices omp
				WHERE omp.model_name = mp.model_name
			) effective
			WHERE mp.model_name = $1
			  AND (
				round(mp.input_price_cents * effective.multiplier * 10000000) > 9223372036854775807
				OR round(mp.input_price_cents * mp.cache_read_ratio * effective.multiplier * 10000000) > 9223372036854775807
				OR round(mp.input_price_cents * mp.cache_creation_ratio * effective.multiplier * 10000000) > 9223372036854775807
				OR round(mp.output_price_cents * effective.multiplier * 10000000) > 9223372036854775807
			  )
		)
	`, modelName).Scan(&overflow)
	return !overflow, err
}

func pricingAuditMeta(r *http.Request, orgID int64) auditMeta {
	return auditMeta{
		OrgID: orgID, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
		RequestID: middleware.GetRequestIDFromContext(r.Context()), Source: auditSourceFromContext(r.Context()),
	}
}
