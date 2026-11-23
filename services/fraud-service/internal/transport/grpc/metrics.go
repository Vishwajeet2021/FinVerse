package grpc

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Buckets straddle the 80 ms NFR-PERF-4 budget so the SLO is readable
	// directly from the histogram.
	checkDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "finverse_fraud_check_duration_seconds",
		Help:    "CheckTransaction latency, tracked against the 80 ms budget.",
		Buckets: []float64{.001, .0025, .005, .01, .025, .05, .08, .15, .3},
	})
	checksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "finverse_fraud_checks_total",
		Help: "CheckTransaction results by verdict (ALLOW, REVIEW, DENY, ERROR).",
	}, []string{"verdict"})
)
