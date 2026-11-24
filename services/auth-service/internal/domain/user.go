// Package domain holds Auth Service's entities and domain errors. It imports
// no framework or infrastructure package, per docs/12-coding-standards.md §1.1 —
// this is what lets the service's business rules be read and reasoned about
// independent of Gin/GORM/Postgres.
package domain

import (
	"errors"
	"time"
)

// UserStatus mirrors the `status` check constraint in
// db/migrations/0001_init.up.sql.
type UserStatus string

const (
	UserStatusActive   UserStatus = "ACTIVE"
	UserStatusLocked   UserStatus = "LOCKED"
	UserStatusDisabled UserStatus = "DISABLED"
)

// Role mirrors docs/10-authentication-authorization.md §3.1.
type Role string

const (
	RoleCustomer     Role = "customer"
	RoleSupport      Role = "support"
	RoleFraudAnalyst Role = "fraud_analyst"
	RoleAuditor      Role = "auditor"
	RoleAdmin        Role = "admin"
	RoleSuperAdmin   Role = "super_admin"
)

// User is the login identity — distinct from Customer, which lives in
// Customer Service, per docs/06-domain-driven-design.md §2.1-2.2.
type User struct {
	ID               string
	Email            string
	Phone            string
	PasswordHash     string
	Role             Role
	EmailVerified    bool
	Status           UserStatus
	FailedLoginCount int
	LockedUntil      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// Second factor (docs/24-admin-mfa.md). These are loaded with the user but never written by
	// UserRepository.Update: only the MFA repository changes them, with conditional statements, so
	// a stale copy of this struct saved elsewhere cannot roll back replay protection.
	MFAEnabled    bool
	MFASecretEnc  string // encrypted; empty when no enrolment has begun
	MFALastStep   int64
	MFAEnrolledAt *time.Time
}

// RequiresMFA reports whether this role may not hold a session without a second factor:
// every internal role. Customers may enrol but are not forced to
// (docs/10-authentication-authorization.md §5, §6).
func (r Role) RequiresMFA() bool { return r != RoleCustomer }

// MaxFailedLoginAttempts and LockoutDuration implement FR-AUTH-9.
const (
	MaxFailedLoginAttempts = 5
	LockoutDuration        = 15 * time.Minute
)

// IsLocked reports whether the account is currently within a lockout window.
func (u *User) IsLocked(now time.Time) bool {
	return u.Status == UserStatusLocked && u.LockedUntil != nil && now.Before(*u.LockedUntil)
}

// Domain errors. Handlers map these to HTTP status/error codes
// (docs/12-coding-standards.md §1.3); they are never string-matched.
var (
	ErrEmailAlreadyRegistered = errors.New("email is already registered")
	ErrPhoneAlreadyRegistered = errors.New("phone is already registered")
	ErrWeakPassword           = errors.New("password does not meet the minimum strength requirement")
	ErrUserNotFound           = errors.New("user not found")
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrAccountLocked          = errors.New("account is temporarily locked due to repeated failed logins")
	ErrAccountDisabled        = errors.New("account is disabled")
	ErrEmailNotVerified       = errors.New("email address is not verified")
	ErrTokenInvalidOrExpired  = errors.New("token is invalid or has expired")
	ErrTokenAlreadyUsed       = errors.New("token has already been used")

	// Second factor.
	ErrMFACodeInvalid      = errors.New("the verification code is not valid")
	ErrMFAChallengeInvalid = errors.New("the sign-in step has expired or was already used")
	ErrMFAAlreadyEnabled   = errors.New("two-step verification is already set up")
	ErrMFANotStarted       = errors.New("two-step verification setup has not been started")
	ErrMFARequired         = errors.New("this account requires two-step verification")
)
