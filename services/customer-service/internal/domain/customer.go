// Package domain holds Customer Service's entities and domain errors. It
// imports no framework or infrastructure package, per
// docs/12-coding-standards.md §1.1 — this is what keeps the KYC state machine
// and other business rules legible independent of Gin/GORM/Postgres.
package domain

import (
	"errors"
	"time"
)

// KYCTier mirrors docs/06-domain-driven-design.md §2.2: 0 (unverified),
// 1 (basic), 2 (full).
type KYCTier int

const (
	KYCTierUnverified KYCTier = 0
	KYCTierBasic      KYCTier = 1
	KYCTierFull       KYCTier = 2
)

// KYCStatus mirrors the `kyc_status` check constraint in
// db/migrations/0001_init.up.sql and the state machine in FR-CUST-3.
type KYCStatus string

const (
	KYCStatusPending     KYCStatus = "PENDING"
	KYCStatusUnderReview KYCStatus = "UNDER_REVIEW"
	KYCStatusApproved    KYCStatus = "APPROVED"
	KYCStatusRejected    KYCStatus = "REJECTED"
)

// Customer is a real-world person with a KYC status and profile, distinct
// from the User identity that authenticates as them
// (docs/06-domain-driven-design.md §2.2). PANEncrypted/PANMasked are kept
// separate per docs/08-database-design.md §6: nothing that displays a
// customer's PAN in a UI or log ever needs to decrypt it.
type Customer struct {
	ID           string
	UserID       string
	FullName     string
	DOB          time.Time
	PANEncrypted []byte
	PANMasked    string
	KYCTier      KYCTier
	KYCStatus    KYCStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// KYCDocument is metadata for an uploaded KYC document; the binary itself
// lives behind the Storage interface (internal/service), never in this
// struct or the relational DB, per FR-CUST-2.
type KYCDocument struct {
	ID         string
	CustomerID string
	DocType    string
	StorageKey string
	UploadedAt time.Time
}

// Beneficiary is a payee a customer has registered for transfers; it has its
// own verification state independent of the customer's own KYC tier
// (docs/06-domain-driven-design.md §2.2, FR-CUST-4).
type Beneficiary struct {
	ID         string
	CustomerID string
	Name       string
	AccountRef string
	Verified   bool
	CreatedAt  time.Time
}

// Domain errors. Handlers map these to HTTP status/error codes
// (docs/12-coding-standards.md §1.3); they are never string-matched.
var (
	ErrCustomerNotFound     = errors.New("customer not found")
	ErrProfileAlreadyExists = errors.New("a customer profile already exists for this user")
	ErrInvalidKYCTransition = errors.New("invalid KYC state transition")
	ErrBeneficiaryNotFound  = errors.New("beneficiary not found")
	ErrDocumentNotFound     = errors.New("document not found")
)
