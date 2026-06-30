package admin

import (
	"context"
	"net/http"
	"time"
)

func (h *Handler) DashboardStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Optional time range params; default to last 24h
	startTime := r.URL.Query().Get("start_time")
	endTime := r.URL.Query().Get("end_time")
	if startTime == "" {
		startTime = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	}
	if endTime == "" {
		endTime = time.Now().Format(time.RFC3339)
	}

	totalRequests, todayCostCents, avgLatencyMs, successRate := h.dashboardOverview(ctx, startTime, endTime)
	recentLogs := h.dashboardRecentLogs(ctx)
	modelPrices := h.dashboardModelPrices(ctx)
	dailyTrend := h.dashboardDailyTrend(ctx, startTime, endTime)
	walletBalance, walletGranted, walletFrozen, walletThreshold := h.dashboardWalletInfo(ctx)
	modelDistribution := h.dashboardModelDistribution(ctx, startTime, endTime)
	channelHealth := h.dashboardChannelHealth(ctx, startTime, endTime)
	deltaCost, deltaLatency, deltaRate := h.dashboardYesterdayDelta(ctx, todayCostCents, avgLatencyMs, successRate)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_requests":     totalRequests,
		"today_cost_cents":   todayCostCents,
		"avg_latency_ms":     avgLatencyMs,
		"success_rate":       successRate,
		"recent_logs":        recentLogs,
		"model_prices":       modelPrices,
		"daily_trend":        dailyTrend,
		"wallet_balance":     walletBalance,
		"wallet_granted":     walletGranted,
		"wallet_frozen":      walletFrozen,
		"wallet_threshold":   walletThreshold,
		"model_distribution": modelDistribution,
		"channel_health":     channelHealth,
		"delta_vs_yesterday": map[string]interface{}{
			"cost_cents":   deltaCost,
			"latency_ms":   deltaLatency,
			"success_rate": deltaRate,
		},
	})
}

// dashboardOverview returns total requests, period cost, avg latency, and success rate.
func (h *Handler) dashboardOverview(ctx context.Context, startTime, endTime string) (totalRequests, todayCostCents int, avgLatencyMs float64, successRate float64) {
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs`).Scan(&totalRequests)
	h.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_cents), 0) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		startTime, endTime,
	).Scan(&todayCostCents)
	h.db.QueryRow(ctx,
		`SELECT COALESCE(AVG(latency_ms), 0) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		startTime, endTime,
	).Scan(&avgLatencyMs)

	var successCount, totalCount int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM call_logs WHERE created_at >= $1 AND created_at <= $2 AND status_code >= 200 AND status_code < 300`,
		startTime, endTime,
	).Scan(&successCount)
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		startTime, endTime,
	).Scan(&totalCount)
	if totalCount > 0 {
		successRate = float64(successCount) / float64(totalCount)
	}
	return
}

// dashboardRecentLogs returns the last 10 call logs.
func (h *Handler) dashboardRecentLogs(ctx context.Context) []map[string]interface{} {
	rows, err := h.db.Query(ctx,
		`SELECT request_id, model, status_code, latency_ms, cost_cents, created_at
		 FROM call_logs ORDER BY created_at DESC LIMIT 10`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	logs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var requestID, model string
		var statusCode, costCents, latencyMs int
		var createdAt time.Time
		rows.Scan(&requestID, &model, &statusCode, &latencyMs, &costCents, &createdAt)
		logs = append(logs, map[string]interface{}{
			"request_id": requestID, "model": model, "status_code": statusCode,
			"latency_ms": latencyMs, "cost_cents": costCents, "created_at": createdAt,
		})
	}
	return logs
}

// dashboardModelPrices returns all model prices.
func (h *Handler) dashboardModelPrices(ctx context.Context) []map[string]interface{} {
	rows, err := h.db.Query(ctx,
		`SELECT model_name, input_price_cents, output_price_cents FROM model_prices ORDER BY model_name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	prices := make([]map[string]interface{}, 0)
	for rows.Next() {
		var modelName string
		var inputPrice, outputPrice int
		rows.Scan(&modelName, &inputPrice, &outputPrice)
		prices = append(prices, map[string]interface{}{
			"model_name": modelName, "input_price_cents": inputPrice, "output_price_cents": outputPrice,
		})
	}
	return prices
}

// dashboardDailyTrend returns 7-day daily request count and cost.
func (h *Handler) dashboardDailyTrend(ctx context.Context, startTime, endTime string) []map[string]interface{} {
	rows, err := h.db.Query(ctx,
		`SELECT DATE(created_at) AS day, COUNT(*) AS requests, COALESCE(SUM(cost_cents), 0) AS cost
		 FROM call_logs
		 WHERE created_at >= $1 AND created_at <= $2
		 GROUP BY DATE(created_at)
		 ORDER BY day ASC`, startTime, endTime)
	if err != nil {
		return nil
	}
	defer rows.Close()
	trend := make([]map[string]interface{}, 0)
	for rows.Next() {
		var day time.Time
		var requests, cost int
		rows.Scan(&day, &requests, &cost)
		trend = append(trend, map[string]interface{}{
			"date": day.Format("2006-01-02"), "requests": requests, "cost_cents": cost,
		})
	}
	return trend
}

