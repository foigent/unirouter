package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/money"
)

type dashboardQueryStage string

const (
	dashboardStageOverview            dashboardQueryStage = "overview"
	dashboardStageRecentLogs          dashboardQueryStage = "recent_logs"
	dashboardStageDailyTrend          dashboardQueryStage = "daily_trend"
	dashboardStageModelDistribution   dashboardQueryStage = "model_distribution"
	dashboardStagePreviousPeriodDelta dashboardQueryStage = "previous_period_delta"
	dashboardStageWallet              dashboardQueryStage = "wallet"
	dashboardStageModelPrices         dashboardQueryStage = "model_prices"
	dashboardStageChannelHealth       dashboardQueryStage = "channel_health"
)

const (
	dashboardReasonContextCanceled  = "context_canceled"
	dashboardReasonDeadlineExceeded = "deadline_exceeded"
	dashboardReasonNoRows           = "unexpected_no_rows"
	dashboardReasonPostgres         = "postgres_query_error"
	dashboardReasonDatabase         = "database_query_error"
)

func (h *Handler) DashboardStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}

	start, end, err := dashboardTimeRange(r, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	startTime, endTime := start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)

	window, err := h.dashboardWindowMetrics(ctx, orgID, startTime, endTime)
	if err != nil {
		writeDashboardQueryError(w, r, dashboardStageOverview, err)
		return
	}
	recentLogs, err := h.dashboardRecentLogs(ctx, orgID)
	if err != nil {
		writeDashboardQueryError(w, r, dashboardStageRecentLogs, err)
		return
	}
	dailyTrend, err := h.dashboardDailyTrend(ctx, orgID, startTime, endTime)
	if err != nil {
		writeDashboardQueryError(w, r, dashboardStageDailyTrend, err)
		return
	}
	modelDistribution, err := h.dashboardModelDistribution(ctx, orgID, startTime, endTime)
	if err != nil {
		writeDashboardQueryError(w, r, dashboardStageModelDistribution, err)
		return
	}
	deltaCostMicros, deltaLatency, deltaRate, err := h.dashboardPreviousPeriodDelta(ctx, orgID, start, end, window.CostMicros, window.P95LatencyMS, window.SuccessRate)
	if err != nil {
		writeDashboardQueryError(w, r, dashboardStagePreviousPeriodDelta, err)
		return
	}

	response := map[string]interface{}{
		"total_requests":    window.TotalRequests,
		"today_cost_micros": strconv.FormatInt(window.CostMicros, 10),
		"currency":          "CNY", "amount_scale": money.MicrosPerCNY,
		"avg_latency_ms":     window.AvgLatencyMS,
		"p95_latency_ms":     window.P95LatencyMS,
		"p99_latency_ms":     window.P99LatencyMS,
		"success_rate":       window.SuccessRate,
		"recent_logs":        recentLogs,
		"daily_trend":        dailyTrend,
		"model_distribution": modelDistribution,
		"delta_vs_yesterday": map[string]interface{}{
			"cost_micros":  strconv.FormatInt(deltaCostMicros, 10),
			"latency_ms":   deltaLatency,
			"success_rate": deltaRate,
		},
	}

	// The dashboard must not become a capability bypass. Direct handler unit
	// tests have no authenticated role, so they keep the complete legacy shape;
	// production requests expose only data the same role may read elsewhere.
	role := actorOrganizationRole(ctx)
	unitContext := isUnauthenticatedUnitContext(ctx)
	if unitContext || auth.IsBillingAdmin(role) {
		walletBalance, walletGranted, walletFrozen, walletThreshold, err := h.dashboardWalletInfo(ctx, orgID)
		if err != nil {
			writeDashboardQueryError(w, r, dashboardStageWallet, err)
			return
		}
		response["wallet_balance_micros"] = strconv.FormatInt(walletBalance, 10)
		response["wallet_granted_micros"] = strconv.FormatInt(walletGranted, 10)
		response["wallet_frozen_micros"] = strconv.FormatInt(walletFrozen, 10)
		response["wallet_threshold_micros"] = strconv.FormatInt(walletThreshold, 10)
	}
	if unitContext || auth.RoleIn(role, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin) {
		modelPrices, err := h.dashboardModelPrices(ctx)
		if err != nil {
			writeDashboardQueryError(w, r, dashboardStageModelPrices, err)
			return
		}
		response["model_prices"] = modelPrices
	}
	if unitContext || auth.IsPlatformAdmin(role) {
		channelHealth, err := h.dashboardChannelHealth(ctx, orgID, startTime, endTime)
		if err != nil {
			writeDashboardQueryError(w, r, dashboardStageChannelHealth, err)
			return
		}
		response["channel_health"] = channelHealth
	}

	writeJSON(w, http.StatusOK, response)
}

