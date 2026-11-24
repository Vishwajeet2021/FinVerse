package service

import (
	"context"

	"github.com/finverse/payment-service/internal/domain"
	"github.com/google/uuid"
)

// In-memory fakes for PaymentRepository/LedgerClient, mirroring the other
// Phase 2 services' fakes_test.go pattern -- per docs/17-testing-strategy.md §2.

type fakePaymentRepository struct {
	byID          map[string]*domain.Payment
	byIdempotency map[string]*domain.Payment // key: fromAccountID + "|" + idempotencyKey
	outboxEvents  []outboxEntry
}

type outboxEntry struct {
	EventType string
	Payload   any
}

func newFakePaymentRepository() *fakePaymentRepository {
	return &fakePaymentRepository{
		byID:          map[string]*domain.Payment{},
		byIdempotency: map[string]*domain.Payment{},
	}
}

func idemKey(fromAccountID, idempotencyKey string) string {
	return fromAccountID + "|" + idempotencyKey
}

func (f *fakePaymentRepository) GetByIdempotencyKey(_ context.Context, fromAccountID, idempotencyKey string) (*domain.Payment, error) {
	if p, ok := f.byIdempotency[idemKey(fromAccountID, idempotencyKey)]; ok {
		return p, nil
	}
	return nil, domain.ErrPaymentNotFound
}

func (f *fakePaymentRepository) GetByID(_ context.Context, paymentID string) (*domain.Payment, error) {
	if p, ok := f.byID[paymentID]; ok {
		return p, nil
	}
	return nil, domain.ErrPaymentNotFound
}

func (f *fakePaymentRepository) Create(_ context.Context, payment *domain.Payment) error {
	cp := *payment
	f.byID[payment.ID] = &cp
	f.byIdempotency[idemKey(payment.FromAccountID, payment.IdempotencyKey)] = &cp
	return nil
}

func (f *fakePaymentRepository) InsertOutboxEvent(_ context.Context, eventType string, payload any) error {
	f.outboxEvents = append(f.outboxEvents, outboxEntry{EventType: eventType, Payload: payload})
	return nil
}

func (f *fakePaymentRepository) MarkCompletedWithOutbox(_ context.Context, paymentID, transactionID, eventType string, payload any) error {
	p, ok := f.byID[paymentID]
	if !ok {
		return domain.ErrPaymentNotFound
	}
	p.Status = domain.StatusCompleted
	p.TransactionID = &transactionID
	f.outboxEvents = append(f.outboxEvents, outboxEntry{EventType: eventType, Payload: payload})
	return nil
}

func (f *fakePaymentRepository) MarkFailedWithOutbox(_ context.Context, paymentID, reason, eventType string, payload any) error {
	p, ok := f.byID[paymentID]
	if !ok {
		return domain.ErrPaymentNotFound
	}
	p.Status = domain.StatusFailed
	p.FailureReason = &reason
	f.outboxEvents = append(f.outboxEvents, outboxEntry{EventType: eventType, Payload: payload})
	return nil
}

// fakeLedgerClient records every PostTransaction call so tests can assert on
// the exact legs sent, and lets tests inject a failure.
type fakeLedgerClient struct {
	calls []postTransactionCall
	err   error
}

type postTransactionCall struct {
	IdempotencyKey string
	Legs           []LedgerLeg
}

func (f *fakeLedgerClient) PostTransaction(_ context.Context, idempotencyKey string, legs []LedgerLeg) (string, error) {
	f.calls = append(f.calls, postTransactionCall{IdempotencyKey: idempotencyKey, Legs: legs})
	if f.err != nil {
		return "", f.err
	}
	return uuid.NewString(), nil
}

// fakeAccountResolver defaults to resolving any account number to itself
// (identity), so existing tests that treat "acct-2" as if it were already
// an account ID keep working unchanged -- tests that care about the
// resolution step itself register an explicit override or a "missing"
// account number.
type fakeAccountResolver struct {
	overrides map[string]string
	missing   map[string]bool
	err       error
	// debit maps a source account id to the error CheckCanDebit returns for it (nil = may debit).
	debit map[string]error
}

func newFakeAccountResolver() *fakeAccountResolver {
	return &fakeAccountResolver{overrides: map[string]string{}, missing: map[string]bool{}, debit: map[string]error{}}
}

func (f *fakeAccountResolver) CheckCanDebit(_ context.Context, accountID string) error {
	return f.debit[accountID]
}

func (f *fakeAccountResolver) ResolveAccountID(_ context.Context, accountNumber string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.missing[accountNumber] {
		return "", domain.ErrRecipientAccountNotFound
	}
	if id, ok := f.overrides[accountNumber]; ok {
		return id, nil
	}
	return accountNumber, nil
}

// fakeFraudChecker allows everything unless told otherwise.
type fakeFraudChecker struct {
	verdict FraudVerdict
	err     error
	calls   int
}

func (f *fakeFraudChecker) CheckTransaction(_ context.Context, _, _, _ string, _ int64, _, _ string) (FraudVerdict, error) {
	f.calls++
	if f.err != nil {
		return FraudVerdict{}, f.err
	}
	if f.verdict.Decision == "" {
		return FraudVerdict{Decision: FraudAllow}, nil
	}
	return f.verdict, nil
}