// dashboardWalletInfo returns wallet balance, granted, frozen, and threshold.
func (h *Handler) dashboardWalletInfo(ctx context.Context) (balance, granted, frozen, threshold int) {
	h.db.QueryRow(ctx,
		`SELECT COALESCE(balance,0), COALESCE(granted_balance,0), COALESCE(frozen,0), COALESCE(low_balance_threshold_cents,0)
		 FROM wallets ORDER BY org_id LIMIT 1`,
	).Scan(&balance, &granted, &frozen, &threshold)
	return
}

// dashboardModelDistribution returns top 5 models by request count in the time range.
func (h *Handler) dashboardModelDistribution(ctx context.Context, startTime, endTime string) []map[string]interface{} {
	rows, err := h.db.Query(ctx,
		`SELECT model, COUNT(*) as cnt
		 FROM call_logs
		 WHERE created_at >= $1 AND created_at <= $2
		 GROUP BY model ORDER BY cnt DESC LIMIT 5`, startTime, endTime)
	if err != nil {
		return nil
	}
	defer rows.Close()
	dist := make([]map[string]interface{}, 0)
	for rows.Next() {
		var model string
		var cnt int
		rows.Scan(&model, &cnt)
		dist = append(dist, map[string]interface{}{"model": model, "requests": cnt})
	}
	return dist
}

// dashboardChannelHealth returns channel health with success/fail counts and derived status.
func (h *Handler) dashboardChannelHealth(ctx context.Context, startTime, endTime string) []map[string]interface{} {
	rows, err := h.db.Query(ctx,
		`SELECT c.id, c.name, c.provider, c.status,
		        COALESCE(SUM(CASE WHEN cl.status_code >= 200 AND cl.status_code < 300 THEN 1 ELSE 0 END), 0) as success_count,
		        COALESCE(SUM(CASE WHEN cl.status_code >= 400 OR cl.status_code = 0 THEN 1 ELSE 0 END), 0) as fail_count
		 FROM channels c
		 LEFT JOIN call_logs cl ON cl.channel = c.name AND cl.created_at >= $1 AND cl.created_at <= $2
		 GROUP BY c.id, c.name, c.provider, c.status
		 ORDER BY c.id`, startTime, endTime)
	if err != nil {
		return nil
	}
	defer rows.Close()
	health := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int
		var name, provider, chStatus string
		var successCount, failCount int
		rows.Scan(&id, &name, &provider, &chStatus, &successCount, &failCount)
		status := "healthy"
		if chStatus != "active" {
			status = "disabled"
		} else if failCount > 0 && successCount == 0 {
			status = "error"
		} else if failCount > 0 && float64(failCount)/float64(successCount+failCount) > 0.5 {
			status = "warning"
		}
		health = append(health, map[string]interface{}{
			"id": id, "name": name, "provider": provider,
			"enabled": chStatus == "active", "status": status,
			"success_count": successCount, "fail_count": failCount,
		})
	}
	return health
}

// dashboardYesterdayDelta computes delta vs yesterday for cost, latency, and success rate.
func (h *Handler) dashboardYesterdayDelta(ctx context.Context, todayCost int, todayLatency, todayRate float64) (deltaCost int, deltaLatency, deltaRate float64) {
	yesterdayStart := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	yesterdayEnd := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	var yesterCost int
	h.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_cents), 0) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		yesterdayStart, yesterdayEnd,
	).Scan(&yesterCost)
	var yesterLatency float64
	h.db.QueryRow(ctx,
		`SELECT COALESCE(AVG(latency_ms), 0) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		yesterdayStart, yesterdayEnd,
	).Scan(&yesterLatency)
	var yesterSuccess, yesterTotal int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM call_logs WHERE created_at >= $1 AND created_at <= $2 AND status_code >= 200 AND status_code < 300`,
		yesterdayStart, yesterdayEnd,
	).Scan(&yesterSuccess)
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM call_logs WHERE created_at >= $1 AND created_at <= $2`,
		yesterdayStart, yesterdayEnd,
	).Scan(&yesterTotal)
	yesterRate := 0.0
	if yesterTotal > 0 {
		yesterRate = float64(yesterSuccess) / float64(yesterTotal)
	}
	return todayCost - yesterCost, todayLatency - yesterLatency, todayRate - yesterRate
}
