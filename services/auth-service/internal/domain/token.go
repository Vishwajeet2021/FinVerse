package domain

import (
	"errors"
	"time"
)

// RefreshToken implements the rotation-family model in
// docs/10-authentication-authorization.md §2.2: every refresh issues a new
// token in the same family and revokes the old one; a revoked token being
// presented again means the family is compromised and must be revoked wholesale.
type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	FamilyID  string
	Revoked   bool
	ExpiresAt time.Time
	CreatedAt time.Time
}

// ErrRefreshTokenReused signals a revoked token was presented again — the
// caller must revoke the entire token family (docs/10-authentication-authorization.md §2.2).
var ErrRefreshTokenReused = errors.New("refresh token reuse detected")

// VerificationToken backs both email verification and password reset —
// same shape, different tables, per docs/08-database-design.md §7.
type VerificationToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

const (
	EmailVerificationTokenTTL = 24 * time.Hour
	PasswordResetTokenTTL     = 30 * time.Minute
)
