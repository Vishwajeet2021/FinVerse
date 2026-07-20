package service

import "errors"

// ErrTierLimitExceeded is returned by CheckWithinTierLimits when a proposed
// transaction/day amount would exceed the caller's tier limit (FR-ACC-2, BR-03).
var ErrTierLimitExceeded = errors.New("transaction exceeds the daily limit for this account's KYC tier")

// TierLimits holds the per-tier ceilings FR-ACC-2 requires Account Service
// enforce (BR-03: "Tier 1 lower than Tier 2").
type TierLimits struct {
	DailyTransactionLimitMinor int64
	MaxBalanceMinor            int64
}

// tierLimits is a placeholder schedule — the actual figures belong in
// product/business config, not hardcoded Go, but no such config source
// exists yet for this phase. Tier 0 gets a zero limit because FR-ACC-1
// already gates account opening at Tier 1+; a Tier 0 caller should never
// reach this check in practice.
var tierLimits = map[int]TierLimits{
	0: {DailyTransactionLimitMinor: 0, MaxBalanceMinor: 0},
	1: {DailyTransactionLimitMinor: 50_000_00, MaxBalanceMinor: 200_000_00},
	2: {DailyTransactionLimitMinor: 500_000_00, MaxBalanceMinor: 2_000_000_00},
}

// LimitsForTier returns the configured limits for kycTier, or the zero-limit
// (Tier 0) schedule for any tier this service doesn't recognize.
func LimitsForTier(kycTier int) TierLimits {
	if l, ok := tierLimits[kycTier]; ok {
		return l
	}
	return tierLimits[0]
}

// CheckWithinTierLimits implements FR-ACC-2's per-tier daily limit check.
//
// kycTier is caller-supplied here, not derived from a live KYCApproved event
// subscription — Account Service doesn't consume KYCApproved yet
// (docs/07-microservice-specifications.md §4 lists it as a "Consumes"
// dependency, but wiring a second event consumer for it is out of scope for
// this pass; the balance-projection consumer in
// internal/service/account_service.go is the one that matters for
// correctness here). Today's callers are expected to pass the kyc_tier
// claim already embedded in the caller's JWT
// (docs/10-authentication-authorization.md §2.1), which is itself sourced
// from Customer Service at token-issuance time — a reasonable stand-in until
// the event subscription lands as a Phase 3 hardening item.
func CheckWithinTierLimits(kycTier int, dailyTotalSoFarMinor, proposedAmountMinor int64) error {
	limits := LimitsForTier(kycTier)
	if dailyTotalSoFarMinor+proposedAmountMinor > limits.DailyTransactionLimitMinor {
		return ErrTierLimitExceeded
	}
	return nil
}
