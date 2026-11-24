// Package domain holds Account Service's entities and domain errors — no
// framework imports, per docs/12-coding-standards.md §1.1. Account Service
// owns account metadata, holder membership, and the balance *read
// projection*; it never owns the ledger of truth (that's Transaction
// Service — docs/06-domain-driven-design.md §2.3).
package domain

import (
	"errors"
	"time"
)

type ProductType string

const (
	ProductSavings ProductType = "SAVINGS"
	ProductCurrent ProductType = "CURRENT"
	ProductJoint   ProductType = "JOINT"
	ProductVirtual ProductType = "VIRTUAL"
)

// Valid reports whether pt is one of the product types the database's CHECK
// constraint (db/migrations/0001_init.up.sql) allows. Checked here too so
// OpenAccount fails fast with a clear domain error instead of a raw
// constraint-violation error from Postgres.
func (pt ProductType) Valid() bool {
	switch pt {
	case ProductSavings, ProductCurrent, ProductJoint, ProductVirtual:
		return true
	}
	return false
}

type AccountStatus string

const (
	StatusActive AccountStatus = "ACTIVE"
	StatusFrozen AccountStatus = "FROZEN"
	StatusClosed AccountStatus = "CLOSED"
)

type HolderRole string

const (
	RoleOwner       HolderRole = "OWNER"
	RoleJointHolder HolderRole = "JOINT_HOLDER"
)

// Account is a ledger participant with a product type, owned by one or more
// Customers (docs/06-domain-driven-design.md §2.3, §4). CustomerID is a
// reference to Customer Service's own aggregate, not a foreign key — the two
// services never share a database (docs/08-database-design.md §1).
type Account struct {
	ID                  string
	CustomerID          string
	AccountNumber       string
	ProductType         ProductType
	Status              AccountStatus
	OverdraftLimitMinor int64
	CreatedAt           time.Time
	// FrozenBy is who placed the current freeze; empty when not frozen (docs/23 D-A4).
	FrozenBy Actor
}

// Actor is who performed a state change.
type Actor string

const (
	ActorCustomer Actor = "customer"
	ActorAdmin    Actor = "admin"
)

// CanDebit implements FR-ACC-6: a frozen or closed account rejects any
// debit-affecting operation while continuing to accept credits. Nothing in
// this service currently initiates a debit directly (Account Service never
// posts to the ledger itself — docs/07-microservice-specifications.md §4)
// but this is the rule Payment/Wallet's pre-transfer checks are expected to
// apply against the account state Account Service reports.
func (a *Account) CanDebit() error {
	switch a.Status {
	case StatusFrozen:
		return ErrAccountFrozen
	case StatusClosed:
		return ErrAccountClosed
	}
	return nil
}

// AccountHolder links a Customer to an Account with a role — the
// authorization source of truth for the ownership rule in
// docs/10-authentication-authorization.md §3.3.
type AccountHolder struct {
	AccountID  string
	CustomerID string
	Role       HolderRole
}

// BalanceProjection is the CQRS read side described in
// docs/05-system-architecture.md §5: maintained by consuming Transaction
// Service's TransactionPosted events, never written to by any
// customer-facing request path directly.
type BalanceProjection struct {
	AccountID             string
	LedgerBalanceMinor    int64
	AvailableBalanceMinor int64
	LastTransactionID     *string
	UpdatedAt             time.Time
}

var (
	ErrAccountNotFound      = errors.New("account not found")
	ErrAccountFrozen        = errors.New("account is frozen")
	ErrAccountClosed        = errors.New("account is closed")
	ErrAccountAlreadyFrozen = errors.New("account is already frozen")
	ErrAccountNotFrozen     = errors.New("account is not frozen")
	ErrAccountAlreadyClosed = errors.New("account is already closed")
	// ErrFrozenByAdmin: a customer tried to lift a hold an administrator placed.
	ErrFrozenByAdmin = errors.New("account was frozen by an administrator")
	// ErrAccountHasBalance guards FR-ACC-5's closure path: an account can
	// only be closed at a zero ledger balance, the same real-world rule
	// every bank enforces — closing is not a way to make money disappear.
	// The caller must move funds out first (a transfer to another account,
	// or a reversal of whatever put the balance there) before retrying.
	ErrAccountHasBalance   = errors.New("account balance must be zero to close it")
	ErrInvalidProductType  = errors.New("invalid product type")
	ErrNotAccountHolder    = errors.New("caller is not a holder of this account")
	ErrInsufficientKYCTier = errors.New("customer's KYC tier is insufficient for this operation")
	// ErrAccountNumberCollision signals the generated account number hit the
	// UNIQUE constraint on accounts.account_number — the caller
	// (internal/service) is expected to retry with a freshly generated
	// number, per internal/service/account_number.go's comment on why a
	// bounded retry loop, not a fatal error, is the correct response.
	ErrAccountNumberCollision = errors.New("generated account number collided with an existing one")
)
