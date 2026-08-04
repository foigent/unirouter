package admin

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/money"
)

const (
	costOptimizationMetricVersion = "unirouter.cost-optimization.metrics.v1"
	costOptimizationRuleVersion   = "unirouter.cost-optimization.opportunities.v1"
)

type costOptimizationGroup struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type costOptimizationSummary struct {
	MetricVersion                   string   `json:"metric_version"`
	Currency                        string   `json:"currency"`
	AmountScale                     int64    `json:"amount_scale"`
	AmountAuthority                 string   `json:"amount_authority"`
	CandidateRequests               int64    `json:"candidate_requests"`
	EligibleRequests                int64    `json:"eligible_requests"`
	CacheUsedRequests               int64    `json:"cache_used_requests"`
	CacheReadRequests               int64    `json:"cache_read_requests"`
	CacheWriteRequests              int64    `json:"cache_write_requests"`
	PromptTokens                    int64    `json:"prompt_tokens"`
	CacheReadTokens                 int64    `json:"cache_read_tokens"`
	CacheCreationTokens             int64    `json:"cache_creation_tokens"`
	OrdinaryInputBaselineCostMicros string   `json:"ordinary_input_baseline_cost_micros"`
	ActualCostMicros                string   `json:"actual_cost_micros"`
	NetSavingsMicros                string   `json:"net_savings_micros"`
	CacheReadSavingsMicros          string   `json:"cache_read_savings_micros"`
	CacheCreationPremiumMicros      string   `json:"cache_creation_premium_micros"`
	RoundingAdjustmentMicros        string   `json:"rounding_adjustment_micros"`
	EvidenceCoverageRatio           float64  `json:"evidence_coverage_ratio"`
	CacheReadTokenRatio             float64  `json:"cache_read_token_ratio"`
	ReadToCreationTokenRatio        *float64 `json:"read_to_creation_token_ratio"`
	ResponseCacheRuntime            string   `json:"response_cache_runtime"`
	CompressionRuntime              string   `json:"compression_runtime"`
	SupplierCOGSAuthority           string   `json:"supplier_cogs_authority"`
	PromptRetention                 string   `json:"prompt_retention"`
	ResponseRetention               string   `json:"response_retention"`
}

type costOptimizationRow struct {
	Key                             string   `json:"key"`
	CandidateRequests               int64    `json:"candidate_requests"`
	EligibleRequests                int64    `json:"eligible_requests"`
	CacheUsedRequests               int64    `json:"cache_used_requests"`
	PromptTokens                    int64    `json:"prompt_tokens"`
	CacheReadTokens                 int64    `json:"cache_read_tokens"`
	CacheCreationTokens             int64    `json:"cache_creation_tokens"`
	OrdinaryInputBaselineCostMicros string   `json:"ordinary_input_baseline_cost_micros"`
	ActualCostMicros                string   `json:"actual_cost_micros"`
	NetSavingsMicros                string   `json:"net_savings_micros"`
	CacheReadSavingsMicros          string   `json:"cache_read_savings_micros"`
	CacheCreationPremiumMicros      string   `json:"cache_creation_premium_micros"`
	RoundingAdjustmentMicros        string   `json:"rounding_adjustment_micros"`
	EvidenceCoverageRatio           float64  `json:"evidence_coverage_ratio"`
	CacheReadTokenRatio             float64  `json:"cache_read_token_ratio"`
	ReadToCreationTokenRatio        *float64 `json:"read_to_creation_token_ratio"`
}

type costOptimizationTrendRow struct {
	Day                        string  `json:"day"`
	CandidateRequests          int64   `json:"candidate_requests"`
	EligibleRequests           int64   `json:"eligible_requests"`
	CacheReadTokens            int64   `json:"cache_read_tokens"`
	CacheCreationTokens        int64   `json:"cache_creation_tokens"`
	NetSavingsMicros           string  `json:"net_savings_micros"`
	CacheReadSavingsMicros     string  `json:"cache_read_savings_micros"`
	CacheCreationPremiumMicros string  `json:"cache_creation_premium_micros"`
	EvidenceCoverageRatio      float64 `json:"evidence_coverage_ratio"`
}

