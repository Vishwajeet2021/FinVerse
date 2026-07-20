package service

import (
	"context"
	"time"

	"github.com/finverse/transaction-service/internal/domain"
)

// TransactionRepository is implemented by internal/repository/postgres.
// CreateWithOutbox and MarkReversedWithOutbox each wrap their DB write and
// the corresponding outbox row insert in one transaction — this is what
// makes the transactional-outbox pattern (docs/05-system-architecture.md §7)
// actually atomic: the event either commits with the ledger write or not at
// all, never one without the other.
type TransactionRepository interface {
	CreateWithOutbox(ctx context.Context, txn *domain.Transaction, eventType string, eventPayload any) error
	GetByIdempotencyKey(ctx context.Context, sourceService, idempotencyKey string) (*domain.Transaction, error)
	GetByID(ctx context.Context, id string) (*domain.Transaction, error)
	IsReversed(ctx context.Context, transactionID string) (bool, error)
	// MarkReversed flips the original transaction's status flag once its
	// reversal has posted. Deliberately separate from CreateWithOutbox: only
	// the reversal's own posting needs an outbox event (TransactionReversed
	// already carries everything a consumer needs) — this is a secondary,
	// non-eventful bookkeeping update. A crash between the reversal posting
	// and this call would leave the original's status flag stale
	// (informational only; the reversal itself is already correctly posted
	// either way) — a known, documented gap acceptable for this phase, not a
	// ledger-correctness issue.
	MarkReversed(ctx context.Context, transactionID string) error
	GetHistory(ctx context.Context, participantID string, from, to *time.Time, limit int, cursor string) ([]domain.Leg, string, error)
	DistinctParticipants(ctx context.Context) ([]ParticipantRef, error)
	BalanceAsOf(ctx context.Context, participantID string, currency string, asOf time.Time) (int64, error)
	InsertClosingSnapshot(ctx context.Context, participantID string, balanceMinor int64, currency string, asOf time.Time) error
	// InsertOutboxEvent queues a standalone event with no owning
	// transaction/legs row — used by daily closing, which has no single
	// Transaction to attach the event to.
	InsertOutboxEvent(ctx context.Context, eventType string, payload any) error
}

// ParticipantRef identifies one ledger participant for daily closing (FR-TXN-5).
type ParticipantRef struct {
	ParticipantID string
	Currency      string
}