func dashboardTimeRange(r *http.Request, now time.Time) (time.Time, time.Time, error) {
	end := now
	if raw := r.URL.Query().Get("end_time"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("end_time must be RFC3339")
		}
		end = parsed
	}
	start := end.Add(-24 * time.Hour)
	if raw := r.URL.Query().Get("start_time"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("start_time must be RFC3339")
		}
		start = parsed
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end_time must be after start_time")
	}
	return start.UTC(), end.UTC(), nil
}

func writeDashboardQueryError(w http.ResponseWriter, r *http.Request, stage dashboardQueryStage, err error) {
	reasonCode, sqlState := classifyDashboardQueryError(err)
	logger.New(logger.Fields{
		RequestID: middleware.GetRequestIDFromContext(r.Context()),
		Module:    "dashboard",
	}).Error("query_failed stage=%s reason_code=%s sqlstate=%s", stage, reasonCode, sqlState)
	writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
		"error":       "Dashboard query failed",
		"code":        "dashboard_query_failed",
		"stage":       stage,
		"reason_code": reasonCode,
	})
}

func classifyDashboardQueryError(err error) (reasonCode, sqlState string) {
	switch {
	case errors.Is(err, context.Canceled):
		return dashboardReasonContextCanceled, "none"
	case errors.Is(err, context.DeadlineExceeded):
		return dashboardReasonDeadlineExceeded, "none"
	case errors.Is(err, pgx.ErrNoRows):
		return dashboardReasonNoRows, "none"
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return dashboardReasonPostgres, pgErr.SQLState()
	}
	return dashboardReasonDatabase, "none"
}

type dashboardWindowMetrics struct {
	TotalRequests int
	CostMicros    int64
	AvgLatencyMS  float64
	P95LatencyMS  float64
	P99LatencyMS  float64
	SuccessRate   float64
}

// dashboardWindowMetrics reads one organization-scoped observation window in
// one PostgreSQL snapshot. Apart from reducing round trips, this prevents the
// count, cost, latency and success-rate tiles from describing different clocks.
func (h *Handler) dashboardWindowMetrics(ctx context.Context, orgID int64, startTime, endTime string) (dashboardWindowMetrics, error) {
	var metrics dashboardWindowMetrics
	var successCount int
	err := h.db.QueryRow(ctx, `
		SELECT COUNT(*)::int,
		       COALESCE(SUM(cost_micros),0)::bigint,
		       COALESCE(AVG(latency_ms) FILTER (WHERE latency_ms>0),0)::float8,
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms)
		         FILTER (WHERE latency_ms>0),0)::float8,
		       COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY latency_ms)
		         FILTER (WHERE latency_ms>0),0)::float8,
		       COUNT(*) FILTER (WHERE status_code>=200 AND status_code<300)::int
		  FROM call_logs
		 WHERE org_id=$1 AND created_at >= $2 AND created_at < $3`,
		orgID, startTime, endTime,
	).Scan(&metrics.TotalRequests, &metrics.CostMicros, &metrics.AvgLatencyMS,
		&metrics.P95LatencyMS, &metrics.P99LatencyMS, &successCount)
	if err != nil {
		return dashboardWindowMetrics{}, err
	}
	if metrics.TotalRequests > 0 {
		metrics.SuccessRate = float64(successCount) / float64(metrics.TotalRequests)
	}
	return metrics, nil
}