type costOptimizationOpportunity struct {
	Kind       string                 `json:"kind"`
	Severity   string                 `json:"severity"`
	GroupKey   string                 `json:"group_key"`
	GroupLabel string                 `json:"group_label"`
	Title      string                 `json:"title"`
	Rationale  string                 `json:"rationale"`
	NextAction string                 `json:"next_action"`
	Evidence   map[string]interface{} `json:"evidence"`
}

func costOptimizationGroupBy(value string) (costOptimizationGroup, string, bool) {
	switch strings.TrimSpace(value) {
	case "overall":
		return costOptimizationGroup{Key: "overall", Label: "总体"}, "'overall'", true
	case "", "model":
		return costOptimizationGroup{Key: "model", Label: "模型"}, "COALESCE(NULLIF(model, ''), '(unknown)')", true
	case "project":
		return costOptimizationGroup{Key: "project", Label: "项目"}, "COALESCE(project_id, 0)::text", true
	case "provider":
		return costOptimizationGroup{Key: "provider", Label: "供应商"}, "COALESCE(NULLIF(provider, ''), '(unknown)')", true
	case "channel":
		return costOptimizationGroup{Key: "channel", Label: "上游通道"}, "COALESCE(NULLIF(channel, ''), '(unknown)')", true
	default:
		return costOptimizationGroup{}, "", false
	}
}

func costOptimizationCTE(where string) string {
	return fmt.Sprintf(`WITH filtered AS (
		SELECT * FROM call_logs%s
	), classified AS (
		SELECT *,
			(status_code > 0 AND status_code < 400
			 AND COALESCE(usage_source, '') IN ('upstream', 'api', '')
			 AND prompt_tokens > 0) AS candidate,
			(status_code > 0 AND status_code < 400
			 AND COALESCE(usage_source, '') IN ('upstream', 'api', '')
			 AND prompt_tokens > 0
			 AND cache_usage_schema <> ''
			 AND cache_usage_status <> ''
			 AND cache_usage_status <> 'not_reported'
			 AND pricing_version <> ''
			 AND uncached_prompt_tokens + cache_read_tokens + cache_creation_tokens = prompt_tokens) AS eligible
		FROM filtered
	), scored AS (
		SELECT *,
			CASE WHEN eligible THEN ROUND((
				prompt_tokens::numeric * effective_input_price_micros_per_million::numeric
				+ completion_tokens::numeric * effective_output_price_micros_per_million::numeric
			) / 1000000) ELSE 0::numeric END AS baseline_cost_micros,
			CASE WHEN eligible THEN cost_micros::numeric ELSE 0::numeric END AS actual_cost_micros,
			CASE WHEN eligible THEN ROUND((
				cache_read_tokens::numeric
				* (effective_input_price_micros_per_million::numeric - effective_cache_read_price_micros_per_million::numeric)
			) / 1000000) ELSE 0::numeric END AS cache_read_savings_micros,
			CASE WHEN eligible THEN ROUND((
				cache_creation_tokens::numeric
				* (effective_cache_creation_price_micros_per_million::numeric - effective_input_price_micros_per_million::numeric)
			) / 1000000) ELSE 0::numeric END AS cache_creation_premium_micros
		FROM classified
	)`, where)
}

func costOptimizationWhere(r *http.Request, h *Handler) (string, []interface{}, error) {
	conditions, args, err := h.analyticsConditions(r)
	if err != nil {
		return "", nil, err
	}
	if len(conditions) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args, nil
}

func writeCostOptimizationConditionError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if err.Error() == "Forbidden" || strings.Contains(err.Error(), "project_id is required") {
		status = http.StatusForbidden
	}
	writeJSON(w, status, map[string]interface{}{"error": err.Error()})
}

