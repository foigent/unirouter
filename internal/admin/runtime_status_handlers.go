package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	goruntime "runtime"
	"time"
)

const runtimeTrafficWindowMinutes = 5
const runtimeMonitoringProbeTimeout = 750 * time.Millisecond
const prometheusUniRouterTargetQuery = `up{job="unirouter"}`

type RuntimeStatusOptions struct {
	Environment    string
	DeploymentMode string
	DeploymentRole string
	Version        string
	Commit         string
	PrometheusURL  string
	GrafanaURL     string
}

type runtimeStatusResponse struct {
	Status                    string                           `json:"status"`
	GeneratedAt               time.Time                        `json:"generated_at"`
	DataFreshnessSeconds      int                              `json:"data_freshness_seconds"`
	RuntimeEnvironment        runtimeEnvironmentStatus         `json:"runtime_environment"`
	CapacityHealth            runtimeCapacityHealth            `json:"capacity_health"`
	ObservabilityIntegrations runtimeObservabilityIntegrations `json:"observability_integrations"`
}

type runtimeEnvironmentStatus struct {
	Service        string `json:"service"`
	Environment    string `json:"environment"`
	DeploymentMode string `json:"deployment_mode"`
	DeploymentRole string `json:"deployment_role"`
	Version        string `json:"version,omitempty"`
	Commit         string `json:"commit,omitempty"`
	GoVersion      string `json:"go_version"`
	GOOS           string `json:"goos"`
	GOARCH         string `json:"goarch"`
	NumCPU         int    `json:"num_cpu"`
	Goroutines     int    `json:"goroutines"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
}

type runtimeCapacityHealth struct {
	AppMemory runtimeMemoryStatus     `json:"app_memory"`
	Database  runtimeDependencyStatus `json:"database"`
	Redis     runtimeDependencyStatus `json:"redis"`
	Traffic   runtimeTrafficStatus    `json:"traffic"`
	CPU       runtimeMetricStatus     `json:"cpu"`
	Disk      runtimeMetricStatus     `json:"disk"`
	Network   runtimeMetricStatus     `json:"network"`
}

type runtimeMemoryStatus struct {
	Status         string `json:"status"`
	AllocBytes     uint64 `json:"alloc_bytes"`
	SysBytes       uint64 `json:"sys_bytes"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64 `json:"heap_sys_bytes"`
}

