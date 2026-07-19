// Package service holds Auth Service's business/use-case logic. It depends
// only on domain types and the repository/mailer interfaces below — never on
// Gin or GORM directly — per docs/12-coding-standards.md §1.1.
package service

import (
	"context"

	"github.com/finverse/auth-service/internal/domain"
)

// UserRepository is implemented by internal/repository/postgres.
type UserRepository interface {
	Create(ctx context.Context, u *domain.User) error
	GetByID(ctx context.Context, id string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	Update(ctx context.Context, u *domain.User) error
}

// RefreshTokenRepository is implemented by internal/repository/postgres.
type RefreshTokenRepository interface {
	Create(ctx context.Context, t *domain.RefreshToken) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error)
	Revoke(ctx context.Context, id string) error
	RevokeFamily(ctx context.Context, familyID string) error
	RevokeAllForUser(ctx context.Context, userID string) error
}

// VerificationTokenRepository is implemented twice by internal/repository/postgres
// — once for email verification tokens, once for password reset tokens — since
// they're separate tables (docs/08-database-design.md §7) despite sharing a shape.
type VerificationTokenRepository interface {
	Create(ctx context.Context, t *domain.VerificationToken) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*domain.VerificationToken, error)
	MarkUsed(ctx context.Context, id string) error
}

// Mailer abstracts sending the two customer-facing emails Auth Service owns
// in Phase 1 — before Notification Service exists (docs/19-implementation-roadmap.md
// Phase 3), Auth sends these directly rather than waiting on an event
// consumer that isn't built yet. ConsoleMailer (dev) and a future SES-backed
// implementation both satisfy this interface without callers changing.
type Mailer interface {
	SendVerificationEmail(ctx context.Context, toEmail, token string) error
	SendPasswordResetEmail(ctx context.Context, toEmail, token string) error
}
