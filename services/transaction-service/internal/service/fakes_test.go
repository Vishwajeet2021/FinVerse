package service

import (
	"context"
	"time"

	"github.com/finverse/transaction-service/internal/domain"
	"github.com/google/uuid"
)

// fakeTransactionRepository is an in-memory stand-in used by the unit tests
// in this package, per docs/17-testing-strategy.md §2 — the database-level
// invariant itself is verified separately, against a real Postgres, in
// internal/repository/postgres's integration test.
type fakeTransactionRepository struct {
	byID          map[string]*domain.Transaction
	byIdempotency map[string]*domain.Transaction // key: sourceService + "|" + idempotencyKey
	reversedOf    map[string]bool                // transactionID -> has a reversal pointing at it
	outboxEvents  []outboxEntry
}

type outboxEntry struct {
	EventType string
	Payload   any
}

func newFakeTransactionRepository() *fakeTransactionRepository {
	return &fakeTransactionRepository{
		byID:          map[string]*domain.Transaction{},
		byIdempotency: map[string]*domain.Transaction{},
		reversedOf:    map[string]bool{},
	}
}

func idemKey(sourceService, idempotencyKey string) string { return sourceService + "|" + idempotencyKey }

func (f *fakeTransactionRepository) CreateWithOutbox(_ context.Context, txn *domain.Transaction, eventType string, eventPayload any) error {
	cp := *txn
	f.byID[txn.ID] = &cp
	f.byIdempotency[idemKey(txn.SourceService, txn.IdempotencyKey)] = &cp
	if txn.ReversesTransactionID != nil {
		f.reversedOf[*txn.ReversesTransactionID] = true
	}
	f.outboxEvents = append(f.outboxEvents, outboxEntry{EventType: eventType, Payload: eventPayload})
	return nil
}

func (f *fakeTransactionRepository) InsertOutboxEvent(_ context.Context, eventType string, payload any) error {
	f.outboxEvents = append(f.outboxEvents, outboxEntry{EventType: eventType, Payload: payload})
	return nil
}

func (f *fakeTransactionRepository) GetByIdempotencyKey(_ context.Context, sourceService, idempotencyKey string) (*domain.Transaction, error) {
	if t, ok := f.byIdempotency[idemKey(sourceService, idempotencyKey)]; ok {
		return t, nil
	}
	return nil, domain.ErrTransactionNotFound
}

func (f *fakeTransactionRepository) GetByID(_ context.Context, id string) (*domain.Transaction, error) {
	if t, ok := f.byID[id]; ok {
		return t, nil
	}
	return nil, domain.ErrTransactionNotFound
}

func (f *fakeTransactionRepository) IsReversed(_ context.Context, transactionID string) (bool, error) {
	return f.reversedOf[transactionID], nil
}

func (f *fakeTransactionRepository) MarkReversed(_ context.Context, transactionID string) error {
	if t, ok := f.byID[transactionID]; ok {
		t.Status = domain.TransactionStatusReversed
	}
	return nil
}

func (f *fakeTransactionRepository) GetHistory(_ context.Context, participantID string, _, _ *time.Time, limit int, _ string) ([]domain.Leg, string, error) {
	var out []domain.Leg
	for _, t := range f.byID {
		for _, l := range t.Legs {
			if l.ParticipantID == participantID {
				out = append(out, l)
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, "", nil
}

func (f *fakeTransactionRepository) DistinctParticipants(_ context.Context) ([]ParticipantRef, error) {
	seen := map[string]ParticipantRef{}
	for _, t := range f.byID {
		for _, l := range t.Legs {
			seen[l.ParticipantID+"|"+l.Currency] = ParticipantRef{ParticipantID: l.ParticipantID, Currency: l.Currency}
		}
	}
	out := make([]ParticipantRef, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeTransactionRepository) BalanceAsOf(_ context.Context, participantID, currency string, _ time.Time) (int64, error) {
	var balance int64
	for _, t := range f.byID {
		for _, l := range t.Legs {
			if l.ParticipantID == participantID && l.Currency == currency {
				if l.Direction == domain.DirectionDebit {
					balance += l.AmountMinor
				} else {
					balance -= l.AmountMinor
				}
			}
		}
	}
	return balance, nil
}

func (f *fakeTransactionRepository) InsertClosingSnapshot(_ context.Context, _ string, _ int64, _ string, _ time.Time) error {
	return nil
}

func newLeg(participantID string, amount int64, dir domain.Direction, currency string) domain.Leg {
	return domain.Leg{ID: uuid.NewString(), ParticipantID: participantID, ParticipantType: domain.ParticipantAccount, AmountMinor: amount, Direction: dir, Currency: currency}
}
