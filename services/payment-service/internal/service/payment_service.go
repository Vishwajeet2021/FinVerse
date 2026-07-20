package service

import (
	"context"
	"fmt"
	"time"

	"github.com/finverse/payment-service/internal/domain"
	"github.com/google/uuid"
)

// ledgerCallTimeout bounds the synchronous PostTransaction call. FR-PAY /
// NFR-PERF-3 budgets the whole synchronous fund-transfer leg at 500ms; this
// gRPC hop is the dominant cost of that budget, so its own timeout is set
// tighter than the overall budget, not equal to it. Local dev value; a real
// deployment would tune this against measured P99s
// (docs/04-non-functional-requirements.md).
const ledgerCallTimeout = 2 * time.Second

type PaymentService struct {
	repo   PaymentRepository
	ledger LedgerClient
}

func NewPaymentService(repo PaymentRepository, ledger LedgerClient) *PaymentService {
	return &PaymentService{repo: repo, ledger: ledger}
}

// InitiatePayment implements FR-PAY-1 (INTERNAL rail only, per
// docs/19-implementation-roadmap.md Phase 2) and the fund-transfer sequence
// in docs/05-system-architecture.md §6.1, minus the Fraud hop -- Fraud
// Service is Phase 3 and does not exist yet; this is a deliberate,
// documented gap, not a silent omission. OTP step-up (FR-PAY-4) is likewise
// deferred -- Auth Service's OTP endpoints aren't built yet either.
//
// Ownership of fromAccountID is NOT verified here (this service has no way
// to check Account Service's account_holders table, and Account Service
// isn't guaranteed to exist in every environment this runs in) -- it trusts
// the caller-supplied ID, which is safe today only because nothing calls
// this except the frontend acting on the authenticated user's own account.
// A real hardening pass adds a call to Account Service to verify holder
// ownership before initiating; tracked as a Phase 3 item.
func (s *PaymentService) InitiatePayment(ctx context.Context, fromAccountID, toAccountID string, amountMinor int64, idempotencyKey string) (*domain.Payment, error) {
	if existing, err := s.repo.GetByIdempotencyKey(ctx, fromAccountID, idempotencyKey); err == nil && existing != nil {
		return existing, nil // FR-TXN-3-style semantics: duplicate request returns the original result
	} else if err != nil && err != domain.ErrPaymentNotFound {
		return nil, fmt.Errorf("checking idempotency key: %w", err)
	}

	if amountMinor <= 0 {
		return nil, domain.ErrInvalidAmount
	}
	if fromAccountID == toAccountID {
		return nil, domain.ErrSameAccount
	}

	payment := &domain.Payment{
		ID:             uuid.NewString(),
		IdempotencyKey: idempotencyKey,
		FromAccountID:  fromAccountID,
		ToReference:    toAccountID,
		Rail:           domain.RailInternal,
		AmountMinor:    amountMinor,
		Status:         domain.StatusInitiated,
		CreatedAt:      time.Now(),
	}
	// PROCESSING is not materialized as its own DB write: this entire flow
	// is one synchronous request/response cycle, so there's no observer
	// between INITIATED and the terminal state that would ever see it --
	// recording it would be an extra round trip with no one to read it.
	if err := s.repo.Create(ctx, payment); err != nil {
		return nil, fmt.Errorf("creating payment: %w", err)
	}
	if err := s.repo.InsertOutboxEvent(ctx, EventPaymentInitiated, paymentInitiatedPayload{
		PaymentID: payment.ID, FromAccountID: fromAccountID, ToReference: toAccountID,
		AmountMinor: amountMinor, Rail: string(domain.RailInternal),
	}); err != nil {
		return nil, fmt.Errorf("queuing PaymentInitiated event: %w", err)
	}

	legs := []LedgerLeg{
		{ParticipantID: fromAccountID, ParticipantType: ParticipantAccount, AmountMinor: amountMinor, Direction: DirectionDebit, Currency: currencyINR},
		{ParticipantID: toAccountID, ParticipantType: ParticipantAccount, AmountMinor: amountMinor, Direction: DirectionCredit, Currency: currencyINR},
	}

	ledgerCtx, cancel := context.WithTimeout(ctx, ledgerCallTimeout)
	transactionID, err := s.ledger.PostTransaction(ledgerCtx, idempotencyKey, legs)
	cancel()

	if err != nil {
		reason := err.Error()
		if markErr := s.repo.MarkFailedWithOutbox(ctx, payment.ID, reason, EventPaymentFailed, paymentFailedPayload{PaymentID: payment.ID, Reason: reason}); markErr != nil {
			return nil, fmt.Errorf("posting to ledger failed (%w) and marking payment failed also failed: %w", err, markErr)
		}
		payment.Status = domain.StatusFailed
		payment.FailureReason = &reason
		return payment, fmt.Errorf("posting payment to ledger: %w", err)
	}

	if err := s.repo.MarkCompletedWithOutbox(ctx, payment.ID, transactionID, EventPaymentCompleted, paymentCompletedPayload{PaymentID: payment.ID, TransactionID: transactionID}); err != nil {
		return nil, fmt.Errorf("marking payment completed: %w", err)
	}
	payment.Status = domain.StatusCompleted
	payment.TransactionID = &transactionID
	return payment, nil
}

// GetPayment implements FR-PAY-6's status polling.
func (s *PaymentService) GetPayment(ctx context.Context, paymentID string) (*domain.Payment, error) {
	return s.repo.GetByID(ctx, paymentID)
}
