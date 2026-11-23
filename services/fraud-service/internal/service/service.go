// Package service is Fraud Service's use-case layer. CheckTransaction is the
// hot path (Redis only); case recording happens off the request path so a slow
// Postgres can never push the check past its latency budget (NFR-PERF-4).
package service

import (
	"context"
	"errors"
	"time"

	"github.com/finverse/fraud-service/internal/rules"
	"go.uber.org/zap"
)

const (
	EventFraudCaseOpened   = "FraudCaseOpened"
	EventFraudCaseResolved = "FraudCaseResolved"
)

var (
	ErrCaseNotFound   = errors.New("fraud case not found")
	ErrCaseNotOpen    = errors.New("fraud case is already resolved")
	ErrInvalidOutcome = errors.New("resolution must be CONFIRMED or DISMISSED")
)

type Case struct {
	ID          string     `json:"id"`
	AccountID   string     `json:"accountId"`
	PaymentID   string     `json:"paymentId,omitempty"`
	AmountMinor int64      `json:"amountMinor"`
	Verdict     string     `json:"verdict"`
	Rule        string     `json:"rule"`
	Reason      string     `json:"reason"`
	RiskScore   int        `json:"riskScore"`
	Status      string     `json:"status"`
	Resolution  *string    `json:"resolution,omitempty"`
	ResolvedBy  *string    `json:"resolvedBy,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
}

// CaseRepository writes a case together with its outbox event atomically.
type CaseRepository interface {
	OpenCase(ctx context.Context, c Case, traceParent *string) error
	List(ctx context.Context, status string, limit int) ([]Case, error)
	Resolve(ctx context.Context, id, outcome, actor string, traceParent *string) (Case, error)
}

type Request struct {
	AccountID, CounterpartyRef, Rail, IdempotencyKey, PaymentID string
	AmountMinor                                                 int64
}

type Service struct {
	engine *rules.Engine
	repo   CaseRepository
	logger *zap.Logger
	cases  chan Case
}

func New(engine *rules.Engine, repo CaseRepository, logger *zap.Logger) *Service {
	return &Service{engine: engine, repo: repo, logger: logger, cases: make(chan Case, 1024)}
}

// Check evaluates the rules. An error means the verdict is *unknown* (e.g.
// Redis down) — the caller (Payment) decides fail-open vs fail-closed.
func (s *Service) Check(ctx context.Context, r Request) (rules.Decision, error) {
	d, err := s.engine.Evaluate(ctx, rules.Request{AccountID: r.AccountID, AmountMinor: r.AmountMinor, Rail: r.Rail})
	if err != nil {
		return rules.Decision{}, err
	}
	if d.Verdict != rules.Allow {
		c := Case{AccountID: r.AccountID, PaymentID: r.PaymentID, AmountMinor: r.AmountMinor,
			Verdict: string(d.Verdict), Rule: d.Rule, Reason: d.Reason, RiskScore: d.RiskScore, CreatedAt: time.Now()}
		select {
		case s.cases <- c:
		default:
			// Never block the hot path on case bookkeeping; the verdict
			// itself is unaffected. Surfaced loudly so it isn't silent.
			s.logger.Error("fraud case queue full, case dropped", zap.String("account_id", r.AccountID), zap.String("rule", d.Rule))
		}
	}
	return d, nil
}

// RunCaseWriter persists queued cases until ctx ends. Failed writes are
// retried a few times, then logged — a dropped case is visible in logs and
// the verdict (and the Redis counters) already took effect.
func (s *Service) RunCaseWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-s.cases:
			var err error
			for attempt := 0; attempt < 3; attempt++ {
				if err = s.repo.OpenCase(ctx, c, nil); err == nil {
					break
				}
				time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
			}
			if err != nil {
				s.logger.Error("persisting fraud case failed", zap.Error(err), zap.String("rule", c.Rule))
			}
		}
	}
}

func (s *Service) ListCases(ctx context.Context, status string, limit int) ([]Case, error) {
	return s.repo.List(ctx, status, limit)
}

func (s *Service) ResolveCase(ctx context.Context, id, outcome, actor string) (Case, error) {
	if outcome != "CONFIRMED" && outcome != "DISMISSED" {
		return Case{}, ErrInvalidOutcome
	}
	return s.repo.Resolve(ctx, id, outcome, actor, nil)
}