func costOptimizationRatio(numerator, denominator int64) float64 {
	if numerator <= 0 || denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func optionalCostOptimizationRatio(numerator, denominator int64) *float64 {
	if denominator <= 0 {
		return nil
	}
	ratio := float64(numerator) / float64(denominator)
	return &ratio
}

func scanCostOptimizationSummary(row interface{ Scan(...any) error }) (costOptimizationSummary, error) {
	var result costOptimizationSummary
	if err := row.Scan(
		&result.CandidateRequests,
		&result.EligibleRequests,
		&result.CacheUsedRequests,
		&result.CacheReadRequests,
		&result.CacheWriteRequests,
		&result.PromptTokens,
		&result.CacheReadTokens,
		&result.CacheCreationTokens,
		&result.OrdinaryInputBaselineCostMicros,
		&result.ActualCostMicros,
		&result.NetSavingsMicros,
		&result.CacheReadSavingsMicros,
		&result.CacheCreationPremiumMicros,
		&result.RoundingAdjustmentMicros,
	); err != nil {
		return result, err
	}
	result.MetricVersion = costOptimizationMetricVersion
	result.Currency = "CNY"
	result.AmountScale = money.MicrosPerCNY
	result.AmountAuthority = "request_time_frozen_customer_pricing"
	result.EvidenceCoverageRatio = costOptimizationRatio(result.EligibleRequests, result.CandidateRequests)
	result.CacheReadTokenRatio = costOptimizationRatio(result.CacheReadTokens, result.PromptTokens)
	result.ReadToCreationTokenRatio = optionalCostOptimizationRatio(result.CacheReadTokens, result.CacheCreationTokens)
	result.ResponseCacheRuntime = "not_connected_to_gateway"
	result.CompressionRuntime = "default_off_shadow_not_started"
	result.SupplierCOGSAuthority = "not_claimed_use_provider_reconciliation"
	result.PromptRetention = "not_stored_by_default"
	result.ResponseRetention = "not_stored_by_default"
	return result, nil
}

func costOptimizationAggregateSelect() string {
	return `
		COUNT(*) FILTER (WHERE candidate),
		COUNT(*) FILTER (WHERE eligible),
		COUNT(*) FILTER (WHERE eligible AND (cache_read_tokens > 0 OR cache_creation_tokens > 0)),
		COUNT(*) FILTER (WHERE eligible AND cache_read_tokens > 0),
		COUNT(*) FILTER (WHERE eligible AND cache_creation_tokens > 0),
		COALESCE(SUM(prompt_tokens) FILTER (WHERE eligible), 0),
		COALESCE(SUM(cache_read_tokens) FILTER (WHERE eligible), 0),
		COALESCE(SUM(cache_creation_tokens) FILTER (WHERE eligible), 0),
		COALESCE(SUM(baseline_cost_micros) FILTER (WHERE eligible), 0)::text,
		COALESCE(SUM(actual_cost_micros) FILTER (WHERE eligible), 0)::text,
		COALESCE(SUM(baseline_cost_micros - actual_cost_micros) FILTER (WHERE eligible), 0)::text,
		COALESCE(SUM(cache_read_savings_micros) FILTER (WHERE eligible), 0)::text,
		COALESCE(SUM(cache_creation_premium_micros) FILTER (WHERE eligible), 0)::text,
		COALESCE(SUM(
			(baseline_cost_micros - actual_cost_micros)
			- cache_read_savings_micros
			+ cache_creation_premium_micros
		) FILTER (WHERE eligible), 0)::text`
}

func (h *Handler) CostOptimizationSummary(w http.ResponseWriter, r *http.Request) {
	where, args, err := costOptimizationWhere(r, h)
	if err != nil {
		writeCostOptimizationConditionError(w, err)
		return
	}
	query := costOptimizationCTE(where) + " SELECT " + costOptimizationAggregateSelect() + " FROM scored"
	result, err := scanCostOptimizationSummary(h.db.QueryRow(r.Context(), query, args...))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Cost optimization summary failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"summary": result})
}

func (h *Handler) CostOptimizationTrend(w http.ResponseWriter, r *http.Request) {
	where, args, err := costOptimizationWhere(r, h)
	if err != nil {
		writeCostOptimizationConditionError(w, err)
		return
	}
	query := costOptimizationCTE(where) + `
		SELECT
			TO_CHAR(DATE_TRUNC('day', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'),
			COUNT(*) FILTER (WHERE candidate),
			COUNT(*) FILTER (WHERE eligible),
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE eligible), 0),
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE eligible), 0),
			COALESCE(SUM(baseline_cost_micros - actual_cost_micros) FILTER (WHERE eligible), 0)::text,
			COALESCE(SUM(cache_read_savings_micros) FILTER (WHERE eligible), 0)::text,
			COALESCE(SUM(cache_creation_premium_micros) FILTER (WHERE eligible), 0)::text
		FROM scored
		WHERE candidate
		GROUP BY 1
		ORDER BY 1`
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Cost optimization trend failed"})
		return
	}
	defer rows.Close()
	result := make([]costOptimizationTrendRow, 0)
	for rows.Next() {
		var item costOptimizationTrendRow
		if err := rows.Scan(
			&item.Day,
			&item.CandidateRequests,
			&item.EligibleRequests,
			&item.CacheReadTokens,
			&item.CacheCreationTokens,
			&item.NetSavingsMicros,
			&item.CacheReadSavingsMicros,
			&item.CacheCreationPremiumMicros,
		); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Cost optimization trend failed"})
			return
		}
		item.EvidenceCoverageRatio = costOptimizationRatio(item.EligibleRequests, item.CandidateRequests)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Cost optimization trend failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"metric_version": costOptimizationMetricVersion,
		"currency":       "CNY",
		"amount_scale":   money.MicrosPerCNY,
		"data":           result,
	})
}

