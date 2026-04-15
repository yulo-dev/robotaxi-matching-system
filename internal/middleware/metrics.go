package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// --- HTTP metrics ---

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "robotaxi_http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "robotaxi_http_request_duration_seconds",
		Help:    "HTTP request duration",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	}, []string{"method", "path"})
)

// --- Business metrics ---

var (
	FaresCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "robotaxi_fares_created_total",
		Help: "Total fare estimates created",
	})

	RidesRequested = promauto.NewCounter(prometheus.CounterOpts{
		Name: "robotaxi_rides_requested_total",
		Help: "Total rides requested",
	})

	RidesMatched = promauto.NewCounter(prometheus.CounterOpts{
		Name: "robotaxi_rides_matched_total",
		Help: "Total rides successfully matched",
	})

	RidesFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "robotaxi_rides_failed_total",
		Help: "Total rides that failed matching",
	})

	MatchLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "robotaxi_match_latency_seconds",
		Help:    "Time to match a ride to an AV",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	})

	DispatchAttempts = promauto.NewCounter(prometheus.CounterOpts{
		Name: "robotaxi_dispatch_attempts_total",
		Help: "Total dispatch attempts (accept + reject)",
	})

	DispatchRejections = promauto.NewCounter(prometheus.CounterOpts{
		Name: "robotaxi_dispatch_rejections_total",
		Help: "Total dispatch rejections by AVs",
	})

	UniqueIndexViolations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "robotaxi_unique_index_violations_total",
		Help: "Duplicate AV assignment attempts blocked by unique index",
	})

	LockContentions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "robotaxi_lock_contentions_total",
		Help: "Per-ride lock contention events",
	})

	ActiveAVs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "robotaxi_active_avs",
		Help: "Current AV count by status",
	}, []string{"status"})

	ActiveWSConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "robotaxi_ws_connections_active",
		Help: "Current WebSocket connections",
	})

	LocationUpdatesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "robotaxi_location_updates_total",
		Help: "Total AV location updates received",
	})

	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "robotaxi_matching_queue_depth",
		Help: "Current matching queue depth",
	})
)

// PrometheusMiddleware records HTTP request metrics
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		httpRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}
