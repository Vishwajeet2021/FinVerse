package service

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	transactionsPosted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "finverse_ledger_transactions_posted_total",
		Help: "Ledger transactions committed.",
	})
	balanceCheckFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "finverse_ledger_balance_check_failures_total",
		Help: "Postings rejected because their legs did not sum to zero. Must always be 0.",
	})
)
