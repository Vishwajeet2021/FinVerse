// Package domain holds Payment Service's entities and domain errors -- zero
// framework imports, per docs/12-coding-standards.md §1.1. Payment is a
// *coordinator*, not a ledger -- it always delegates actual posting to
// Transaction Service (docs/06-domain-driven-design.md §2.6).
package domain

import (
	"errors"
	"time"
)

type Rail string

const (
	RailInternal Rail = "INTERNAL"
	RailNEFT     Rail = "NEFT"
	RailRTGS     Rail = "RTGS"
	RailIMPS     Rail = "IMPS"
	RailUPI      Rail = "UPI"
)

type Status string

const (
	StatusInitiated  Status = "INITIATED"
	StatusProcessing Status = "PROCESSING"
	StatusCompleted  Status = "COMPLETED"
	StatusFailed     Status = "FAILED"
	StatusReversed   Status = "REVERSED"
)

// Payment is the aggregate root. INTERNAL settles synchronously; NEFT, RTGS,
// IMPS and UPI are simulated with rail-specific limits and settlement timing
// (internal/service/rails.go).
type Payment struct {
	ID             string
	IdempotencyKey string
	FromAccountID  string
	ToReference    string
	Rail           Rail
	AmountMinor    int64
	Status         Status
	TransactionID  *string
	FailureReason  *string
	CreatedAt      time.Time
	// SettleAt / SettleAttempts are only meaningful for asynchronous rails
	// (NEFT/RTGS/IMPS/UPI): when the rail clears the payment, and how many
	// settlement attempts have been made (bounded retry, FR-PAY-8).
	SettleAt       *time.Time
	SettleAttempts int
}

var (
	ErrPaymentNotFound          = errors.New("payment not found")
	ErrInvalidAmount            = errors.New("amount must be a positive number of minor units")
	ErrSameAccount              = errors.New("cannot transfer an account to itself")
	ErrRecipientAccountNotFound = errors.New("no account exists with that account number")
)

// MaxAmountMinor is the largest single amount (INR 1 crore) accepted by any money-moving path.
// It bounds arithmetic and sits far above retail limits, which Fraud enforces per rail.
const MaxAmountMinor int64 = 1_000_000_000
