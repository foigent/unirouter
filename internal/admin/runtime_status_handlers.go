package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/uniroute/unirouter/internal/runtimeconfig"
)

const runtimeTrafficWindowMinutes = 5
const runtimeMonitoringProbeTimeout = 750 * time.Millisecond
const prometheusUniRouterTargetQuery = `up{job="unirouter"}`

type RuntimeStatusOptions struct {
	Environment     string
	DeploymentMode  string
	DeploymentRole  string
	Version         string
	Commit          string
	PrometheusURL   string
	GrafanaURL      string
	EffectiveConfig runtimeconfig.Snapshot
}

type runtimeStatusResponse struct {
	Status                    string                           `json:"status"`
	GeneratedAt               time.Time                        `json:"generated_at"`
	DataFreshnessSeconds      int                              `json:"data_freshness_seconds"`
	RuntimeEnvironment        runtimeEnvironmentStatus         `json:"runtime_environment"`
	CapacityHealth            runtimeCapacityHealth            `json:"capacity_health"`
	ObservabilityIntegrations runtimeObservabilityIntegrations `json:"observability_integrations"`
	Topology                  runtimeTopologyStatus            `json:"topology"`
	EffectiveConfiguration    runtimeconfig.Snapshot           `json:"effective_configuration"`
}

type runtimeTopologyStatus struct {
	Scope              string                `json:"scope"`
	Visibility         string                `json:"visibility"`
	NetworkExposure    string                `json:"network_exposure"`
	MultiInstanceState string                `json:"multi_instance_state"`
	Nodes              []runtimeTopologyNode `json:"nodes"`
	Edges              []runtimeTopologyEdge `json:"edges"`
	Limitations        []string              `json:"limitations"`
}

type runtimeTopologyNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

type runtimeTopologyEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
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
	Status               string   `json:"status"`
	Reason               string   `json:"reason,omitempty"`
	UsageRatio           *float64 `json:"usage_ratio,omitempty"`
	LogicalCores         int      `json:"logical_cores,omitempty"`
	SampleMillis         int64    `json:"sample_millis,omitempty"`
	TotalBytes           uint64   `json:"total_bytes,omitempty"`
	UsedBytes            uint64   `json:"used_bytes,omitempty"`
	FreeBytes            uint64   `json:"free_bytes,omitempty"`
	MountPoint           string   `json:"mount_point,omitempty"`
	InterfaceCount       int      `json:"interface_count,omitempty"`
	ActiveInterfaceCount int      `json:"active_interface_count,omitempty"`
	RxBytes              uint64   `json:"rx_bytes,omitempty"`
	TxBytes              uint64   `json:"tx_bytes,omitempty"`
}

type runtimeHostTelemetry struct {
	CPU     runtimeMetricStatus
	Disk    runtimeMetricStatus
	Network runtimeMetricStatus
}

type linuxCPUReading struct {
	Total uint64
	Idle  uint64
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
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	database := h.runtimeDatabaseHealth(r.Context())
	redis := h.runtimeRedisHealth(r.Context())
	traffic := h.runtimeTrafficStatus(r.Context(), orgID)
	hostTelemetry := h.runtimeHostTelemetry()

	overallStatus := runtimeOverallStatus(database, redis, traffic)
	response := runtimeStatusResponse{
		Status:               overallStatus,
		GeneratedAt:          time.Now().UTC(),
		DataFreshnessSeconds: 0,
		RuntimeEnvironment:   h.runtimeEnvironmentStatus(),
		CapacityHealth: runtimeCapacityHealth{
			AppMemory: h.runtimeMemoryStatus(),
			Database:  database,
			Redis:     redis,
			Traffic:   traffic,
			CPU:       hostTelemetry.CPU,
			Disk:      hostTelemetry.Disk,
			Network:   hostTelemetry.Network,
		},
		ObservabilityIntegrations: h.runtimeObservabilityIntegrations(r.Context()),
		Topology:                  h.runtimeTopologyStatus(overallStatus, database, redis),
		EffectiveConfiguration:    h.runtimeOptions.EffectiveConfig,
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) runtimeTopologyStatus(appStatus string, database, redis runtimeDependencyStatus) runtimeTopologyStatus {
	return runtimeTopologyStatus{
		Scope:              "current_process",
		Visibility:         "redacted",
		NetworkExposure:    "not_observed",
		MultiInstanceState: "not_configured",
		Nodes: []runtimeTopologyNode{
			{ID: "service:uniroute", Kind: "service", Role: defaultRuntimeValue(h.runtimeOptions.DeploymentRole, "stable"), Status: appStatus},
			{ID: "dependency:database", Kind: "dependency", Role: "postgresql", Status: database.Status},
			{ID: "dependency:redis", Kind: "dependency", Role: "redis", Status: redis.Status},
		},
		Edges: []runtimeTopologyEdge{
			{From: "service:uniroute", To: "dependency:database", Kind: "runtime_dependency", Status: database.Status},
			{From: "service:uniroute", To: "dependency:redis", Kind: "runtime_dependency", Status: redis.Status},
		},
		Limitations: []string{
			"network_exposure_not_observed",
			"multi_instance_inventory_not_configured",
			"host_and_address_identifiers_redacted",
		},
	}
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

func (h *Handler) runtimeHostTelemetry() runtimeHostTelemetry {
	if h.runtimeHostTelemetryProvider != nil {
		return h.runtimeHostTelemetryProvider()
	}
	return collectRuntimeHostTelemetry()
}

func collectRuntimeHostTelemetry() runtimeHostTelemetry {
	return runtimeHostTelemetry{
		CPU:     runtimeCPUStatus(),
		Disk:    runtimeDiskStatus("/"),
		Network: runtimeNetworkStatus(),
	}
}

func runtimeCPUStatus() runtimeMetricStatus {
	status := runtimeMetricStatus{
		Status:       "available",
		LogicalCores: goruntime.NumCPU(),
	}
	first, ok := readLinuxCPUReading()
	if !ok {
		status.Reason = "usage_sample_not_supported"
		return status
	}
	const sampleWindow = 50 * time.Millisecond
	time.Sleep(sampleWindow)
	second, ok := readLinuxCPUReading()
	if !ok {
		status.Reason = "usage_sample_unavailable"
		return status
	}
	usage, ok := linuxCPUUsageRatio(first, second)
	if !ok {
		status.Reason = "usage_sample_invalid"
		return status
	}
	status.UsageRatio = &usage
	status.SampleMillis = sampleWindow.Milliseconds()
	return status
}

func readLinuxCPUReading() (linuxCPUReading, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return linuxCPUReading{}, false
	}
	line := bytes.SplitN(data, []byte("\n"), 2)[0]
	fields := strings.Fields(string(line))
	if len(fields) < 5 || fields[0] != "cpu" {
		return linuxCPUReading{}, false
	}
	var total uint64
	var idle uint64
	for i, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return linuxCPUReading{}, false
		}
		total += value
		if i == 3 || i == 4 {
			idle += value
		}
	}
	if total == 0 {
		return linuxCPUReading{}, false
	}
	return linuxCPUReading{Total: total, Idle: idle}, true
}

