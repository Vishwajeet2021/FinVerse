// Package service implements Payment Service's business/use-case logic. It
// depends only on domain types and the interfaces below -- never on GORM,
// Gin, or the generated gRPC client directly -- per docs/12-coding-standards.md §1.1.
package service

import (
	"context"
	"time"

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

	// MarkProcessing moves an accepted async-rail payment to PROCESSING and
	// records when the rail will clear it.
	MarkProcessing(ctx context.Context, paymentID string, settleAt time.Time) error
	// ClaimDueForSettlement atomically leases up to limit PROCESSING payments
	// whose settle_at has passed (FOR UPDATE SKIP LOCKED, so replicas never
	// settle the same one) and increments each one's settle_attempts.
	ClaimDueForSettlement(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*domain.Payment, error)
	// ReleaseForRetry makes a leased payment eligible again at retryAt.
	ReleaseForRetry(ctx context.Context, paymentID string, retryAt time.Time) error
}

// ScheduleRepository persists scheduled payments and standing instructions
// (FR-PAY-2, FR-PAY-3). Every Claim* method leases rows with SKIP LOCKED so
// several replicas can poll safely; execution is additionally idempotent
// (the payment idempotency key is derived from the schedule), so even a lost
// lease can never double-pay.
type ScheduleRepository interface {
	CreateScheduled(ctx context.Context, sp *domain.ScheduledPayment) error
	ListScheduled(ctx context.Context, createdBy string, limit int) ([]domain.ScheduledPayment, error)
	CancelScheduled(ctx context.Context, createdBy, id string) error
	ClaimDueScheduled(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]domain.ScheduledPayment, error)
	CompleteScheduled(ctx context.Context, id, paymentID string) error
	ReleaseScheduled(ctx context.Context, id string, retryAt time.Time) error

	CreateStanding(ctx context.Context, si *domain.StandingInstruction) error
	ListStanding(ctx context.Context, createdBy string, limit int) ([]domain.StandingInstruction, error)
	GetStanding(ctx context.Context, createdBy, id string) (*domain.StandingInstruction, error)
	// SetStandingStatus moves an instruction between ACTIVE/PAUSED/CANCELLED
	// (and, on resume, to nextRunAt). A CANCELLED instruction is terminal.
	SetStandingStatus(ctx context.Context, createdBy, id, status string, nextRunAt *time.Time) error
	ClaimDueStanding(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]domain.StandingInstruction, error)
	AdvanceStanding(ctx context.Context, id string, nextRunAt time.Time, lastPaymentID string) error
	ReleaseStanding(ctx context.Context, id string, retryAt time.Time) error
}

// LedgerClient is the gRPC boundary to Transaction Service
// (docs/05-system-architecture.md §4: Payment<->Transaction is a designated
// synchronous pair). Implemented by internal/client/ledger against the real
// service; faked in tests.
type LedgerClient interface {
	PostTransaction(ctx context.Context, idempotencyKey string, legs []LedgerLeg) (transactionID string, err error)
}

// AccountResolver is the HTTP boundary to Account Service's internal
// account-number lookup (internal/client/account). This isn't one of the
// designated gRPC pairs in docs/05-system-architecture.md §4 — it follows
// the same plain-HTTP, internal-network-trust posture Account Service
// itself uses to call Customer Service for KYC tier, since it's a one-off
// lookup, not the money-movement critical path.
type AccountResolver interface {
	ResolveAccountID(ctx context.Context, accountNumber string) (string, error)
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

// FraudVerdict is Fraud Service's answer to CheckTransaction, in this
// service's vocabulary (internal/client/fraud translates from the proto).
type FraudVerdict struct {
	Decision  FraudDecision
	RiskScore int
	Rule      string
	Reason    string // customer-safe
}

type FraudDecision string

const (
	FraudAllow  FraudDecision = "ALLOW"
	FraudReview FraudDecision = "REVIEW" // proceeds; Fraud Service opens a case
	FraudDeny   FraudDecision = "DENY"
)

// FraudChecker is the synchronous gRPC boundary to Fraud Service
// (docs/05-system-architecture.md section 4 and 6.1). An error means the
// verdict is unknown; InitiatePayment applies its fail-open/closed policy.
type FraudChecker interface {
	CheckTransaction(ctx context.Context, paymentID, fromAccountID, toReference string, amountMinor int64, rail, idempotencyKey string) (FraudVerdict, error)
}