func (h *Handler) queryCostOptimizationBreakdown(ctx context.Context, r *http.Request) (costOptimizationGroup, []costOptimizationRow, error) {
	group, groupExpr, ok := costOptimizationGroupBy(r.URL.Query().Get("group_by"))
	if !ok {
		return costOptimizationGroup{}, nil, fmt.Errorf("Invalid group_by")
	}
	where, args, err := costOptimizationWhere(r, h)
	if err != nil {
		return costOptimizationGroup{}, nil, err
	}
	query := costOptimizationCTE(where) + fmt.Sprintf(`
		SELECT
			%s AS group_key,
			COUNT(*) FILTER (WHERE candidate),
			COUNT(*) FILTER (WHERE eligible),
			COUNT(*) FILTER (WHERE eligible AND (cache_read_tokens > 0 OR cache_creation_tokens > 0)),
			COALESCE(SUM(prompt_tokens) FILTER (WHERE eligible), 0),
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE eligible), 0),
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE eligible), 0),
			COALESCE(SUM(baseline_cost_micros) FILTER (WHERE eligible), 0)::text,
			COALESCE(SUM(actual_cost_micros) FILTER (WHERE eligible), 0)::text,
			COALESCE(SUM(baseline_cost_micros - actual_cost_micros) FILTER (WHERE eligible), 0)::text,
			COALESCE(SUM(cache_read_savings_micros) FILTER (WHERE eligible), 0)::text,
			COALESCE(SUM(cache_creation_premium_micros) FILTER (WHERE eligible), 0)::text,
			COALESCE(SUM(
				(baseline_cost_micros - actual_cost_micros)
				- cache_read_savings_micros
				+ cache_creation_premium_micros
			) FILTER (WHERE eligible), 0)::text
		FROM scored
		WHERE candidate
		GROUP BY 1
		ORDER BY ABS(COALESCE(SUM(baseline_cost_micros - actual_cost_micros) FILTER (WHERE eligible), 0)) DESC, 1
		LIMIT 50`, groupExpr)
	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return group, nil, err
	}
	defer rows.Close()
	result := make([]costOptimizationRow, 0)
	for rows.Next() {
		var item costOptimizationRow
		if err := rows.Scan(
			&item.Key,
			&item.CandidateRequests,
			&item.EligibleRequests,
			&item.CacheUsedRequests,
			&item.PromptTokens,
			&item.CacheReadTokens,
			&item.CacheCreationTokens,
			&item.OrdinaryInputBaselineCostMicros,
			&item.ActualCostMicros,
			&item.NetSavingsMicros,
			&item.CacheReadSavingsMicros,
			&item.CacheCreationPremiumMicros,
			&item.RoundingAdjustmentMicros,
		); err != nil {
			return group, nil, err
		}
		item.EvidenceCoverageRatio = costOptimizationRatio(item.EligibleRequests, item.CandidateRequests)
		item.CacheReadTokenRatio = costOptimizationRatio(item.CacheReadTokens, item.PromptTokens)
		item.ReadToCreationTokenRatio = optionalCostOptimizationRatio(item.CacheReadTokens, item.CacheCreationTokens)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return group, nil, err
	}
	return group, result, nil
}

func (h *Handler) CostOptimizationBreakdown(w http.ResponseWriter, r *http.Request) {
	group, result, err := h.queryCostOptimizationBreakdown(r.Context(), r)
	if err != nil {
		if err.Error() == "Invalid group_by" || err.Error() == "Forbidden" || strings.Contains(err.Error(), "project_id is required") {
			writeCostOptimizationConditionError(w, err)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Cost optimization breakdown failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"metric_version": costOptimizationMetricVersion,
		"currency":       "CNY",
		"amount_scale":   money.MicrosPerCNY,
		"group_by":       group,
		"data":           result,
	})
}