type runtimeDependencyStatus struct {
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type runtimeTrafficStatus struct {
	Status        string  `json:"status"`
	WindowMinutes int     `json:"window_minutes"`
	Requests      int64   `json:"requests"`
	ErrorRequests int64   `json:"error_requests"`
	ErrorRate     float64 `json:"error_rate"`
	TotalTokens   int64   `json:"total_tokens"`
	CostCents     int64   `json:"cost_cents"`
	P95LatencyMS  float64 `json:"p95_latency_ms"`
	P99LatencyMS  float64 `json:"p99_latency_ms"`
	Reason        string  `json:"reason,omitempty"`
}

type runtimeMetricStatus struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type runtimeObservabilityIntegrations struct {
	Prometheus runtimeIntegrationStatus `json:"prometheus"`
	Grafana    runtimeIntegrationStatus `json:"grafana"`
}

type runtimeIntegrationStatus struct {
	Status       string     `json:"status"`
	URL          string     `json:"url,omitempty"`
	HealthURL    string     `json:"health_url,omitempty"`
	CheckedAt    *time.Time `json:"checked_at,omitempty"`
	LatencyMS    int64      `json:"latency_ms,omitempty"`
	Reason       string     `json:"reason,omitempty"`
	ScrapeStatus string     `json:"scrape_status,omitempty"`
	Version      string     `json:"version,omitempty"`
}

type prometheusQueryResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		Result []struct {
			Value []json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

type grafanaHealthResponse struct {
	Database string `json:"database"`
	Version  string `json:"version"`
}

type monitoringHTTPResult struct {
	StatusCode int
	Body       []byte
	LatencyMS  int64
}

func (h *Handler) RuntimeStatus(w http.ResponseWriter, r *http.Request) {
	database := h.runtimeDatabaseHealth(r.Context())
	redis := h.runtimeRedisHealth(r.Context())
	traffic := h.runtimeTrafficStatus(r.Context())

	response := runtimeStatusResponse{
		Status:               runtimeOverallStatus(database, redis, traffic),
		GeneratedAt:          time.Now().UTC(),
		DataFreshnessSeconds: 0,
		RuntimeEnvironment:   h.runtimeEnvironmentStatus(),
		CapacityHealth: runtimeCapacityHealth{
			AppMemory: h.runtimeMemoryStatus(),
			Database:  database,
			Redis:     redis,
			Traffic:   traffic,
			CPU:       unavailableRuntimeMetric("telemetry_source_not_configured"),
			Disk:      unavailableRuntimeMetric("telemetry_source_not_configured"),
			Network:   unavailableRuntimeMetric("telemetry_source_not_configured"),
		},
		ObservabilityIntegrations: h.runtimeObservabilityIntegrations(r.Context()),
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) runtimeEnvironmentStatus() runtimeEnvironmentStatus {
	return runtimeEnvironmentStatus{
		Service:        "uniroute",
		Environment:    defaultRuntimeValue(h.runtimeOptions.Environment, "unspecified"),
		DeploymentMode: defaultRuntimeValue(h.runtimeOptions.DeploymentMode, "single-node"),
		DeploymentRole: defaultRuntimeValue(h.runtimeOptions.DeploymentRole, "stable"),
		Version:        h.runtimeOptions.Version,
		Commit:         h.runtimeOptions.Commit,
		GoVersion:      goruntime.Version(),
		GOOS:           goruntime.GOOS,
		GOARCH:         goruntime.GOARCH,
		NumCPU:         goruntime.NumCPU(),
		Goroutines:     goruntime.NumGoroutine(),
		UptimeSeconds:  int64(time.Since(h.startedAt).Seconds()),
	}
}

func (h *Handler) runtimeMemoryStatus() runtimeMemoryStatus {
	var mem goruntime.MemStats
	goruntime.ReadMemStats(&mem)
	return runtimeMemoryStatus{
		Status:         "available",
		AllocBytes:     mem.Alloc,
		SysBytes:       mem.Sys,
		HeapAllocBytes: mem.HeapAlloc,
		HeapSysBytes:   mem.HeapSys,
	}
}

func (h *Handler) runtimeDatabaseHealth(ctx context.Context) runtimeDependencyStatus {
	if h.db == nil {
		return runtimeDependencyStatus{Status: "unavailable", Reason: "database_not_configured"}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	started := time.Now()
	var one int
	if err := h.db.QueryRow(probeCtx, "SELECT 1").Scan(&one); err != nil {
		return runtimeDependencyStatus{Status: "unavailable", Reason: "query_failed"}
	}
	return runtimeDependencyStatus{Status: "connected", LatencyMS: elapsedMillis(started)}
}

func (h *Handler) runtimeRedisHealth(ctx context.Context) runtimeDependencyStatus {
	if h.redisClient == nil {
		return runtimeDependencyStatus{Status: "unavailable", Reason: "redis_client_not_configured"}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := h.redisClient.Ping(probeCtx).Err(); err != nil {
		return runtimeDependencyStatus{Status: "unavailable", Reason: "ping_failed"}
	}
	return runtimeDependencyStatus{Status: "connected", LatencyMS: elapsedMillis(started)}
}

func (h *Handler) runtimeTrafficStatus(ctx context.Context) runtimeTrafficStatus {
	status := runtimeTrafficStatus{Status: "available", WindowMinutes: runtimeTrafficWindowMinutes}
	if h.db == nil {
		status.Status = "unavailable"
		status.Reason = "database_not_configured"
		return status
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := h.db.QueryRow(queryCtx, `
WITH recent AS (
	SELECT total_tokens, cost_cents, latency_ms, status_code
	FROM call_logs
	WHERE created_at >= NOW() - ($1::int * INTERVAL '1 minute')
)
SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN status_code = 0 OR status_code >= 500 THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(total_tokens), 0),
	COALESCE(SUM(cost_cents), 0),
	COALESCE((SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) FROM recent WHERE latency_ms > 0), 0),
	COALESCE((SELECT percentile_cont(0.99) WITHIN GROUP (ORDER BY latency_ms) FROM recent WHERE latency_ms > 0), 0)
FROM recent`, runtimeTrafficWindowMinutes).Scan(
		&status.Requests,
		&status.ErrorRequests,
		&status.TotalTokens,
		&status.CostCents,
		&status.P95LatencyMS,
		&status.P99LatencyMS,
	)
	if err != nil {
		status.Status = "unavailable"
		status.Reason = "query_failed"
		return status
	}
	if status.Requests > 0 {
		status.ErrorRate = float64(status.ErrorRequests) / float64(status.Requests)
	}
	return status
}

func runtimeOverallStatus(database, redis runtimeDependencyStatus, traffic runtimeTrafficStatus) string {
	if database.Status == "unavailable" {
		return "degraded"
	}
	if redis.Status == "unavailable" || traffic.Status == "unavailable" {
		return "warning"
	}
	if traffic.Requests >= 20 && traffic.ErrorRate >= 0.05 {
		return "warning"
	}
	return "operational"
}

func (h *Handler) runtimeObservabilityIntegrations(ctx context.Context) runtimeObservabilityIntegrations {
	prometheusCh := make(chan runtimeIntegrationStatus, 1)
	grafanaCh := make(chan runtimeIntegrationStatus, 1)
	go func() {
		prometheusCh <- runtimePrometheusIntegration(ctx, h.runtimeOptions.PrometheusURL)
	}()
	go func() {
		grafanaCh <- runtimeGrafanaIntegration(ctx, h.runtimeOptions.GrafanaURL)
	}()
	return runtimeObservabilityIntegrations{
		Prometheus: <-prometheusCh,
		Grafana:    <-grafanaCh,
	}
}

func runtimePrometheusIntegration(ctx context.Context, rawURL string) runtimeIntegrationStatus {
	if rawURL == "" {
		return runtimeIntegration("PROMETHEUS_URL", rawURL)
	}
	checkedAt := time.Now().UTC()
	started := time.Now()
	status := runtimeIntegrationStatus{
		Status:    "unknown",
		URL:       rawURL,
		HealthURL: monitoringURL(rawURL, "/-/ready"),
		CheckedAt: &checkedAt,
	}

	healthResp, err := monitoringGet(ctx, status.HealthURL)
	status.LatencyMS = elapsedMillis(started)
	if err != nil {
		status.Status = "unavailable"
		status.Reason = "probe_failed"
		return status
	}
	if healthResp.StatusCode < 200 || healthResp.StatusCode >= 300 {
		status.Status = "unavailable"
		status.Reason = fmt.Sprintf("health_http_status_%d", healthResp.StatusCode)
		return status
	}

	queryResp, err := monitoringGet(ctx, monitoringPrometheusQueryURL(rawURL))
	status.LatencyMS = elapsedMillis(started)
	if err != nil {
		status.Status = "unavailable"
		status.Reason = "query_failed"
		return status
	}
	if queryResp.StatusCode < 200 || queryResp.StatusCode >= 300 {
		status.Status = "misconfigured"
		status.Reason = fmt.Sprintf("query_http_status_%d", queryResp.StatusCode)
		return status
	}
	var payload prometheusQueryResponse
	if err := json.Unmarshal(queryResp.Body, &payload); err != nil {
		status.Status = "misconfigured"
		status.Reason = "invalid_query_response"
		return status
	}
	if payload.Status != "success" {
		status.Status = "misconfigured"
		status.Reason = "prometheus_query_not_success"
		return status
	}
	if len(payload.Data.Result) == 0 {
		status.Status = "stale"
		status.Reason = "unirouter_target_missing"
		status.ScrapeStatus = "missing"
		return status
	}
	value, ok := prometheusResultValue(payload.Data.Result[0].Value)
	if !ok {
		status.Status = "misconfigured"
		status.Reason = "invalid_unirouter_target_value"
		return status
	}
	status.ScrapeStatus = value
	if value == "1" {
		status.Status = "connected"
		status.ScrapeStatus = "up"
		return status
	}
	if value == "0" {
		status.Status = "stale"
		status.ScrapeStatus = "down"
		status.Reason = "unirouter_target_down"
		return status
	}
	status.Status = "stale"
	status.Reason = "unirouter_target_unexpected_value"
	return status
}

func runtimeGrafanaIntegration(ctx context.Context, rawURL string) runtimeIntegrationStatus {
	if rawURL == "" {
		return runtimeIntegration("GRAFANA_URL", rawURL)
	}
	checkedAt := time.Now().UTC()
	started := time.Now()
	status := runtimeIntegrationStatus{
		Status:    "unknown",
		URL:       rawURL,
		HealthURL: monitoringURL(rawURL, "/api/health"),
		CheckedAt: &checkedAt,
	}
	resp, err := monitoringGet(ctx, status.HealthURL)
	status.LatencyMS = elapsedMillis(started)
	if err != nil {
		status.Status = "unavailable"
		status.Reason = "probe_failed"
		return status
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		status.Status = "misconfigured"
		status.Reason = "auth_required"
		return status
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status.Status = "unavailable"
		status.Reason = fmt.Sprintf("health_http_status_%d", resp.StatusCode)
		return status
	}
	var payload grafanaHealthResponse
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		status.Status = "misconfigured"
		status.Reason = "invalid_health_response"
		return status
	}
	status.Version = payload.Version
	if payload.Database != "" && payload.Database != "ok" {
		status.Status = "stale"
		status.Reason = "grafana_database_not_ok"
		return status
	}
	status.Status = "connected"
	return status
}

func unavailableRuntimeMetric(reason string) runtimeMetricStatus {
	return runtimeMetricStatus{Status: "unavailable", Reason: reason}
}

func runtimeIntegration(configName, rawURL string) runtimeIntegrationStatus {
	if rawURL == "" {
		return runtimeIntegrationStatus{Status: "not_configured", Reason: configName + "_empty"}
	}
	return runtimeIntegrationStatus{Status: "configured", URL: rawURL}
}

func monitoringGet(ctx context.Context, targetURL string) (monitoringHTTPResult, error) {
	probeCtx, cancel := context.WithTimeout(ctx, runtimeMonitoringProbeTimeout)
	defer cancel()
	started := time.Now()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return monitoringHTTPResult{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return monitoringHTTPResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return monitoringHTTPResult{}, err
	}
	return monitoringHTTPResult{
		StatusCode: resp.StatusCode,
		Body:       body,
		LatencyMS:  elapsedMillis(started),
	}, nil
}

func monitoringURL(rawURL, path string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func monitoringPrometheusQueryURL(rawURL string) string {
	return monitoringURL(rawURL, "/api/v1/query") + "?query=" + url.QueryEscape(prometheusUniRouterTargetQuery)
}

func prometheusResultValue(raw []json.RawMessage) (string, bool) {
	if len(raw) < 2 {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw[1], &text); err == nil {
		return text, text != ""
	}
	var number float64
	if err := json.Unmarshal(raw[1], &number); err == nil {
		if number == 1 {
			return "1", true
		}
		if number == 0 {
			return "0", true
		}
		return fmt.Sprintf("%g", number), true
	}
	return "", false
}

func defaultRuntimeValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func elapsedMillis(started time.Time) int64 {
	elapsed := time.Since(started).Milliseconds()
	if elapsed < 1 {
		return 1
	}
	return elapsed
}
