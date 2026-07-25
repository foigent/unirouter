package completedfeed

import "github.com/prometheus/client_golang/prometheus"

var (
	feedRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unirouter_completed_execution_feed_requests_total",
		Help: "Completed-execution feed HTTP requests by bounded outcome.",
	}, []string{"result"})
	feedItems = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "unirouter_completed_execution_feed_items_total",
		Help: "Completed-execution feed items returned to authorized readers.",
	})
	feedBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "unirouter_completed_execution_feed_bytes_total",
		Help: "Decoded JSON bytes returned by the completed-execution feed.",
	})
	feedMaterialized = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unirouter_completed_execution_feed_materialized_total",
		Help: "Billing-terminal events materialized into the retained feed.",
	}, []string{"terminal_status", "result"})
	feedCursorLag = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "unirouter_completed_execution_feed_cursor_lag_items",
		Help: "Items remaining after the most recently served authorized cursor.",
	})
	feedOldestReadyAge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "unirouter_completed_execution_feed_oldest_ready_age_seconds",
		Help: "Age of the oldest item remaining after the most recently served authorized cursor.",
	})
	feedRetentionDeleted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "unirouter_completed_execution_feed_retention_deleted_total",
		Help: "Completed-execution feed items deleted after the fixed replay window.",
	})
)

func init() {
	prometheus.MustRegister(
		feedRequests,
		feedItems,
		feedBytes,
		feedMaterialized,
		feedCursorLag,
		feedOldestReadyAge,
		feedRetentionDeleted,
	)
}
