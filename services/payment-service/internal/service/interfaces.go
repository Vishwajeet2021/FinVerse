// Package service implements Payment Service's business/use-case logic. It
// depends only on domain types and the interfaces below -- never on GORM,
// Gin, or the generated gRPC client directly -- per docs/12-coding-standards.md §1.1.
package service

import (
	"context"

	"github.com/finverse/payment-service/internal/domain"
)

const (
	DirectionDebit  = "D"
	DirectionCredit = "C"

	ParticipantAccount = "ACCOUNT"

	currencyINR = "INR"

	// SourceServiceName is what this service identifies itself as on every
	// PostTransaction call.
	SourceServiceName = "payment-service"
)

// PaymentRepository is implemented by internal/repository/postgres.
type PaymentRepository interface {
	// GetByIdempotencyKey backs FR-PAY-style idempotent replay: a duplicate
	// request for the same (from_account_id, idempotency_key) returns the
	// original payment unchanged, per the same pattern Transaction Service
	// uses for its own idempotency key (internal/service/ledger_service.go
	// in transaction-service).
	GetByIdempotencyKey(ctx context.Context, fromAccountID, idempotencyKey string) (*domain.Payment, error)
	GetByID(ctx context.Context, paymentID string) (*domain.Payment, error)
	Create(ctx context.Context, payment *domain.Payment) error
	// MarkCompletedWithOutbox atomically sets status=COMPLETED,
	// transaction_id, and queues the PaymentCompleted event in one DB
	// transaction (docs/05-system-architecture.md §7).
	MarkCompletedWithOutbox(ctx context.Context, paymentID, transactionID, eventType string, eventPayload any) error
	// MarkFailedWithOutbox atomically sets status=FAILED, failure_reason,
	// and queues PaymentFailed.
	MarkFailedWithOutbox(ctx context.Context, paymentID, reason, eventType string, eventPayload any) error
	// InsertOutboxEvent queues a standalone event with no accompanying status
	// change — used for PaymentInitiated, which fires right after the
	// payment row is created and before the ledger call, so it has no
	// terminal status to piggyback on the way Completed/Failed do.
	InsertOutboxEvent(ctx context.Context, eventType string, payload any) error
}

// LedgerClient is the gRPC boundary to Transaction Service
// (docs/05-system-architecture.md §4: Payment<->Transaction is a designated
// synchronous pair). Implemented by internal/client/ledger against the real
// service; faked in tests.
type LedgerClient interface {
	PostTransaction(ctx context.Context, idempotencyKey string, legs []LedgerLeg) (transactionID string, err error)
}

// LedgerLeg is one leg of a PostTransaction call, in this service's own
// vocabulary -- internal/client/ledger translates it to the ledger.proto
// wire shape so this package never imports generated proto types.
type LedgerLeg struct {
	ParticipantID   string
	ParticipantType string
	AmountMinor     int64
	Direction       string
	Currency        string
}
