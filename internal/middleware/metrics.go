package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "unirouter_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "unirouter_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
		},
		[]string{"method", "path"},
	)

	tokensProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "unirouter_tokens_processed_total",
			Help: "Total tokens processed",
		},
		[]string{"type", "model"}, // type: prompt/completion
	)

	upstreamLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "unirouter_upstream_latency_seconds",
			Help:    "Upstream provider latency in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
		},
		[]string{"provider", "model"},
	)

	activeRequests = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "unirouter_active_requests",
			Help: "Number of active requests",
		},
	)

	billingFreezeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "unirouter_billing_freeze_total",
			Help: "Total billing freeze operations",
		},
		[]string{"status"}, // success, error
	)
)

// MetricsMiddleware records HTTP metrics for each request.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		activeRequests.Inc()
		defer activeRequests.Dec()

		// Wrap response writer to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(rw.statusCode)
		path := r.URL.Path

		httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

// RecordTokenUsage records token counts for a model.
func RecordTokenUsage(tokenType, model string, count int) {
	tokensProcessed.WithLabelValues(tokenType, model).Add(float64(count))
}

// RecordUpstreamLatency records upstream provider latency.
func RecordUpstreamLatency(provider, model string, duration time.Duration) {
	upstreamLatency.WithLabelValues(provider, model).Observe(duration.Seconds())
}

// RecordBillingFreeze records a billing freeze operation.
func RecordBillingFreeze(status string) {
	billingFreezeTotal.WithLabelValues(status).Inc()
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// Unwrap lets http.ResponseController reach the original server writer. SSE
// handlers use it to renew per-write deadlines beyond the server's aggregate
// WriteTimeout without weakening timeout protection for non-stream handlers.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush proxies http.Flusher so SSE streaming works through MetricsMiddleware.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
