package service

import (
	"fmt"
	"time"

	"github.com/finverse/payment-service/internal/domain"
)

// Rail simulation (FR-PAY-1). These model the *shape* of each Indian rail -
// limits and how settlement behaves - not real clearing:
//
//	INTERNAL  synchronous, ledger posted inside the request (own-bank transfer)
//	UPI       instant (seconds), capped at INR 1,00,000
//	IMPS      instant (seconds), capped at INR 5,00,000
//	NEFT      settles in batches (real: half-hourly; here NeftBatch, default 30s)
//	RTGS      real-time gross settlement, minimum INR 2,00,000, no cap
//
// Settlement delays are deliberately short so the lifecycle is observable
// locally; production-like timings are a config change (NeftBatch) plus the
// constants below.
const (
	upiMaxMinor  = 100_000_00
	impsMaxMinor = 500_000_00
	rtgsMinMinor = 200_000_00

	upiDelay  = 2 * time.Second
	impsDelay = 3 * time.Second
	rtgsDelay = 5 * time.Second
)

type RailConfig struct {
	// NeftBatch is the NEFT batch interval; NEFT payments settle at the next
	// multiple of it.
	NeftBatch time.Duration
	// FailureRate (0..1) is the simulated chance the beneficiary side rejects
	// a payment at settlement (returned funds, closed account, ...).
	FailureRate float64
}

func DefaultRailConfig() RailConfig { return RailConfig{NeftBatch: 30 * time.Second} }

// IsAsync reports whether the rail settles after the request returns.
func IsAsync(r domain.Rail) bool { return r != domain.RailInternal }

// Validate checks the rail exists and the amount is within its limits.
func (c RailConfig) Validate(r domain.Rail, amountMinor int64) error {
	switch r {
	case domain.RailInternal, domain.RailNEFT:
		return nil
	case domain.RailUPI:
		if amountMinor > upiMaxMinor {
			return fmt.Errorf("%w: UPI is capped at INR 1,00,000 per payment", domain.ErrRailLimit)
		}
	case domain.RailIMPS:
		if amountMinor > impsMaxMinor {
			return fmt.Errorf("%w: IMPS is capped at INR 5,00,000 per payment", domain.ErrRailLimit)
		}
	case domain.RailRTGS:
		if amountMinor < rtgsMinMinor {
			return fmt.Errorf("%w: RTGS requires at least INR 2,00,000 per payment", domain.ErrRailLimit)
		}
	default:
		return domain.ErrUnknownRail
	}
	return nil
}

// SettleAt is when the rail clears a payment accepted at `now`.
func (c RailConfig) SettleAt(r domain.Rail, now time.Time) time.Time {
	switch r {
	case domain.RailUPI:
		return now.Add(upiDelay)
	case domain.RailIMPS:
		return now.Add(impsDelay)
	case domain.RailRTGS:
		return now.Add(rtgsDelay)
	case domain.RailNEFT:
		b := c.NeftBatch
		if b <= 0 {
			b = 30 * time.Second
		}
		return now.Truncate(b).Add(b) // the next batch boundary, strictly after now
	default:
		return now
	}
}
