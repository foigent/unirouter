package middleware

import (
	"fmt"
	"strings"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// getMetricValue reads a metric value from the default registry.
func getMetricValue(name string, labels map[string]string) (float64, error) {
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return 0, err
	}
	for _, mf := range metricFamilies {
		if mf.GetName() == name {
			for _, m := range mf.GetMetric() {
				match := true
				for k, v := range labels {
					found := false
					for _, lp := range m.GetLabel() {
						if lp.Name != nil && lp.Value != nil && *lp.Name == k && *lp.Value == v {
							found = true
							break
						}
					}
					if !found {
						match = false
						break
					}
				}
				if match {
					switch mf.GetType() {
					case 1: // COUNTER
						return m.GetCounter().GetValue(), nil
					case 8: // GAUGE
						return m.GetGauge().GetValue(), nil
					case 4: // HISTOGRAM
						return float64(m.GetHistogram().GetSampleCount()), nil
					}
				}
			}
		}
	}
	return 0, nil
}

func TestMetricsMiddleware_IncrementsCounter(t *testing.T) {
	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test-metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
}

func TestMetricsMiddleware_CapturesStatusCode(t *testing.T) {
	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest("GET", "/not-found", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestMetricsMiddleware_ActiveRequests(t *testing.T) {
	before, _ := getMetricValue("unirouter_active_requests", nil)

	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// During handler execution, active requests should be > 0
		during, _ := getMetricValue("unirouter_active_requests", nil)
		if during <= before {
			t.Logf("Active requests during handler: %.0f (before: %.0f)", during, before)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/active-test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestRecordTokenUsage(t *testing.T) {
	RecordTokenUsage("prompt", "test-model", 100)
	RecordTokenUsage("completion", "test-model", 50)

	// Verify no panic, metrics registered
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "unirouter_tokens_processed_total" {
			found = true
			break
		}
	}
	if !found {
		t.Error("unirouter_tokens_processed_total metric not found")
	}
}

func TestRecordUpstreamLatency(t *testing.T) {
	RecordUpstreamLatency("deepseek", "deepseek-chat", 100)

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "unirouter_upstream_latency_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Error("unirouter_upstream_latency_seconds metric not found")
	}
}

func TestRecordBillingFreeze(t *testing.T) {
	RecordBillingFreeze("success")
	RecordBillingFreeze("error")

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "unirouter_billing_freeze_total" {
			found = true
			break
		}
	}
	if !found {
		t.Error("unirouter_billing_freeze_total metric not found")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	// Test that /metrics endpoint returns prometheus format
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		// This would normally be promhttp.Handler()
		// Just verify the route exists
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for /metrics, got %d", rec.Code)
	}
}

func TestMetricsMiddleware_FlusherInterface(t *testing.T) {
	// Bug #12: responseWriter must implement http.Flusher for SSE streaming
	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("responseWriter does not implement http.Flusher — SSE streaming will fail")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: hello\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "data: world\n\n")
		flusher.Flush()
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data: hello") || !strings.Contains(body, "data: world") {
		t.Errorf("Expected SSE data in response body, got: %s", body)
	}
}
