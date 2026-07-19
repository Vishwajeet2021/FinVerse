package service

import (
	"context"
	"fmt"
	"time"

	"github.com/finverse/auth-service/internal/domain"
	"github.com/finverse/shared/jwtauth"
	"github.com/google/uuid"
)

// AuthService implements the FR-AUTH-* requirements in
// docs/03-functional-requirements.md §2. It depends only on the interfaces
// in interfaces.go, never on a concrete database or mail transport.
type AuthService struct {
	users              UserRepository
	refreshTokens      RefreshTokenRepository
	emailVerifications VerificationTokenRepository
	passwordResets     VerificationTokenRepository
	signer             *jwtauth.Signer
	mailer             Mailer
}

func NewAuthService(
	users UserRepository,
	refreshTokens RefreshTokenRepository,
	emailVerifications VerificationTokenRepository,
	passwordResets VerificationTokenRepository,
	signer *jwtauth.Signer,
	mailer Mailer,
) *AuthService {
	return &AuthService{
		users:              users,
		refreshTokens:      refreshTokens,
		emailVerifications: emailVerifications,
		passwordResets:     passwordResets,
		signer:             signer,
		mailer:             mailer,
	}
}

// Register implements FR-AUTH-1/FR-AUTH-2: create the login identity and
// send an email verification token. The caller remains Tier 0 (unverified)
// in Customer Service's sense until VerifyEmail succeeds — that tier
// enforcement lives in Customer/Account Service, not here (docs/06-domain-driven-design.md §2.2).
func (s *AuthService) Register(ctx context.Context, email, phone, password string) (*domain.User, error) {
	if len(password) < MinPasswordLength {
		return nil, domain.ErrWeakPassword
	}

	if existing, err := s.users.GetByEmail(ctx, email); err == nil && existing != nil {
		return nil, domain.ErrEmailAlreadyRegistered
	} else if err != nil && err != domain.ErrUserNotFound {
		return nil, fmt.Errorf("checking existing user: %w", err)
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	now := time.Now()
	user := &domain.User{
		ID:            uuid.NewString(),
		Email:         email,
		Phone:         phone,
		PasswordHash:  hash,
		Role:          domain.RoleCustomer,
		EmailVerified: false,
		Status:        domain.UserStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err // already a domain error (ErrEmailAlreadyRegistered/ErrPhoneAlreadyRegistered) or wrapped infra error
	}

	if err := s.issueEmailVerificationToken(ctx, user); err != nil {
		// The user account was created successfully; a verification email
		// failure is not fatal to registration (FR-AUTH-1 is satisfied) but
		// must not be silently swallowed either — the caller logs it and the
		// customer can be offered a "resend verification email" action.
		return user, fmt.Errorf("sending verification email: %w", err)
	}

	return user, nil
}

func (s *AuthService) issueEmailVerificationToken(ctx context.Context, user *domain.User) error {
	raw, err := generateOpaqueToken()
	if err != nil {
		return fmt.Errorf("generating verification token: %w", err)
	}

	now := time.Now()
	token := &domain.VerificationToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: hashToken(raw),
		ExpiresAt: now.Add(domain.EmailVerificationTokenTTL),
		CreatedAt: now,
	}
	if err := s.emailVerifications.Create(ctx, token); err != nil {
		return fmt.Errorf("storing verification token: %w", err)
	}

	return s.mailer.SendVerificationEmail(ctx, user.Email, raw)
}

// VerifyEmail implements FR-AUTH-2.
func (s *AuthService) VerifyEmail(ctx context.Context, rawToken string) error {
	token, err := s.emailVerifications.GetByTokenHash(ctx, hashToken(rawToken))
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
		return fmt.Errorf("loading user for verification: %w", err)
	}

	user.EmailVerified = true
	user.UpdatedAt = time.Now()
	if err := s.users.Update(ctx, user); err != nil {
		return fmt.Errorf("marking user verified: %w", err)
	}

	return s.emailVerifications.MarkUsed(ctx, token.ID)
}
