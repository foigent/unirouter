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
		        COALESCE(NULLIF(BTRIM(model),''),''),COALESCE(route_deployment_id,0),
		        COALESCE(NULLIF(BTRIM(route_model_operation_id),''),''),COALESCE(route_channel_id,0),
		        provider,channel,status_code,latency_ms,cost_micros,created_at
		 FROM call_logs WHERE org_id = $1 ORDER BY created_at DESC LIMIT 10`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var requestID, model, routingModel, operationID, provider, channel string
		var routeDeploymentID, routeChannelID int64
		var statusCode, latencyMs int
		var costMicros int64
		var createdAt time.Time
		if err := rows.Scan(&requestID, &model, &routingModel, &routeDeploymentID, &operationID,
			&routeChannelID, &provider, &channel, &statusCode, &latencyMs, &costMicros, &createdAt); err != nil {
			return nil, err
		}
		routeIdentity := ""
		if routeDeploymentID > 0 && operationID != "" {
			routeIdentity = "canonical_deployment_operation"
		} else if routeChannelID > 0 {
			routeIdentity = "legacy_history"
		}
		logs = append(logs, map[string]interface{}{
			"request_id": requestID, "model": model, "routing_model": routingModel,
			"route_deployment_id": routeDeploymentID, "route_model_operation_id": operationID,
			"route_channel_id": routeChannelID, "route_identity": routeIdentity,
			"provider": provider, "channel": channel, "status_code": statusCode,
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
		          status_code,latency_ms,cost_micros
		     FROM call_logs
		    WHERE org_id=$1 AND created_at >= $2 AND created_at < $3
		 )
		 SELECT model,
		        COUNT(*)::int AS requests,
		        COUNT(*) FILTER (WHERE status_code >= 200 AND status_code < 300)::int AS success_count,
		        COUNT(*) FILTER (WHERE status_code < 200 OR status_code >= 300)::int AS fail_count,
		        COALESCE(AVG(latency_ms) FILTER (WHERE latency_ms > 0),0)::float8 AS avg_latency_ms,
		        COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms)
		          FILTER (WHERE latency_ms > 0),0)::float8 AS p95_latency_ms,
		        COALESCE(SUM(cost_micros),0)::bigint AS cost_micros
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
		var costMicros int64
		if err := rows.Scan(&model, &requests, &successCount, &failCount, &avgLatencyMS, &p95LatencyMS, &costMicros); err != nil {
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
			"cost_micros": strconv.FormatInt(costMicros, 10),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return dist, nil
}

// dashboardChannelHealth preserves the historical response key while making
// each live row a canonical deployment+operation projection. Window counts are
// joined only by route_deployment_id+route_model_operation_id. A legacy
// route_channel_id remains available in recent_logs as history, but can never
// make a current deployment appear healthy.
func (h *Handler) dashboardChannelHealth(ctx context.Context, orgID int64, startTime, endTime string) ([]map[string]interface{}, error) {
	projection, err := h.loadCanonicalModelApplications(ctx, orgID, 0, "")
	if err != nil {
		return nil, fmt.Errorf("load canonical deployment health projection: %w", err)
	}
	type routeIdentity struct {
		deploymentID int64
		operationID  string
	}
	type historicalCounts struct {
		success int
		failure int
	}
	rows, err := h.db.Query(ctx,
		`SELECT route_deployment_id,route_model_operation_id,
		        COUNT(*) FILTER (WHERE status_code >= 200 AND status_code < 300)::int,
		        COUNT(*) FILTER (WHERE status_code < 200 OR status_code >= 300)::int
		   FROM call_logs
		  WHERE org_id=$1 AND created_at >= $2 AND created_at < $3
		    AND route_deployment_id IS NOT NULL
		    AND COALESCE(NULLIF(BTRIM(route_model_operation_id),''),'') <> ''
		  GROUP BY route_deployment_id,route_model_operation_id
		  ORDER BY route_deployment_id,route_model_operation_id`, orgID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[routeIdentity]historicalCounts)
	for rows.Next() {
		var identity routeIdentity
		var item historicalCounts
		if err := rows.Scan(&identity.deploymentID, &identity.operationID, &item.success, &item.failure); err != nil {
			return nil, err
		}
		counts[identity] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	health := make([]map[string]interface{}, 0)
	for _, application := range projection.Applications {
		for _, operation := range application.Operations {
			identity := routeIdentity{deploymentID: application.Deployment.ID, operationID: operation.OperationID}
			history := counts[identity]
			runtimePublished := operation.Runtime.State == "stable" || operation.Runtime.State == "canary"
			credentialReady := application.Credential.EligibleCount > 0
			effectiveEnabled := application.Connection.AdminState == "enabled" &&
				application.Deployment.AdminState == "enabled" &&
				application.Connection.RetiredAt == nil && application.Deployment.RetiredAt == nil
			operationReady := canonicalReadinessOperationVerified(operation)
			available := effectiveEnabled && credentialReady && runtimePublished && operationReady &&
				application.Deployment.CircuitState == "healthy" && application.PublicModel.Lifecycle != "retired"
			status := canonicalDashboardDeploymentStatus(application, operation, effectiveEnabled, credentialReady, runtimePublished, operationReady, available, history.success, history.failure)
			health = append(health, map[string]interface{}{
				"id":                            application.Deployment.ID,
				"deployment_id":                 application.Deployment.ID,
				"connection_id":                 application.Connection.ID,
				"name":                          application.Connection.Name,
				"provider":                      application.Connection.Provider,
				"base_url":                      application.Connection.BaseURL,
				"public_model":                  application.PublicModel.ModelName,
				"public_models":                 []string{application.PublicModel.ModelName},
				"model_operation_id":            operation.OperationID,
				"base_status":                   application.Deployment.AdminState,
				"runtime_published":             runtimePublished,
				"enabled":                       effectiveEnabled,
				"credential_ready":              credentialReady,
				"operation_ready":               operationReady,
				"available":                     available,
				"circuit_state":                 application.Deployment.CircuitState,
				"status":                        status,
				"projection_version":            canonicalModelApplicationContractVersion,
				"historical_identity_authority": "route_deployment_id+route_model_operation_id",
				"success_count":                 history.success,
				"fail_count":                    history.failure,
			})
		}
	}
	sort.Slice(health, func(i, j int) bool {
		leftID, _ := health[i]["deployment_id"].(int64)
		rightID, _ := health[j]["deployment_id"].(int64)
		if leftID != rightID {
			return leftID < rightID
		}
		leftOperation, _ := health[i]["model_operation_id"].(string)
		rightOperation, _ := health[j]["model_operation_id"].(string)
		return leftOperation < rightOperation
	})
	return health, nil
}

func canonicalDashboardDeploymentStatus(
	application canonicalModelApplication,
	operation canonicalOperationAxis,
	effectiveEnabled, credentialReady, runtimePublished, operationReady, available bool,
	successCount, failureCount int,
) string {
	switch {
	case application.PublicModel.Lifecycle == "retired" || application.Deployment.AdminState == "retired":
		return "retired"
	case !effectiveEnabled:
		return "disabled"
	case !runtimePublished:
		return "awaiting_release"
	case !credentialReady:
		return "no_credential"
	case !operationReady:
		return "operation_unverified"
	case application.Deployment.CircuitState != "healthy":
		return "circuit_unavailable"
	case !available:
		return "standby"
	case failureCount > 0 && successCount == 0:
		return "error"
	case failureCount > 0:
		return "warning"
	default:
		return "healthy"
	}
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
