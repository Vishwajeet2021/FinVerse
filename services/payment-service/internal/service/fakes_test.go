package service

import (
	"context"

	"github.com/finverse/payment-service/internal/domain"
	"github.com/google/uuid"
)

// In-memory fakes for PaymentRepository/LedgerClient, mirroring the other
// Phase 2 services' fakes_test.go pattern -- per docs/17-testing-strategy.md §2.

type fakePaymentRepository struct {
	byID            map[string]*domain.Payment
	byIdempotency    map[string]*domain.Payment // key: fromAccountID + "|" + idempotencyKey
	outboxEvents     []outboxEntry
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

func idemKey(fromAccountID, idempotencyKey string) string { return fromAccountID + "|" + idempotencyKey }

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
