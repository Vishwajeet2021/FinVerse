package telemetry

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// RED metrics (docs/16-observability.md §4). The route label is the matched
// route template ("/api/v1/payments/:id"), never the raw path, so cardinality
// stays bounded no matter how many ids are requested.
var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "finverse_http_requests_total",
		Help: "HTTP requests handled, by service, method, route template and status code.",
	}, []string{"service", "method", "route", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "finverse_http_request_duration_seconds",
		Help:    "HTTP request latency, by service, method and route template.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"service", "method", "route"})
)

// withMetrics runs inner (the tracing middleware, which calls c.Next itself)
// and then records one counter increment and one histogram observation, so
// the measured duration includes the span work and every later handler.
func withMetrics(serviceName string, inner gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		inner(c)
		route := c.FullPath()
		if route == "" {
			route = "unmatched" // 404s: do not label by the attacker-controlled path
		}
		httpRequests.WithLabelValues(serviceName, c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		httpDuration.WithLabelValues(serviceName, c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}

// MetricsHandler serves the Prometheus exposition format on /metrics.
func MetricsHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) { h.ServeHTTP(c.Writer, c.Request) }
}