func linuxCPUUsageRatio(first, second linuxCPUReading) (float64, bool) {
	if second.Total <= first.Total || second.Idle < first.Idle {
		return 0, false
	}
	totalDelta := second.Total - first.Total
	if totalDelta == 0 {
		return 0, false
	}
	idleDelta := second.Idle - first.Idle
	if idleDelta > totalDelta {
		return 0, false
	}
	return float64(totalDelta-idleDelta) / float64(totalDelta), true
}

func runtimeDiskStatus(mountPoint string) runtimeMetricStatus {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPoint, &stat); err != nil {
		return unavailableRuntimeMetric("statfs_failed")
	}
	blockSize := uint64(stat.Bsize)
	if blockSize == 0 {
		return unavailableRuntimeMetric("invalid_block_size")
	}
	total := stat.Blocks * blockSize
	free := stat.Bavail * blockSize
	used := uint64(0)
	if total >= free {
		used = total - free
	}
	var usageRatio *float64
	if total > 0 {
		ratio := float64(used) / float64(total)
		usageRatio = &ratio
	}
	return runtimeMetricStatus{
		Status:     "available",
		UsageRatio: usageRatio,
		TotalBytes: total,
		UsedBytes:  used,
		FreeBytes:  free,
		MountPoint: mountPoint,
	}
}

func runtimeNetworkStatus() runtimeMetricStatus {
	interfaces, err := net.Interfaces()
	if err != nil {
		return unavailableRuntimeMetric("interfaces_unavailable")
	}
	status := runtimeMetricStatus{
		Status:         "available",
		InterfaceCount: len(interfaces),
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
			status.ActiveInterfaceCount++
		}
	}
	rxBytes, txBytes, ok := linuxNetworkBytes()
	if ok {
		status.RxBytes = rxBytes
		status.TxBytes = txBytes
	} else {
		status.Reason = "byte_counters_not_supported"
	}
	if status.ActiveInterfaceCount == 0 {
		status.Status = "unavailable"
		status.Reason = "no_active_network_interfaces"
	}
	return status
}

func linuxNetworkBytes() (uint64, uint64, bool) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, false
	}
	var rxTotal uint64
	var txTotal uint64
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" || name == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			continue
		}
		rxTotal += rx
		txTotal += tx
	}
	return rxTotal, txTotal, true
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

func (h *Handler) runtimeTrafficStatus(ctx context.Context, orgID int64) runtimeTrafficStatus {
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
	WHERE org_id = $1
	  AND created_at >= NOW() - ($2::int * INTERVAL '1 minute')
)
SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN status_code = 0 OR status_code >= 500 THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(total_tokens), 0),
	COALESCE(SUM(cost_cents), 0),
	COALESCE((SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) FROM recent WHERE latency_ms > 0), 0),
	COALESCE((SELECT percentile_cont(0.99) WITHIN GROUP (ORDER BY latency_ms) FROM recent WHERE latency_ms > 0), 0)
FROM recent`, orgID, runtimeTrafficWindowMinutes).Scan(
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
	healthURL := monitoringURL(rawURL, "/-/ready")
	status := runtimeIntegrationStatus{
		Status:    "unknown",
		URL:       monitoringDisplayURL(rawURL),
		HealthURL: monitoringDisplayURL(healthURL),
		CheckedAt: &checkedAt,
	}

	healthResp, err := monitoringGet(ctx, healthURL)
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
	healthURL := monitoringURL(rawURL, "/api/health")
	status := runtimeIntegrationStatus{
		Status:    "unknown",
		URL:       monitoringDisplayURL(rawURL),
		HealthURL: monitoringDisplayURL(healthURL),
		CheckedAt: &checkedAt,
	}
	resp, err := monitoringGet(ctx, healthURL)
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
	return runtimeIntegrationStatus{Status: "configured", URL: monitoringDisplayURL(rawURL)}
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

func monitoringDisplayURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
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
