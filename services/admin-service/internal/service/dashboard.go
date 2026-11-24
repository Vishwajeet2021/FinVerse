package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Metrics reads operational numbers from Prometheus.
type Metrics interface {
	// Scalar evaluates an instant query that yields at most one value (0 when empty).
	Scalar(ctx context.Context, promQL string) (float64, error)
	// Firing lists the names of alerts currently firing.
	Firing(ctx context.Context) ([]string, error)
}

// Section is one dashboard panel. Available is false when its source could not be read, so
// the page can say "unavailable" instead of showing a misleading zero (docs/23 D-A5).
type Section[T any] struct {
	Available bool `json:"available"`
	Data      T    `json:"data,omitempty"`
}

type FraudPanel struct {
	OpenCases int             `json:"openCases"`
	Recent    json.RawMessage `json:"recent"`
}

type PaymentsPanel struct {
	Initiated1h int64 `json:"initiated1h"`
	Completed1h int64 `json:"completed1h"`
	Failed1h    int64 `json:"failed1h"`
}

type OpsPanel struct {
	DeadLetterDepth int64    `json:"deadLetterDepth"`
	FiringAlerts    []string `json:"firingAlerts"`
	ServicesDown    int64    `json:"servicesDown"`
}

type Dashboard struct {
	GeneratedAt time.Time              `json:"generatedAt"`
	Fraud       Section[FraudPanel]    `json:"fraud"`
	Payments    Section[PaymentsPanel] `json:"payments"`
	Ops         Section[OpsPanel]      `json:"ops"`
}

const panelTimeout = 3 * time.Second

// Dashboard gathers every panel concurrently, each under its own short timeout, so one slow
// or failed source costs only its own panel.
func (s *Service) Dashboard(ctx context.Context, bearer string, m Metrics) Dashboard {
	d := Dashboard{GeneratedAt: s.now().UTC()}
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		c, cancel := context.WithTimeout(ctx, panelTimeout)
		defer cancel()
		recent, total, err := s.fraud.OpenCases(c, bearer, 10)
		if err != nil {
			s.logf("dashboard: fraud panel unavailable", "error", err)
			return
		}
		d.Fraud = Section[FraudPanel]{Available: true, Data: FraudPanel{OpenCases: total, Recent: recent}}
	}()

	go func() {
		defer wg.Done()
		if m == nil {
			return
		}
		c, cancel := context.WithTimeout(ctx, panelTimeout)
		defer cancel()
		p := PaymentsPanel{}
		for _, q := range []struct {
			status string
			dst    *int64
		}{{"INITIATED", &p.Initiated1h}, {"COMPLETED", &p.Completed1h}, {"FAILED", &p.Failed1h}} {
			v, err := m.Scalar(c, `sum(increase(finverse_payments_total{status="`+q.status+`"}[1h]))`)
			if err != nil {
				s.logf("dashboard: payments panel unavailable", "error", err)
				return
			}
			*q.dst = int64(v + 0.5)
		}
		d.Payments = Section[PaymentsPanel]{Available: true, Data: p}
	}()

	go func() {
		defer wg.Done()
		if m == nil {
			return
		}
		c, cancel := context.WithTimeout(ctx, panelTimeout)
		defer cancel()
		dlq, err1 := m.Scalar(c, `sum(max by (stream, group) (finverse_eventbus_dlq_depth))`)
		down, err2 := m.Scalar(c, `count(up{job="finverse"} == 0)`)
		firing, err3 := m.Firing(c)
		if err1 != nil || err2 != nil || err3 != nil {
			s.logf("dashboard: ops panel unavailable", "dlq", err1, "down", err2, "alerts", err3)
			return
		}
		if firing == nil {
			firing = []string{}
		}
		d.Ops = Section[OpsPanel]{Available: true, Data: OpsPanel{DeadLetterDepth: int64(dlq + 0.5), FiringAlerts: firing, ServicesDown: int64(down + 0.5)}}
	}()

	wg.Wait()
	return d
}
