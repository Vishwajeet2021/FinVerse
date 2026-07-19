package service

import (
	"context"
	"fmt"
	"time"

	"github.com/finverse/auth-service/internal/domain"
	"github.com/google/uuid"
)

// RequestPasswordReset implements FR-AUTH-8. It never reveals whether the
// email exists — the same "no error either way" response prevents account
// enumeration, per docs/11-security-standards.md.
func (s *AuthService) RequestPasswordReset(ctx context.Context, email string) error {
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if err == domain.ErrUserNotFound {
			return nil
		}
		return fmt.Errorf("loading user: %w", err)
	}

	raw, err := generateOpaqueToken()
	if err != nil {
		return fmt.Errorf("generating reset token: %w", err)
	}

	now := time.Now()
	token := &domain.VerificationToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: hashToken(raw),
		ExpiresAt: now.Add(domain.PasswordResetTokenTTL),
		CreatedAt: now,
	}
	if err := s.passwordResets.Create(ctx, token); err != nil {
		return fmt.Errorf("storing reset token: %w", err)
	}

	return s.mailer.SendPasswordResetEmail(ctx, user.Email, raw)
}

// ResetPassword implements FR-AUTH-8. Per docs/10-authentication-authorization.md §2.2,
// a password change also revokes every existing session — if the password
// leaked, whoever had it should lose access the moment it's rotated out.
func (s *AuthService) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if len(newPassword) < MinPasswordLength {
		return domain.ErrWeakPassword
	}

	token, err := s.passwordResets.GetByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		return domain.ErrTokenInvalidOrExpired
	}
	if token.UsedAt != nil {
		return domain.ErrTokenAlreadyUsed
	}
	if time.Now().After(token.ExpiresAt) {
		return domain.ErrTokenInvalidOrExpired
	}

	user, err := s.users.GetByID(ctx, token.UserID)
	if err != nil {
		return fmt.Errorf("loading user for password reset: %w", err)
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hashing new password: %w", err)
	}
	user.PasswordHash = hash
	user.UpdatedAt = time.Now()
	if err := s.users.Update(ctx, user); err != nil {
		return fmt.Errorf("saving new password: %w", err)
	}

	if err := s.passwordResets.MarkUsed(ctx, token.ID); err != nil {
		return fmt.Errorf("marking reset token used: %w", err)
	}

	return s.refreshTokens.RevokeAllForUser(ctx, user.ID)
}