// dashboardRecentLogs returns the last 10 call logs.
func (h *Handler) dashboardRecentLogs(ctx context.Context, orgID int64) ([]map[string]interface{}, error) {
	rows, err := h.db.Query(ctx,
		`SELECT request_id,
		        COALESCE(NULLIF(BTRIM(requested_model),''),NULLIF(BTRIM(model),''),'(unknown)'),
		        COALESCE(NULLIF(BTRIM(model),''),''),COALESCE(route_channel_id,0),
		        provider,channel,status_code,latency_ms,cost_micros,created_at
		 FROM call_logs WHERE org_id = $1 ORDER BY created_at DESC LIMIT 10`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var requestID, model, routingModel, provider, channel string
		var routeChannelID int64
		var statusCode, latencyMs int
		var costMicros int64
		var createdAt time.Time
		if err := rows.Scan(&requestID, &model, &routingModel, &routeChannelID, &provider, &channel, &statusCode, &latencyMs, &costMicros, &createdAt); err != nil {
			return nil, err
		}
		logs = append(logs, map[string]interface{}{
			"request_id": requestID, "model": model, "routing_model": routingModel,
			"route_channel_id": routeChannelID, "provider": provider, "channel": channel, "status_code": statusCode,
			"latency_ms": latencyMs, "cost_micros": strconv.FormatInt(costMicros, 10), "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

// dashboardModelPrices returns all model prices.
func (h *Handler) dashboardModelPrices(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := h.db.Query(ctx,
		`SELECT model_name, (input_price_cents * 10)::text, (output_price_cents * 10)::text FROM model_prices ORDER BY model_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prices := make([]map[string]interface{}, 0)
	for rows.Next() {
		var modelName string
		var inputPrice, outputPrice string
		if err := rows.Scan(&modelName, &inputPrice, &outputPrice); err != nil {
			return nil, err
		}
		prices = append(prices, map[string]interface{}{
			"model_name": modelName, "input_price_cny_per_million": inputPrice, "output_price_cny_per_million": outputPrice,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return prices, nil
}

// dashboardDailyTrend returns hourly buckets for short ranges and daily buckets for longer ranges.
func (h *Handler) dashboardDailyTrend(ctx context.Context, orgID int64, startTime, endTime string) ([]map[string]interface{}, error) {
	start, startErr := time.Parse(time.RFC3339Nano, startTime)
	end, endErr := time.Parse(time.RFC3339Nano, endTime)
	granularity := "day"
	if startErr == nil && endErr == nil && end.Sub(start) <= 48*time.Hour {
		granularity = "hour"
	}

	rows, err := h.db.Query(ctx,
		`SELECT date_trunc($4, created_at) AS bucket, COUNT(*) AS requests, COALESCE(SUM(cost_micros), 0) AS cost
		 FROM call_logs
		 WHERE org_id = $1 AND created_at >= $2 AND created_at < $3
		 GROUP BY bucket
		 ORDER BY bucket ASC`, orgID, startTime, endTime, granularity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type bucketValue struct {
		requests int
		cost     int64
	}
	values := make(map[string]bucketValue)
	for rows.Next() {
		var bucket time.Time
		var requests int
		var cost int64
		if err := rows.Scan(&bucket, &requests, &cost); err != nil {
			return nil, err
		}
		bucket = bucket.UTC()
		bucketKey := bucket.Format("2006-01-02")
		if granularity == "hour" {
			bucketKey = bucket.Format(time.RFC3339)
		}
		values[bucketKey] = bucketValue{requests: requests, cost: cost}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if startErr != nil || endErr != nil || !end.After(start) {
		trend := make([]map[string]interface{}, 0, len(values))
		for bucket, value := range values {
			trend = append(trend, map[string]interface{}{"date": bucket, "requests": value.requests, "cost_micros": strconv.FormatInt(value.cost, 10)})
		}
		return trend, nil
	}

	start = start.UTC()
	end = end.UTC()
	var cursor, stop time.Time
	var step time.Duration
	if granularity == "hour" {
		cursor = start.Truncate(time.Hour)
		stop = end.Truncate(time.Hour)
		step = time.Hour
	} else {
		cursor = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		stop = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
		step = 24 * time.Hour
	}

	trend := make([]map[string]interface{}, 0)
	for !cursor.After(stop) {
		key := cursor.Format("2006-01-02")
		if granularity == "hour" {
			key = cursor.Format(time.RFC3339)
		}
		value := values[key]
		trend = append(trend, map[string]interface{}{
			"date": key, "requests": value.requests, "cost_micros": strconv.FormatInt(value.cost, 10),
		})
		cursor = cursor.Add(step)
	}
	return trend, nil
}

// dashboardWalletInfo returns wallet balance, granted, frozen, and threshold.
func (h *Handler) dashboardWalletInfo(ctx context.Context, orgID int64) (balance, granted, frozen, threshold int64, err error) {
	var thresholdCents int64
	err = h.db.QueryRow(ctx,
		`SELECT COALESCE(balance_micros,0), COALESCE(granted_balance_micros,0), COALESCE(frozen_micros,0), COALESCE(low_balance_threshold_cents,0)
		 FROM wallets WHERE org_id = $1`,
		orgID,
	).Scan(&balance, &granted, &frozen, &thresholdCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, 0, 0, nil
	}
	if err != nil {
		return
	}
	threshold, err = money.CentsToMicros(thresholdCents)
	return
}

// dashboardModelDistribution returns request health by the resolved callable
// model for the selected window. call_logs.model is the alias-resolved identity;
// requested_model is retained separately for request inspection. This
// projection must not invent another identity by rewriting case, spaces or
// punctuation.
func (h *Handler) dashboardModelDistribution(ctx context.Context, orgID int64, startTime, endTime string) ([]map[string]interface{}, error) {
	rows, err := h.db.Query(ctx,
		`WITH normalized_logs AS (
		   SELECT COALESCE(NULLIF(BTRIM(model),''),NULLIF(BTRIM(requested_model),''),'(unknown)') AS model,
		          status_code,latency_ms
		     FROM call_logs
		    WHERE org_id=$1 AND created_at >= $2 AND created_at < $3
		 )
		 SELECT model,
		        COUNT(*)::int AS requests,
		        COUNT(*) FILTER (WHERE status_code >= 200 AND status_code < 300)::int AS success_count,
		        COUNT(*) FILTER (WHERE status_code < 200 OR status_code >= 300)::int AS fail_count,
		        COALESCE(AVG(latency_ms) FILTER (WHERE latency_ms > 0),0)::float8 AS avg_latency_ms,
		        COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms)
		          FILTER (WHERE latency_ms > 0),0)::float8 AS p95_latency_ms
		   FROM normalized_logs
		  GROUP BY model
		  ORDER BY requests DESC,model ASC`, orgID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dist := make([]map[string]interface{}, 0)
	for rows.Next() {
		var model string
		var requests, successCount, failCount int
		var avgLatencyMS, p95LatencyMS float64
		if err := rows.Scan(&model, &requests, &successCount, &failCount, &avgLatencyMS, &p95LatencyMS); err != nil {
			return nil, err
		}
		successRate := 0.0
		if requests > 0 {
			successRate = float64(successCount) / float64(requests)
		}
		dist = append(dist, map[string]interface{}{
			"model": model, "requests": requests,
			"success_count": successCount, "fail_count": failCount, "success_rate": successRate,
			"avg_latency_ms": avgLatencyMS, "p95_latency_ms": p95LatencyMS,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return dist, nil
}

// dashboardChannelHealth separates current routability from historical window
// results. A staged channel may be enabled by a stable runtime release, while a
// published channel without an eligible credential is not currently routable.
func (h *Handler) dashboardChannelHealth(ctx context.Context, orgID int64, startTime, endTime string) ([]map[string]interface{}, error) {
	endpointProjection, err := h.loadModelEndpointProjection(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("load canonical model endpoint projection: %w", err)
	}
	operational := dashboardChannelOperationalStates(endpointProjection)

	rows, err := h.db.Query(ctx,
		`SELECT c.id,c.name,c.provider,c.base_url,c.status,c.circuit_state,
		        COALESCE(SUM(CASE WHEN cl.status_code >= 200 AND cl.status_code < 300 THEN 1 ELSE 0 END), 0) as success_count,
		        COALESCE(SUM(CASE WHEN cl.status_code >= 400 OR cl.status_code = 0 THEN 1 ELSE 0 END), 0) as fail_count
		 FROM channels c
		 LEFT JOIN call_logs cl ON cl.route_channel_id = c.id AND cl.org_id = $1 AND cl.created_at >= $2 AND cl.created_at < $3
		 GROUP BY c.id,c.name,c.provider,c.base_url,c.status,c.circuit_state
		 ORDER BY c.id`, orgID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	health := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int
		var name, provider, baseURL, chStatus, circuitState string
		var successCount, failCount int
		if err := rows.Scan(&id, &name, &provider, &baseURL, &chStatus, &circuitState, &successCount, &failCount); err != nil {
			return nil, err
		}
		state := operational[int64(id)]
		publicModels := make([]string, 0, len(state.publicModels))
		for publicModel := range state.publicModels {
			publicModels = append(publicModels, publicModel)
		}
		sort.Strings(publicModels)
		runtimePublished := state.runtimePublished
		credentialReady := state.credentialReady
		effectiveEnabled := chStatus == "active" || (chStatus == "staged" && runtimePublished)
		available := state.available
		status := "healthy"
		if chStatus == "disabled" {
			status = "disabled"
		} else if state.staleAssignment {
			status = "stale_release"
		} else if state.projectionUnavailable {
			status = "projection_unavailable"
		} else if !effectiveEnabled {
			status = "awaiting_release"
		} else if !credentialReady {
			status = "no_credential"
		} else if circuitState != "healthy" {
			status = "circuit_unavailable"
		} else if !available {
			status = "standby"
		} else if failCount > 0 && successCount == 0 {
			status = "error"
		} else if failCount > 0 {
			status = "warning"
		} else if state.degraded {
			status = "degraded"
		}
		health = append(health, map[string]interface{}{
			"id": id, "name": name, "provider": provider, "base_url": baseURL,
			"public_models": publicModels,
			"base_status":   chStatus, "runtime_published": runtimePublished,
			"enabled": effectiveEnabled, "credential_ready": credentialReady,
			"available": available, "circuit_state": circuitState, "status": status,
			"projection_version": endpointProjection.ContractVersion,
			"success_count":      successCount, "fail_count": failCount,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return health, nil
}

type dashboardChannelOperationalState struct {
	runtimePublished      bool
	credentialReady       bool
	available             bool
	staleAssignment       bool
	projectionUnavailable bool
	degraded              bool
	publicModels          map[string]struct{}
}

func dashboardChannelOperationalStates(projection modelEndpointProjection) map[int64]dashboardChannelOperationalState {
	states := make(map[int64]dashboardChannelOperationalState)
	for _, model := range projection.Models {
		for _, target := range model.Targets {
			state := states[target.ChannelID]
			if state.publicModels == nil {
				state.publicModels = make(map[string]struct{})
			}
			state.publicModels[model.PublicModel] = struct{}{}
			stableCurrent := target.StableAssignment && target.StableExpectedEpoch == target.RoutingEpoch
			canaryCurrent := target.CanaryAssignment && target.CanaryExpectedEpoch == target.RoutingEpoch
			state.runtimePublished = state.runtimePublished || stableCurrent || canaryCurrent
			currentRouteTarget := stableCurrent || canaryCurrent || target.RouteCandidate
			state.credentialReady = state.credentialReady || (currentRouteTarget && target.EligibleCredentialCount > 0)
			state.staleAssignment = state.staleAssignment || target.State == "stale"
			state.projectionUnavailable = state.projectionUnavailable || model.ReasonCode == "routing_projection_unavailable"
			state.degraded = state.degraded || model.State == "degraded"
			if model.RouteAvailable && target.CircuitState == "healthy" {
				switch target.State {
				case "active", "canary", "legacy_active":
					state.available = true
				}
			}
			states[target.ChannelID] = state
		}
	}
	return states
}

// dashboardPreviousPeriodDelta compares the selected range with the immediately
// preceding range of the same duration. For the default 24h view this is the
// previous 24-hour period represented by the legacy response field name.
func (h *Handler) dashboardPreviousPeriodDelta(ctx context.Context, orgID int64, start, end time.Time, todayCost int64, todayLatency, todayRate float64) (deltaCost int64, deltaLatency, deltaRate float64, err error) {
	previousEnd := start
	previousStart := start.Add(-end.Sub(start))
	previous, err := h.dashboardWindowMetrics(ctx, orgID,
		previousStart.Format(time.RFC3339Nano), previousEnd.Format(time.RFC3339Nano))
	if err != nil {
		return 0, 0, 0, err
	}
	return todayCost - previous.CostMicros, todayLatency - previous.P95LatencyMS, todayRate - previous.SuccessRate, nil
}
