package service

import (
	"context"
	"fmt"
	"time"

	"github.com/finverse/auth-service/internal/domain"
	"github.com/finverse/shared/jwtauth"
	"github.com/google/uuid"
)

// TokenPair is what every login/refresh call returns to the caller. The
// transport layer (internal/transport/http) puts RefreshToken in an
// HttpOnly cookie rather than the JSON body, per
// docs/10-authentication-authorization.md §2 — RefreshTokenExpiresAt lets it
// set an accurate cookie Max-Age without re-deriving the TTL.
type TokenPair struct {
	AccessToken           string
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
}

// Login implements FR-AUTH-3 and the lockout rule FR-AUTH-9.
func (s *AuthService) Login(ctx context.Context, email, password string) (*TokenPair, error) {
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if err == domain.ErrUserNotFound {
			// Deliberately the same error as a wrong password — never reveal
			// which part of the credential pair was wrong (enumeration defense).
			return nil, domain.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("loading user: %w", err)
	}

	now := time.Now()
	if user.IsLocked(now) {
		return nil, domain.ErrAccountLocked
	}
	if user.Status == domain.UserStatusDisabled {
		return nil, domain.ErrAccountDisabled
	}

	ok, err := VerifyPassword(password, user.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("verifying password: %w", err)
	}
	if !ok {
		return nil, s.recordFailedLogin(ctx, user, now)
	}

	if user.FailedLoginCount > 0 || user.Status == domain.UserStatusLocked {
		user.FailedLoginCount = 0
		user.Status = domain.UserStatusActive
		user.LockedUntil = nil
		user.UpdatedAt = now
		if err := s.users.Update(ctx, user); err != nil {
			return nil, fmt.Errorf("resetting lockout state: %w", err)
		}
	}

	return s.issueTokenPair(ctx, user)
}

// recordFailedLogin implements FR-AUTH-9: lock the account for
// domain.LockoutDuration after domain.MaxFailedLoginAttempts consecutive
// failures. It always returns domain.ErrInvalidCredentials so the caller
// doesn't need its own branching on top of this.
func (s *AuthService) recordFailedLogin(ctx context.Context, user *domain.User, now time.Time) error {
	user.FailedLoginCount++
	if user.FailedLoginCount >= domain.MaxFailedLoginAttempts {
		user.Status = domain.UserStatusLocked
		lockedUntil := now.Add(domain.LockoutDuration)
		user.LockedUntil = &lockedUntil
	}
	user.UpdatedAt = now
	if err := s.users.Update(ctx, user); err != nil {
		return fmt.Errorf("recording failed login: %w", err)
	}
	return domain.ErrInvalidCredentials
}

// issueTokenPair mints a fresh access token and starts a new refresh-token
// rotation family, per docs/10-authentication-authorization.md §2.
func (s *AuthService) issueTokenPair(ctx context.Context, user *domain.User) (*TokenPair, error) {
	return s.issueTokenPairInFamily(ctx, user, uuid.NewString())
}

func (s *AuthService) issueTokenPairInFamily(ctx context.Context, user *domain.User, familyID string) (*TokenPair, error) {
	access, err := s.signer.Sign(jwtauth.AccessTokenInput{
		UserID:  user.ID,
		Role:    string(user.Role),
		Scopes:  scopesForRole(user.Role),
		KYCTier: 0, // Customer Service owns KYC tier; Phase 1 Auth has no cross-service call yet.
		MFA:     false,
	})
	if err != nil {
		return nil, fmt.Errorf("signing access token: %w", err)
	}

	rawRefresh, err := generateOpaqueToken()
	if err != nil {
		return nil, fmt.Errorf("generating refresh token: %w", err)
	}

	ttl := jwtauth.CustomerRefreshTokenTTL
	if RefreshTokenTTLForRole(user.Role) {
		ttl = jwtauth.AdminRefreshTokenTTL
	}

	now := time.Now()
	refreshRecord := &domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: hashToken(rawRefresh),
		FamilyID:  familyID,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	if err := s.refreshTokens.Create(ctx, refreshRecord); err != nil {
		return nil, fmt.Errorf("storing refresh token: %w", err)
	}

	return &TokenPair{AccessToken: access, RefreshToken: rawRefresh, RefreshTokenExpiresAt: refreshRecord.ExpiresAt}, nil
}

// Refresh implements FR-AUTH-6: rotate the refresh token and detect reuse of
// an already-revoked token as a compromise signal.
func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken string) (*TokenPair, error) {
	record, err := s.refreshTokens.GetByTokenHash(ctx, hashToken(rawRefreshToken))
	if err != nil {
		return nil, domain.ErrTokenInvalidOrExpired
	}

	if record.Revoked {
		// A revoked token being presented again means it was stolen and used
		// by both the legitimate holder and an attacker (or vice versa) — the
		// whole family is untrustworthy now, so kill it entirely and force
		// re-authentication (docs/10-authentication-authorization.md §2.2).
		_ = s.refreshTokens.RevokeFamily(ctx, record.FamilyID)
		return nil, domain.ErrRefreshTokenReused
	}
	if time.Now().After(record.ExpiresAt) {
		return nil, domain.ErrTokenInvalidOrExpired
	}

	user, err := s.users.GetByID(ctx, record.UserID)
	if err != nil {
		return nil, fmt.Errorf("loading user for refresh: %w", err)
	}

	if err := s.refreshTokens.Revoke(ctx, record.ID); err != nil {
		return nil, fmt.Errorf("revoking used refresh token: %w", err)
	}

	return s.issueTokenPairInFamily(ctx, user, record.FamilyID)
}

// Logout implements FR-AUTH-7 (single session).
func (s *AuthService) Logout(ctx context.Context, rawRefreshToken string) error {
	record, err := s.refreshTokens.GetByTokenHash(ctx, hashToken(rawRefreshToken))
	if err != nil {
		// Already invalid/gone — logging out of a session that doesn't exist
		// is a no-op success, not an error, from the client's point of view.
		return nil
	}
	return s.refreshTokens.Revoke(ctx, record.ID)
}

// LogoutAll implements FR-AUTH-7 (revoke every session for the user).
func (s *AuthService) LogoutAll(ctx context.Context, userID string) error {
	return s.refreshTokens.RevokeAllForUser(ctx, userID)
}
