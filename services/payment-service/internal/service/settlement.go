package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/payment-service/internal/domain"
)

const (
	settleLease        = 30 * time.Second
	settleBatch        = 50
	maxSettleAttempts  = 3 // FR-PAY-8: bounded retry of a transiently failing downstream call
	settleRetryBackoff = 2 * time.Second
)

// SettleDue clears every async-rail payment whose settle time has passed:
// the simulated beneficiary-side rejection, then the real ledger post.
// It returns how many payments reached a terminal state. Safe to call from
// several replicas concurrently (leased claims) and to call repeatedly (a
// payment's ledger idempotency key is its payment key, so a retry after a
// lost response can never post twice).
func (s *PaymentService) SettleDue(ctx context.Context) (int, error) {
	due, err := s.repo.ClaimDueForSettlement(ctx, s.now(), settleLease, settleBatch)
	if err != nil {
		return 0, fmt.Errorf("claiming due payments: %w", err)
	}
	done := 0
	for _, p := range due {
		if s.settleOne(ctx, p) {
			done++
		}
	}
	return done, nil
}

// settleOne returns true when the payment reached COMPLETED or FAILED.
func (s *PaymentService) settleOne(ctx context.Context, p *domain.Payment) bool {
	if s.rails.FailureRate > 0 && s.rng() < s.rails.FailureRate {
		_, _ = s.failPayment(ctx, p, "RAIL_REJECTED: the beneficiary bank rejected this payment", nil)
		return true
	}

	toAccountID, err := s.account.ResolveAccountID(ctx, p.ToReference)
	if errors.Is(err, domain.ErrRecipientAccountNotFound) {
		_, _ = s.failPayment(ctx, p, "RAIL_REJECTED: beneficiary account not found", nil)
		return true
	}
	var txID string
	if err == nil {
		legs := []LedgerLeg{
			{ParticipantID: p.FromAccountID, ParticipantType: ParticipantAccount, AmountMinor: p.AmountMinor, Direction: DirectionDebit, Currency: currencyINR},
			{ParticipantID: toAccountID, ParticipantType: ParticipantAccount, AmountMinor: p.AmountMinor, Direction: DirectionCredit, Currency: currencyINR},
		}
		ledgerCtx, cancel := context.WithTimeout(ctx, ledgerCallTimeout)
		txID, err = s.ledger.PostTransaction(ledgerCtx, p.IdempotencyKey, legs)
		cancel()
	}
	if err != nil {
		if p.SettleAttempts >= maxSettleAttempts {
			_, _ = s.failPayment(ctx, p, "SETTLEMENT_FAILED: could not complete settlement, funds were not moved", err)
			return true
		}
		// Transient: retry with exponential backoff (2s, 4s, ...).
		backoff := settleRetryBackoff << (p.SettleAttempts - 1)
		_ = s.repo.ReleaseForRetry(ctx, p.ID, s.now().Add(backoff))
		return false
	}
	if err := s.repo.MarkCompletedWithOutbox(ctx, p.ID, txID, EventPaymentCompleted, paymentCompletedPayload{PaymentID: p.ID, TransactionID: txID}); err != nil {
		// The ledger post succeeded; the lease expiry will retry this and the
		// idempotent ledger call returns the same transaction.
		return false
	}
	return true
}
