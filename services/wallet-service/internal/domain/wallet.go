// Package domain holds Wallet Service's entities and domain errors -- zero
// framework imports, per docs/12-coding-standards.md §1.1. A Wallet is a
// lightweight ledger participant, always backed by exactly one funding
// Account (docs/06-domain-driven-design.md §2.4); the ledger of truth for
// its balance lives in Transaction Service, never here -- this package's
// BalanceProjection is a read-side cache of that truth, not a second
// source of it.
package domain

import (
	"errors"
	"time"
)

type WalletStatus string

const (
	WalletStatusActive WalletStatus = "ACTIVE"
	WalletStatusClosed WalletStatus = "CLOSED"
)

// Wallet is the aggregate root owned by this service. FundingAccountID is a
// cross-service reference to Account Service's own database, not a foreign
// key -- the two services never share a schema.
type Wallet struct {
	ID               string
	CustomerID       string
	FundingAccountID string
	Status           WalletStatus
	CreatedAt        time.Time
}

// IsActive reports whether this wallet may currently be debited. A closed
// wallet can still be read (balance history, statements) but never moves
// money.
func (w *Wallet) IsActive() bool {
	return w.Status == WalletStatusActive
}

// BalanceProjection is the CQRS read side described in
// docs/05-system-architecture.md §5: maintained solely by consuming
// Transaction Service's TransactionPosted events, never written to
// directly by a customer-facing request.
type BalanceProjection struct {
	WalletID          string
	BalanceMinor      int64
	LastTransactionID *string
	UpdatedAt         time.Time
}

// HasSufficientBalance reports whether a debit of amountMinor would leave
// this projection non-negative. This is Wallet Service's own best-effort
// pre-check (FR-WAL-4) -- see the longer comment on
// internal/service.WalletService.Transfer for why it cannot be a strict
// concurrency guarantee in this phase.
func (b *BalanceProjection) HasSufficientBalance(amountMinor int64) bool {
	return b.BalanceMinor >= amountMinor
}

var (
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrWalletAlreadyExists = errors.New("a wallet already exists for this customer")
	ErrWalletClosed        = errors.New("wallet is closed and cannot be debited or credited")
	ErrInsufficientBalance = errors.New("this debit would take the wallet balance negative")
	ErrSameWallet          = errors.New("cannot transfer a wallet to itself")
	ErrInvalidAmount       = errors.New("amount must be a positive number of minor units")
	// ErrFundingAccountFrozen / ErrFundingAccountClosed: a top-up debits the funding account, so
	// a freeze (FR-ACC-6) must stop it just as it stops a payment.
	ErrFundingAccountFrozen = errors.New("the funding account is frozen")
	ErrFundingAccountClosed = errors.New("the funding account is closed")
)

// MaxAmountMinor is the largest single amount (INR 1 crore) accepted by any money-moving path.
// It bounds arithmetic and sits far above retail limits, which Fraud enforces per rail.
const MaxAmountMinor int64 = 1_000_000_000
