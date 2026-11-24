package service

import (
	"context"

	"github.com/finverse/wallet-service/internal/domain"
)

// Wire-level constants shared between outbound ledger legs (LedgerLeg,
// built by this service when calling Transaction Service) and inbound
// TransactionPosted legs (LegPayload, consumed from Transaction Service) --
// both sides of the same gRPC/event contract use these exact values
// (shared/proto/ledger/v1/ledger.proto, mirrored by
// transaction-service/internal/domain.Direction/ParticipantType).
const (
	DirectionDebit  = "D"
	DirectionCredit = "C"

	ParticipantAccount = "ACCOUNT"
	ParticipantWallet  = "WALLET"

	currencyINR = "INR"

	// SourceServiceName is what this service identifies itself as on every
	// PostTransaction call — used by internal/client/ledger, exported so that
	// package doesn't need its own duplicate string literal.
	SourceServiceName = "wallet-service"
)

// AccountDebitChecker asks Account Service whether an account may be debited. It returns
// domain.ErrFundingAccountFrozen or ErrFundingAccountClosed when it may not, and any other error
// when the state could not be determined (the caller then refuses, rather than guessing).
type AccountDebitChecker interface {
	CheckCanDebit(ctx context.Context, accountID string) error
}

// WalletRepository is implemented by internal/repository/postgres.
// CreateWalletWithProjection and ApplyTransactionPostedIdempotent each wrap
// their DB write(s) and the relevant outbox/dedupe row in one transaction --
// the same transactional-outbox and at-least-once-safe-consumer shapes
// documented in docs/05-system-architecture.md §5 and §7.
type WalletRepository interface {
	// CreateWalletWithProjection implements FR-WAL-1's write: a new wallet
	// row plus its zero-balance projection row plus (optionally) an outbox
	// event, atomically. Returns domain.ErrWalletAlreadyExists if the
	// customer_id unique constraint is violated -- this is the actual,
	// non-bypassable guarantee against a concurrent double-provision race
	// that a service-layer "check then insert" alone cannot close.
	CreateWalletWithProjection(ctx context.Context, wallet *domain.Wallet, eventType string, eventPayload any) error
	GetByID(ctx context.Context, id string) (*domain.Wallet, error)
	GetByCustomerID(ctx context.Context, customerID string) (*domain.Wallet, error)
	ExistsByID(ctx context.Context, id string) (bool, error)
	GetBalance(ctx context.Context, walletID string) (*domain.BalanceProjection, error)
	// ApplyTransactionPostedIdempotent implements the redelivery-safe
	// projection update described in docs/05-system-architecture.md §5: one
	// TransactionPosted event can carry more than one leg this service
	// cares about (e.g. both sides of a wallet-to-wallet transfer), so the
	// dedupe key is the event as a whole, not one row per leg -- applied
	// reports false when the event was already processed, letting the
	// caller ack without double-applying any of its deltas.
	ApplyTransactionPostedIdempotent(ctx context.Context, eventID, transactionID string, deltas []WalletDelta) (applied bool, err error)
	// InsertOutboxEvent queues a standalone event with no owning
	// wallet-creation row to attach it to (used by TopUp/Transfer, which
	// post to the ledger via gRPC first and only then queue the
	// WalletToppedUp/WalletTransferred event).
	InsertOutboxEvent(ctx context.Context, eventType string, payload any) error
}

// WalletDelta is one wallet's balance adjustment extracted from a
// TransactionPosted event's legs.
type WalletDelta struct {
	WalletID   string
	DeltaMinor int64
}

// LedgerClient is the gRPC boundary to Transaction Service
// (docs/05-system-architecture.md §4: Wallet<->Transaction is a designated
// synchronous pair). Implemented by internal/client/ledger against the real
// service; faked in tests so business logic is verified without a network
// call.
type LedgerClient interface {
	PostTransaction(ctx context.Context, idempotencyKey string, legs []LedgerLeg) (transactionID string, err error)
}

// LedgerLeg is one leg of a PostTransaction call, in this service's own
// vocabulary -- internal/client/ledger translates it to the ledger.proto
// wire shape so this package never imports generated proto types
// (docs/12-coding-standards.md §1.1).
type LedgerLeg struct {
	ParticipantID   string
	ParticipantType string
	AmountMinor     int64
	Direction       string
	Currency        string
}
