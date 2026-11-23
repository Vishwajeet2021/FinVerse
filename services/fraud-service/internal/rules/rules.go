// Package rules is Fraud Service's decision engine: pure rule evaluation over
// counters, no I/O of its own beyond the Counters interface. Keeping Redis
// behind an interface is what lets the rules be unit-tested exhaustively and
// keeps CheckTransaction on its 80ms budget (NFR-PERF-4) — one Redis round
// trip, never Postgres.
package rules

import (
	"context"
	"fmt"
	"time"
)

type Verdict string

const (
	Allow  Verdict = "ALLOW"
	Review Verdict = "REVIEW" // proceeds, but opens a case for an analyst
	Deny   Verdict = "DENY"
)

const (
	RuleSingleTxnLimit = "SINGLE_TXN_LIMIT"
	RuleDailyLimit     = "DAILY_LIMIT"
	RuleVelocity       = "VELOCITY"
	RuleReviewAmount   = "REVIEW_AMOUNT"
)

type Config struct {
	SingleTxnLimitMinor  int64
	ReviewThresholdMinor int64
	DailyLimitMinor      int64
	VelocityMaxPerMin    int64

	// RTGS is a high-value rail (minimum INR 2,00,000), so the retail single
	// transfer cap and daily limit would reject every RTGS payment. Zero means
	// "use the retail value".
	RTGSSingleTxnLimitMinor int64
	RTGSDailyLimitMinor     int64
}

const railRTGS = "RTGS"

func (c Config) singleLimit(rail string) int64 {
	if rail == railRTGS && c.RTGSSingleTxnLimitMinor > 0 {
		return c.RTGSSingleTxnLimitMinor
	}
	return c.SingleTxnLimitMinor
}

func (c Config) dailyLimit(rail string) int64 {
	if rail == railRTGS && c.RTGSDailyLimitMinor > 0 {
		return c.RTGSDailyLimitMinor
	}
	return c.DailyLimitMinor
}

type Request struct {
	AccountID   string
	AmountMinor int64
	Rail        string // optional; selects per-rail limits
}

type Decision struct {
	Verdict   Verdict
	RiskScore int
	Rule      string
	Reason    string // customer-safe: never reveals thresholds
}

// Counters is the Redis-backed state the rules read. Record is called once
// per evaluated request and returns the *post-increment* values, so a single
// atomic round trip both counts this transaction and reports the totals.
type Counters interface {
	Record(ctx context.Context, accountID string, amountMinor int64, now time.Time) (perMinute int64, dailyTotalMinor int64, err error)
}

type Engine struct {
	cfg      Config
	counters Counters
	now      func() time.Time
}

func NewEngine(cfg Config, counters Counters) *Engine {
	return &Engine{cfg: cfg, counters: counters, now: time.Now}
}

// Evaluate applies the rules in order of cost: the stateless amount cap first
// (no Redis call at all for an obviously-too-large payment), then velocity and
// daily-total, then the review threshold. Note denied attempts still count
// toward velocity — repeatedly hammering a limit is itself the signal.
func (e *Engine) Evaluate(ctx context.Context, r Request) (Decision, error) {
	if limit := e.cfg.singleLimit(r.Rail); limit > 0 && r.AmountMinor > limit {
		return Decision{Verdict: Deny, RiskScore: 95, Rule: RuleSingleTxnLimit,
			Reason: "This amount is above the limit for a single transfer."}, nil
	}

	perMin, daily, err := e.counters.Record(ctx, r.AccountID, r.AmountMinor, e.now())
	if err != nil {
		return Decision{}, fmt.Errorf("recording counters: %w", err)
	}

	if e.cfg.VelocityMaxPerMin > 0 && perMin > e.cfg.VelocityMaxPerMin {
		return Decision{Verdict: Deny, RiskScore: 90, Rule: RuleVelocity,
			Reason: "Too many transfers in a short time. Please try again in a minute."}, nil
	}
	dailyLimit := e.cfg.dailyLimit(r.Rail)
	if dailyLimit > 0 && daily > dailyLimit {
		return Decision{Verdict: Deny, RiskScore: 92, Rule: RuleDailyLimit,
			Reason: "This transfer would exceed your daily limit."}, nil
	}
	if e.cfg.ReviewThresholdMinor > 0 && r.Rail != railRTGS && r.AmountMinor >= e.cfg.ReviewThresholdMinor {
		return Decision{Verdict: Review, RiskScore: 60, Rule: RuleReviewAmount,
			Reason: "Large transfer flagged for review."}, nil
	}

	score := 0
	if dailyLimit > 0 {
		score = int(40 * daily / dailyLimit)
		if score > 40 {
			score = 40
		}
	}
	return Decision{Verdict: Allow, RiskScore: score}, nil
}
