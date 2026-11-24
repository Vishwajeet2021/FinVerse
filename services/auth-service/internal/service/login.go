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

// LoginResult is what a password check yields: either a session (Tokens), or a pending second
// step (Challenge). Exactly one is set. A user whose role requires a second factor can never
// receive Tokens from Login itself.
type LoginResult struct {
	Tokens    *TokenPair
	Challenge *Challenge
}

// Challenge tells the caller which second step is owed and carries the one-time token that
// proves the password step was passed. The token is shown once and only its hash is stored.
type Challenge struct {
	Token   string
	Purpose domain.ChallengePurpose
}

// Login implements FR-AUTH-3 and the lockout rule FR-AUTH-9, then the second-factor policy of
// docs/24-admin-mfa.md: a user who has MFA, or whose role requires it, gets a challenge instead
// of tokens.
func (s *AuthService) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if err == domain.ErrUserNotFound {
			// Deliberately the same error as a wrong password — never reveal
			// which part of the credential pair was wrong (enumeration defense).
			return nil, domain.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("loading user: %w", err)
	}

	now := s.now()
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

	if user.MFAEnabled || user.Role.RequiresMFA() {
		// The failure counter is deliberately NOT reset here. If a correct password cleared it,
		// someone who knows the password could guess six-digit codes indefinitely: five wrong
		// codes, log in again (counter cleared), five more. It is reset only when the second
		// step succeeds.
		return s.startChallenge(ctx, user)
	}

	if err := s.clearLockoutState(ctx, user, now); err != nil {
		return nil, err
	}
	tokens, err := s.issueTokenPair(ctx, user, false)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Tokens: tokens}, nil
}

// clearLockoutState resets the failure counter after a fully successful sign-in.
func (s *AuthService) clearLockoutState(ctx context.Context, user *domain.User, now time.Time) error {
	if user.FailedLoginCount == 0 && user.Status != domain.UserStatusLocked {
		return nil
	}
	user.FailedLoginCount = 0
	user.Status = domain.UserStatusActive
	user.LockedUntil = nil
	user.UpdatedAt = now
	if err := s.users.Update(ctx, user); err != nil {
		return fmt.Errorf("resetting lockout state: %w", err)
	}
	return nil
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
//
// mfaVerified is recorded on the refresh token and copied down the rotation family, so the
// access token minted at every refresh carries the same mfa claim the session earned at login.
func (s *AuthService) issueTokenPair(ctx context.Context, user *domain.User, mfaVerified bool) (*TokenPair, error) {
	return s.issueTokenPairInFamily(ctx, user, uuid.NewString(), mfaVerified)
}

func (s *AuthService) issueTokenPairInFamily(ctx context.Context, user *domain.User, familyID string, mfaVerified bool) (*TokenPair, error) {
	access, err := s.signer.Sign(jwtauth.AccessTokenInput{
		UserID:  user.ID,
		Role:    string(user.Role),
		Scopes:  scopesForRole(user.Role),
		KYCTier: 0, // Customer Service owns KYC tier; Phase 1 Auth has no cross-service call yet.
		MFA:     mfaVerified,
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
		ID:          uuid.NewString(),
		UserID:      user.ID,
		TokenHash:   hashToken(rawRefresh),
		FamilyID:    familyID,
		MFAVerified: mfaVerified,
		ExpiresAt:   now.Add(ttl),
		CreatedAt:   now,
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
	if s.now().After(record.ExpiresAt) {
		return nil, domain.ErrTokenInvalidOrExpired
	}

	user, err := s.users.GetByID(ctx, record.UserID)
	if err != nil {
		return nil, fmt.Errorf("loading user for refresh: %w", err)
	}

	// A session for an internal role that never passed the second factor (one opened before MFA
	// existed, or by any path that skipped it) is not renewed: it ends here and must sign in again,
	// which routes through enrolment.
	if user.Role.RequiresMFA() && !record.MFAVerified {
		_ = s.refreshTokens.RevokeFamily(ctx, record.FamilyID)
		return nil, domain.ErrMFARequired
	}

	if err := s.refreshTokens.Revoke(ctx, record.ID); err != nil {
		return nil, fmt.Errorf("revoking used refresh token: %w", err)
	}

	return s.issueTokenPairInFamily(ctx, user, record.FamilyID, record.MFAVerified)
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
