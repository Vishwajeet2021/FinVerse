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

// Payment is the aggregate root. Only the INTERNAL rail is implemented in
// this phase (docs/19-implementation-roadmap.md Phase 2) -- NEFT/RTGS/IMPS/UPI
// timing simulation is Phase 3.
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
}

var (
	ErrPaymentNotFound  = errors.New("payment not found")
	ErrInvalidAmount    = errors.New("amount must be a positive number of minor units")
	ErrSameAccount      = errors.New("cannot transfer an account to itself")
)
