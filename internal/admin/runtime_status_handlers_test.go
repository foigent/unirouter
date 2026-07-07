package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRuntimeStatus_ReturnsRuntimeHealthAndUnavailableTelemetry(t *testing.T) {
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
	if resp.CapacityHealth.CPU.Status != "unavailable" || resp.CapacityHealth.Network.Reason != "telemetry_source_not_configured" {
		t.Fatalf("expected unavailable host telemetry, got cpu=%#v network=%#v", resp.CapacityHealth.CPU, resp.CapacityHealth.Network)
	}
	if resp.ObservabilityIntegrations.Prometheus.Status != "connected" || resp.ObservabilityIntegrations.Prometheus.ScrapeStatus != "up" {
		t.Fatalf("unexpected observability integrations: %#v", resp.ObservabilityIntegrations)
	}
	if resp.ObservabilityIntegrations.Grafana.Status != "connected" || resp.ObservabilityIntegrations.Grafana.Version != "11.0.0" {
		t.Fatalf("unexpected grafana integration: %#v", resp.ObservabilityIntegrations.Grafana)
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
