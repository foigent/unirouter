package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRuntimeStatus_ReturnsRuntimeHealthAndHostTelemetry(t *testing.T) {
	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/-/ready":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
		case "/api/v1/query":
			if got := r.URL.Query().Get("query"); got != prometheusUniRouterTargetQuery {
				t.Fatalf("unexpected prometheus query: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"value":[1783420000,"1"]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer prometheus.Close()
	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"database":"ok","version":"11.0.0"}`))
	}))
	defer grafana.Close()

	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "SELECT 1") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			}
			if strings.Contains(sql, "FROM call_logs") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*(dest[0].(*int64)) = 40
					*(dest[1].(*int64)) = 1
					*(dest[2].(*int64)) = 1200
					*(dest[3].(*int64)) = 88
					*(dest[4].(*float64)) = 240
					*(dest[5].(*float64)) = 310
					return nil
				}}
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return fmt.Errorf("unexpected query") }}
		},
	}
	h := NewHandler(db, "jwt-secret", []byte("0123456789abcdef01234567"), nil)
	h.SetRuntimeStatusOptions(RuntimeStatusOptions{
		Environment:    "staging",
		DeploymentMode: "docker-compose",
		DeploymentRole: "canary",
		Version:        "2026.07.07",
		Commit:         "abc1234",
		PrometheusURL:  prometheus.URL,
		GrafanaURL:     grafana.URL,
	})
	cpuUsage := 0.25
	diskUsage := 0.70
	h.runtimeHostTelemetryProvider = func() runtimeHostTelemetry {
		return runtimeHostTelemetry{
			CPU: runtimeMetricStatus{
				Status:       "available",
				UsageRatio:   &cpuUsage,
				LogicalCores: 8,
				SampleMillis: 50,
			},
			Disk: runtimeMetricStatus{
				Status:     "available",
				UsageRatio: &diskUsage,
				TotalBytes: 1000,
				UsedBytes:  700,
				FreeBytes:  300,
				MountPoint: "/",
			},
			Network: runtimeMetricStatus{
				Status:               "available",
				InterfaceCount:       3,
				ActiveInterfaceCount: 2,
				RxBytes:              1024,
				TxBytes:              2048,
			},
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/status", nil)
	rec := httptest.NewRecorder()
	h.RuntimeStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp runtimeStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Status != "warning" {
		t.Fatalf("expected warning because redis is not configured, got %s", resp.Status)
	}
	if resp.RuntimeEnvironment.Environment != "staging" || resp.RuntimeEnvironment.DeploymentRole != "canary" {
		t.Fatalf("unexpected runtime environment: %#v", resp.RuntimeEnvironment)
	}
	if resp.CapacityHealth.Database.Status != "connected" {
		t.Fatalf("expected database connected, got %#v", resp.CapacityHealth.Database)
	}
	if resp.CapacityHealth.Redis.Status != "unavailable" || resp.CapacityHealth.Redis.Reason != "redis_client_not_configured" {
		t.Fatalf("expected redis unavailable, got %#v", resp.CapacityHealth.Redis)
	}
	if resp.CapacityHealth.Traffic.Requests != 40 || math.Abs(resp.CapacityHealth.Traffic.ErrorRate-0.025) > 0.000001 {
		t.Fatalf("unexpected traffic status: %#v", resp.CapacityHealth.Traffic)
	}
	if resp.CapacityHealth.CPU.Status != "available" || resp.CapacityHealth.CPU.UsageRatio == nil || math.Abs(*resp.CapacityHealth.CPU.UsageRatio-0.25) > 0.000001 {
		t.Fatalf("expected available cpu telemetry, got %#v", resp.CapacityHealth.CPU)
	}
	if resp.CapacityHealth.Disk.Status != "available" || resp.CapacityHealth.Disk.TotalBytes != 1000 || resp.CapacityHealth.Disk.MountPoint != "/" {
		t.Fatalf("expected available disk telemetry, got %#v", resp.CapacityHealth.Disk)
	}
	if resp.CapacityHealth.Network.Status != "available" || resp.CapacityHealth.Network.ActiveInterfaceCount != 2 || resp.CapacityHealth.Network.RxBytes != 1024 {
		t.Fatalf("expected available network telemetry, got %#v", resp.CapacityHealth.Network)
	}
	if resp.ObservabilityIntegrations.Prometheus.Status != "connected" || resp.ObservabilityIntegrations.Prometheus.ScrapeStatus != "up" {
		t.Fatalf("unexpected observability integrations: %#v", resp.ObservabilityIntegrations)
	}
	if resp.ObservabilityIntegrations.Grafana.Status != "connected" || resp.ObservabilityIntegrations.Grafana.Version != "11.0.0" {
		t.Fatalf("unexpected grafana integration: %#v", resp.ObservabilityIntegrations.Grafana)
	}
}

func TestLinuxCPUUsageRatio(t *testing.T) {
	usage, ok := linuxCPUUsageRatio(
		linuxCPUReading{Total: 1000, Idle: 600},
		linuxCPUReading{Total: 1200, Idle: 700},
	)
	if !ok {
		t.Fatal("expected valid cpu usage sample")
	}
	if math.Abs(usage-0.5) > 0.000001 {
		t.Fatalf("expected 50%% usage, got %f", usage)
	}
	if _, ok := linuxCPUUsageRatio(linuxCPUReading{Total: 1000, Idle: 600}, linuxCPUReading{Total: 900, Idle: 650}); ok {
		t.Fatal("expected invalid sample when total decreases")
	}
	if _, ok := linuxCPUUsageRatio(linuxCPUReading{Total: 1000, Idle: 600}, linuxCPUReading{Total: 1100, Idle: 750}); ok {
		t.Fatal("expected invalid sample when idle delta exceeds total delta")
	}
}

func TestRuntimeStatus_DatabaseFailureReturnsDegradedPartialStatus(t *testing.T) {
	h := NewHandler(&testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return fmt.Errorf("database down") }}
		},
	}, "jwt-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/status", nil)
	rec := httptest.NewRecorder()
	h.RuntimeStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp runtimeStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "degraded" {
		t.Fatalf("expected degraded when database probe fails, got %s", resp.Status)
	}
	if resp.CapacityHealth.Database.Status != "unavailable" || resp.CapacityHealth.Traffic.Status != "unavailable" {
		t.Fatalf("expected database and traffic unavailable, got db=%#v traffic=%#v", resp.CapacityHealth.Database, resp.CapacityHealth.Traffic)
	}
}

func TestRuntimeStatus_ReportsExternalMonitoringProblems(t *testing.T) {
	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/-/ready":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/query":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer prometheus.Close()
	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer grafana.Close()

	h := NewHandler(nil, "jwt-secret", []byte("0123456789abcdef01234567"), nil)
	h.SetRuntimeStatusOptions(RuntimeStatusOptions{
		PrometheusURL: prometheus.URL,
		GrafanaURL:    grafana.URL,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/status", nil)
	rec := httptest.NewRecorder()
	h.RuntimeStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp runtimeStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ObservabilityIntegrations.Prometheus.Status != "stale" || resp.ObservabilityIntegrations.Prometheus.Reason != "unirouter_target_missing" {
		t.Fatalf("expected prometheus stale missing target, got %#v", resp.ObservabilityIntegrations.Prometheus)
	}
	if resp.ObservabilityIntegrations.Grafana.Status != "misconfigured" || resp.ObservabilityIntegrations.Grafana.Reason != "auth_required" {
		t.Fatalf("expected grafana auth required, got %#v", resp.ObservabilityIntegrations.Grafana)
	}
}

func TestRuntimeStatus_RedactsMonitoringURLsButUsesRawProbeURL(t *testing.T) {
	prometheusSecret := "prometheus-secret"
	grafanaSecret := "grafana-secret"
	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "ops" || pass != prometheusSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/-/ready":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/query":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"value":[1783420000,"1"]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer prometheus.Close()
	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "ops" || pass != grafanaSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"database":"ok","version":"11.0.0"}`))
	}))
	defer grafana.Close()

	h := NewHandler(nil, "jwt-secret", []byte("0123456789abcdef01234567"), nil)
	h.SetRuntimeStatusOptions(RuntimeStatusOptions{
		PrometheusURL: monitoringURLWithSecret(t, prometheus.URL, "ops", prometheusSecret, "token=prom-token"),
		GrafanaURL:    monitoringURLWithSecret(t, grafana.URL, "ops", grafanaSecret, "api_key=grafana-token"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/status", nil)
	rec := httptest.NewRecorder()
	h.RuntimeStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp runtimeStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ObservabilityIntegrations.Prometheus.Status != "connected" {
		t.Fatalf("expected prometheus probe to use raw credentials, got %#v", resp.ObservabilityIntegrations.Prometheus)
	}
	if resp.ObservabilityIntegrations.Grafana.Status != "connected" {
		t.Fatalf("expected grafana probe to use raw credentials, got %#v", resp.ObservabilityIntegrations.Grafana)
	}
	encoded := rec.Body.String()
	for _, secret := range []string{prometheusSecret, grafanaSecret, "prom-token", "grafana-token", "ops@"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("runtime status response leaked monitoring secret %q: %s", secret, encoded)
		}
	}
	if strings.Contains(resp.ObservabilityIntegrations.Prometheus.URL, "?") || strings.Contains(resp.ObservabilityIntegrations.Prometheus.HealthURL, "?") {
		t.Fatalf("expected prometheus display URLs without query: %#v", resp.ObservabilityIntegrations.Prometheus)
	}
}

func TestRuntimeOverallStatus(t *testing.T) {
	if got := runtimeOverallStatus(runtimeDependencyStatus{Status: "unavailable"}, runtimeDependencyStatus{Status: "connected"}, runtimeTrafficStatus{Status: "available"}); got != "degraded" {
		t.Fatalf("database unavailable: got %s", got)
	}
	if got := runtimeOverallStatus(runtimeDependencyStatus{Status: "connected"}, runtimeDependencyStatus{Status: "unavailable"}, runtimeTrafficStatus{Status: "available"}); got != "warning" {
		t.Fatalf("redis unavailable: got %s", got)
	}
	if got := runtimeOverallStatus(runtimeDependencyStatus{Status: "connected"}, runtimeDependencyStatus{Status: "connected"}, runtimeTrafficStatus{Status: "available", Requests: 40, ErrorRate: 0.1}); got != "warning" {
		t.Fatalf("high error rate: got %s", got)
	}
	if got := runtimeOverallStatus(runtimeDependencyStatus{Status: "connected"}, runtimeDependencyStatus{Status: "connected"}, runtimeTrafficStatus{Status: "available", Requests: 10, ErrorRate: 0.1}); got != "operational" {
		t.Fatalf("small sample should stay operational, got %s", got)
	}
}

func monitoringURLWithSecret(t *testing.T, rawURL, username, password, rawQuery string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	parsed.User = url.UserPassword(username, password)
	parsed.RawQuery = rawQuery
	return parsed.String()
}
