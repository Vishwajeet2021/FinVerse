// Package domain holds Transaction Service's entities and domain errors —
// no framework imports, per docs/12-coding-standards.md §1.1. This is the
// core domain of the whole platform (docs/06-domain-driven-design.md §1):
// every other service's "money movement" ultimately resolves to a
// Transaction here.
package domain

import (
	"errors"
	"time"
)

type TransactionStatus string

const (
	TransactionStatusPosted   TransactionStatus = "POSTED"
	TransactionStatusReversed TransactionStatus = "REVERSED"
)

type Direction string

const (
	DirectionDebit  Direction = "D"
	DirectionCredit Direction = "C"
)

type ParticipantType string

const (
	ParticipantAccount  ParticipantType = "ACCOUNT"
	ParticipantWallet   ParticipantType = "WALLET"
	ParticipantSuspense ParticipantType = "SUSPENSE"
)

// Leg is one debit or credit against one ledger participant. A Transaction's
// legs must sum to zero per currency — see Transaction.Validate.
type Leg struct {
	ID              string
	TransactionID   string
	ParticipantID   string
	ParticipantType ParticipantType
	AmountMinor     int64
	Direction       Direction
	Currency        string
	// CreatedAt is the owning transaction's time. Set only on legs read back through account
	// history (statements need it); zero on legs being posted.
	CreatedAt time.Time
}

// Transaction is the aggregate root of the ledger — docs/06-domain-driven-design.md §4.
type Transaction struct {
	ID                    string
	IdempotencyKey        string
	Status                TransactionStatus
	ReversesTransactionID *string
	SourceService         string
	CreatedAt             time.Time
	Legs                  []Leg
}

// Validate enforces the double-entry invariant in application code — a
// pre-check that fails fast with a clear domain error, before the request
// ever reaches the database-level constraint trigger
// (db/migrations/0001_init.up.sql) that's the real, non-bypassable guarantee.
func (t *Transaction) Validate() error {
	if len(t.Legs) < 2 {
		return ErrTooFewLegs
	}

	balances := map[string]int64{} // currency -> debits-credits
	for _, leg := range t.Legs {
		if leg.AmountMinor <= 0 {
			return ErrNonPositiveAmount
		}
		switch leg.Direction {
		case DirectionDebit:
			balances[leg.Currency] += leg.AmountMinor
		case DirectionCredit:
			balances[leg.Currency] -= leg.AmountMinor
		default:
			return ErrInvalidDirection
		}
	}

	for currency, imbalance := range balances {
		if imbalance != 0 {
			return ErrUnbalancedLegs
		}
		_ = currency
	}
	return nil
}

var (
	ErrTooFewLegs          = errors.New("a transaction needs at least two legs")
	ErrNonPositiveAmount   = errors.New("leg amount must be positive")
	ErrInvalidDirection    = errors.New("leg direction must be debit or credit")
	ErrUnbalancedLegs      = errors.New("transaction legs do not sum to zero for at least one currency")
	ErrIdempotencyReplay   = errors.New("idempotency key already used with a different payload")
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrAlreadyReversed     = errors.New("transaction has already been reversed")
)