func costOptimizationDecimalSign(value string) int {
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return 0
	}
	return parsed.Sign()
}

func costOptimizationOpportunities(group costOptimizationGroup, rows []costOptimizationRow) []costOptimizationOpportunity {
	result := make([]costOptimizationOpportunity, 0)
	for _, row := range rows {
		gap := row.CandidateRequests - row.EligibleRequests
		if gap > 0 {
			result = append(result, costOptimizationOpportunity{
				Kind:       "evidence_gap",
				Severity:   "warning",
				GroupKey:   row.Key,
				GroupLabel: group.Label,
				Title:      "补齐 Prompt Cache 计量证据",
				Rationale:  "存在成功上游请求，但缺少供应商缓存分区、请求时计价版本或精确 Token 分区，当前不能把它们计入节省。",
				NextAction: "先检查请求日志中的计量来源、缓存回传状态和计价版本；不要把缺失证据按零节省处理。",
				Evidence: map[string]interface{}{
					"candidate_requests": row.CandidateRequests,
					"eligible_requests":  row.EligibleRequests,
					"gap_requests":       gap,
					"coverage_ratio":     row.EvidenceCoverageRatio,
				},
			})
		}
		if costOptimizationDecimalSign(row.NetSavingsMicros) < 0 {
			result = append(result, costOptimizationOpportunity{
				Kind:       "negative_net_savings",
				Severity:   "high",
				GroupKey:   row.Key,
				GroupLabel: group.Label,
				Title:      "缓存写入成本高于已实现读取节省",
				Rationale:  "按请求时冻结客户费率计算，当前筛选窗口的 Prompt Cache 净节省为负。",
				NextAction: "检查稳定前缀是否被重复读取、缓存 TTL 与 Provider 写入倍率；任何策略变更仍需在模型治理中显式评审。",
				Evidence: map[string]interface{}{
					"net_savings_micros":            row.NetSavingsMicros,
					"cache_read_savings_micros":     row.CacheReadSavingsMicros,
					"cache_creation_premium_micros": row.CacheCreationPremiumMicros,
				},
			})
		}
		if row.CacheCreationTokens > 0 && row.CacheReadTokens < row.CacheCreationTokens {
			result = append(result, costOptimizationOpportunity{
				Kind:       "low_read_amortization",
				Severity:   "review",
				GroupKey:   row.Key,
				GroupLabel: group.Label,
				Title:      "缓存读取尚未摊薄写入 Token",
				Rationale:  "缓存读取 Token 少于缓存写入 Token；这只是可复核候选，不代表请求内容必然适合缓存。",
				NextAction: "在不读取或保留明文 Prompt 的前提下，按模型与通道核查复用窗口和业务重复模式。",
				Evidence: map[string]interface{}{
					"cache_read_tokens":      row.CacheReadTokens,
					"cache_creation_tokens":  row.CacheCreationTokens,
					"read_to_creation_ratio": row.ReadToCreationTokenRatio,
				},
			})
		}
	}
	severityRank := map[string]int{"high": 0, "warning": 1, "review": 2}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := severityRank[result[i].Severity], severityRank[result[j].Severity]
		if left != right {
			return left < right
		}
		if result[i].GroupKey != result[j].GroupKey {
			return result[i].GroupKey < result[j].GroupKey
		}
		return result[i].Kind < result[j].Kind
	})
	if len(result) > 30 {
		return result[:30]
	}
	return result
}

func (h *Handler) CostOptimizationOpportunities(w http.ResponseWriter, r *http.Request) {
	group, rows, err := h.queryCostOptimizationBreakdown(r.Context(), r)
	if err != nil {
		if err.Error() == "Invalid group_by" || err.Error() == "Forbidden" || strings.Contains(err.Error(), "project_id is required") {
			writeCostOptimizationConditionError(w, err)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Cost optimization opportunities failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rules_version":  costOptimizationRuleVersion,
		"generated_from": "terminal_request_metering_without_prompt_or_response_content",
		"group_by":       group,
		"data":           costOptimizationOpportunities(group, rows),
	})
}
