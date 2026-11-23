package postgres

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// paymentsTotal is the payment funnel (docs/16-observability.md §6): one
// increment per state a payment enters, counted only after the write commits.
var paymentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "finverse_payments_total",
	Help: "Payments by the status they entered (INITIATED, PROCESSING, COMPLETED, FAILED).",
}, []string{"status"})

func countOnSuccess(status string, err error) error {
	if err == nil {
		paymentsTotal.WithLabelValues(status).Inc()
	}
	return err
}
