// Package service implements Account Service's business/use-case logic. It
// depends only on domain types and the interfaces below — never on GORM,
// Gin, or gRPC directly — per docs/12-coding-standards.md §1.1.
package service

import (
	"context"
	"time"

	"github.com/finverse/account-service/internal/domain"
)

// AccountRepository is implemented by internal/repository/postgres.
type AccountRepository interface {
	// OpenAccountWithOutbox creates the account and its OWNER holder row and
	// a zero-balance projection row, and inserts the AccountOpened outbox
	// event, all in one DB transaction — the same atomic
	// write-plus-outbox-row pattern as Transaction Service's
	// CreateWithOutbox (docs/05-system-architecture.md §7). Returns
	// domain.ErrAccountNumberCollision if the generated account number
	// collided with an existing one, so the caller can retry with a new one.
	OpenAccountWithOutbox(ctx context.Context, account *domain.Account, holder *domain.AccountHolder, eventType string, eventPayload any) error

	GetByID(ctx context.Context, accountID string) (*domain.Account, error)

	// GetByAccountNumber backs the internal account-number -> account-ID
	// resolution endpoint (transport/http/handlers.go's ResolveAccountNumber)
	// — the only identifier a payer actually knows for someone else's
	// account is its public account number, never the internal UUID
	// (docs/07-microservice-specifications.md §7).
	GetByAccountNumber(ctx context.Context, accountNumber string) (*domain.Account, error)

	// ListByCustomerID backs FR-ACC-1's read side: a customer can hold more
	// than one account (docs/06-domain-driven-design.md §2.3), and there was
	// no way to discover them without this — a gap surfaced when wiring the
	// frontend dashboard, not part of the original interface sketch.
	ListByCustomerID(ctx context.Context, customerID string) ([]domain.Account, error)

	// GetHolder returns domain.ErrNotAccountHolder if customerID is not a
	// holder of accountID — the row-level ownership check required by
	// docs/10-authentication-authorization.md §3.3.
	GetHolder(ctx context.Context, accountID, customerID string) (*domain.AccountHolder, error)

	GetBalance(ctx context.Context, accountID string) (*domain.BalanceProjection, error)

	// UpdateStatusWithOutbox atomically flips the account's status and
	// inserts the corresponding AccountFrozen/AccountUnfrozen outbox event.
	UpdateStatusWithOutbox(ctx context.Context, accountID string, status domain.AccountStatus, eventType string, eventPayload any) error

	// ApplyTransactionPosted implements the idempotent balance-projection
	// update described in docs/05-system-architecture.md §5 and §7: within
	// one DB transaction, it first inserts eventID into processed_events
	// (ON CONFLICT DO NOTHING); if that insert affects zero rows, the event
	// has already been fully applied and every leg update is skipped — this
	// is what makes redelivery from the at-least-once Redis Streams
	// consumer group (shared/go/eventbus) safe. Legs whose AccountID isn't
	// one of this service's own accounts are silently no-ops (the UPDATE
	// simply matches zero rows), which is how a multi-participant
	// TransactionPosted event (e.g. one leg on a WALLET participant) is
	// naturally filtered without a separate existence check.
	ApplyTransactionPosted(ctx context.Context, eventID, transactionID string, legs []LegUpdate) error

	AccountNumberExists(ctx context.Context, accountNumber string) (bool, error)
}

// LegUpdate is one ACCOUNT-type leg's effect on a balance projection, in the
// direction already resolved to a signed delta (see
// internal/service/events.go's toLegUpdates) so the repository layer never
// needs to know about ledger debit/credit semantics.
type LegUpdate struct {
	AccountID  string
	DeltaMinor int64
}

// LedgerHistoryClient is implemented by internal/client/ledger. Account
// Service calls this only for statement generation (FR-ACC-7) — it never
// calls Transaction Service to post money movement itself
// (docs/07-microservice-specifications.md §4: Account's sync dependency on
// Transaction is for statements only).
type LedgerHistoryClient interface {
	GetAccountHistory(ctx context.Context, participantID string, from, to *time.Time, limit int, cursor string) ([]StatementEntry, string, error)
}

// StatementEntry is one ledger leg as reported by Transaction Service,
// reshaped for this service's own statement response — deliberately not the
// generated ledgerv1 protobuf type, so internal/service stays independent of
// the gRPC client per docs/12-coding-standards.md §1.1.
type StatementEntry struct {
	TransactionID string
	LegID         string
	AmountMinor   int64
	Direction     string
	Currency      string
	CreatedAt     time.Time
	SourceService string
}
