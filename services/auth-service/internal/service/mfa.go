package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/finverse/auth-service/internal/domain"
	"github.com/finverse/auth-service/internal/mfa"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// issuerName is what authenticator apps display next to the account.
const issuerName = "FinVerse"

// Second-factor flows (docs/24-admin-mfa.md).
//
//	password ok ──► user has MFA ──────────────► LOGIN challenge ──► CompleteLogin(code) ──► session
//	            └─► role requires it, none yet ─► ENROLL challenge ─► Begin/ConfirmLoginEnrollment ──► session
//	            └─► otherwise ───────────────────► session (no MFA)
//
// The invariant the rest of the system relies on: a role that requires MFA is never handed an
// access or refresh token without having passed this file.

// Method says how the second step was satisfied; callers log it.
type Method string

const (
	MethodTOTP     Method = "totp"
	MethodRecovery Method = "recovery_code"
)

// Enrollment is what the user needs to add the account to an authenticator app.
type Enrollment struct {
	Secret string // base32, for typing in by hand
	URI    string // otpauth:// link, for the QR code
}

// EnrollmentResult is the outcome of confirming enrolment during sign-in.
type EnrollmentResult struct {
	Tokens        *TokenPair
	RecoveryCodes []string
}

func (s *AuthService) startChallenge(ctx context.Context, user *domain.User) (*LoginResult, error) {
	purpose := domain.ChallengeEnroll
	if user.MFAEnabled {
		purpose = domain.ChallengeLogin
	}
	raw, err := generateOpaqueToken()
	if err != nil {
		return nil, fmt.Errorf("generating MFA challenge: %w", err)
	}
	now := s.now()
	if err := s.mfaRepo.CreateChallenge(ctx, &domain.MFAChallenge{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: hashToken(raw),
		Purpose:   purpose,
		ExpiresAt: now.Add(domain.MFAChallengeTTL),
		CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return &LoginResult{Challenge: &Challenge{Token: raw, Purpose: purpose}}, nil
}

// loadChallenge resolves a presented challenge token to a usable challenge and its user. Every
// way it can be unusable (unknown, wrong purpose, used, expired, out of attempts) returns the
// same error, so a caller learns nothing about which.
func (s *AuthService) loadChallenge(ctx context.Context, raw string, purpose domain.ChallengePurpose) (*domain.MFAChallenge, *domain.User, error) {
	ch, err := s.mfaRepo.GetChallengeByHash(ctx, hashToken(raw))
	if err != nil {
		return nil, nil, err
	}
	if ch.Purpose != purpose || ch.ConsumedAt != nil || s.now().After(ch.ExpiresAt) ||
		ch.Attempts >= domain.MFAChallengeMaxAttempts {
		return nil, nil, domain.ErrMFAChallengeInvalid
	}
	user, err := s.users.GetByID(ctx, ch.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("loading user for MFA challenge: %w", err)
	}
	if user.IsLocked(s.now()) {
		return nil, nil, domain.ErrAccountLocked
	}
	if user.Status == domain.UserStatusDisabled {
		return nil, nil, domain.ErrAccountDisabled
	}
	return ch, user, nil
}

// rejectCode is the one place a wrong code is punished: it spends one of the challenge's
// attempts AND counts as a failed login for the account (FR-AUTH-9). The second is what makes
// opening a fresh challenge pointless as a way to get more guesses.
func (s *AuthService) rejectCode(ctx context.Context, ch *domain.MFAChallenge, user *domain.User) error {
	attempts, err := s.mfaRepo.RecordChallengeAttempt(ctx, ch.ID)
	if err != nil {
		return err
	}
	if err := s.recordFailedLogin(ctx, user, s.now()); err != nil && err != domain.ErrInvalidCredentials {
		return err
	}
	s.log.Warn("mfa_code_rejected",
		zap.String("user_id", user.ID), zap.String("purpose", string(ch.Purpose)),
		zap.Int("challenge_attempts", attempts), zap.Int("failed_logins", user.FailedLoginCount))
	return domain.ErrMFACodeInvalid
}

// checkSecondFactor validates a code from the user's authenticator or a recovery code.
func (s *AuthService) checkSecondFactor(ctx context.Context, user *domain.User, code string) (Method, bool, error) {
	code = strings.TrimSpace(code)

	if mfa.IsTOTPShape(code) {
		secret, err := s.mfaCipher.Open(user.ID, user.MFASecretEnc)
		if err != nil {
			return "", false, fmt.Errorf("opening MFA secret: %w", err)
		}
		step, ok := mfa.Verify(secret, code, s.now())
		if !ok {
			return "", false, nil
		}
		// Accept each 30-second step once. Without this, a code someone watched being typed
		// would work again for up to 90 seconds.
		fresh, err := s.mfaRepo.AdvanceStep(ctx, user.ID, step)
		if err != nil {
			return "", false, err
		}
		return MethodTOTP, fresh, nil
	}

	if norm, ok := mfa.NormalizeRecoveryCode(code); ok {
		used, err := s.mfaRepo.ConsumeRecoveryCode(ctx, user.ID, hashToken(norm))
		if err != nil {
			return "", false, err
		}
		return MethodRecovery, used, nil
	}
	return "", false, nil
}

// CompleteLogin finishes a LOGIN challenge with a TOTP or recovery code and issues the session.
func (s *AuthService) CompleteLogin(ctx context.Context, rawChallenge, code string) (*TokenPair, Method, error) {
	ch, user, err := s.loadChallenge(ctx, rawChallenge, domain.ChallengeLogin)
	if err != nil {
		return nil, "", err
	}
	method, ok, err := s.checkSecondFactor(ctx, user, code)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", s.rejectCode(ctx, ch, user)
	}
	consumed, err := s.mfaRepo.ConsumeChallenge(ctx, ch.ID)
	if err != nil {
		return nil, "", err
	}
	if !consumed {
		return nil, "", domain.ErrMFAChallengeInvalid
	}
	if err := s.clearLockoutState(ctx, user, s.now()); err != nil {
		return nil, "", err
	}
	tokens, err := s.issueTokenPair(ctx, user, true)
	if err != nil {
		return nil, "", err
	}
	if method == MethodRecovery {
		// A recovery code means the phone is gone or unavailable. Worth a loud line: it is also
		// what a successful theft of a printed code sheet would look like.
		s.log.Warn("mfa_recovery_code_used", zap.String("user_id", user.ID), zap.String("role", string(user.Role)))
	}
	s.log.Info("mfa_login_succeeded", zap.String("user_id", user.ID), zap.String("role", string(user.Role)), zap.String("method", string(method)))
	return tokens, method, nil
}

// beginEnrollment creates (or replaces) the pending secret. Until it is confirmed it is not
// trusted for anything.
func (s *AuthService) beginEnrollment(ctx context.Context, user *domain.User) (*Enrollment, error) {
	if user.MFAEnabled {
		return nil, domain.ErrMFAAlreadyEnabled
	}
	secret, err := mfa.NewSecret()
	if err != nil {
		return nil, err
	}
	enc, err := s.mfaCipher.Seal(user.ID, secret)
	if err != nil {
		return nil, fmt.Errorf("encrypting MFA secret: %w", err)
	}
	if err := s.mfaRepo.SetPendingSecret(ctx, user.ID, enc); err != nil {
		return nil, err
	}
	return &Enrollment{Secret: mfa.EncodeSecret(secret), URI: mfa.ProvisioningURI(issuerName, user.Email, secret)}, nil
}

// confirmEnrollment proves the user can produce a code from the pending secret, then turns MFA
// on and returns the recovery codes. They are returned exactly once; only hashes are kept.
func (s *AuthService) confirmEnrollment(ctx context.Context, user *domain.User, code string) ([]string, error) {
	if user.MFAEnabled {
		return nil, domain.ErrMFAAlreadyEnabled
	}
	if user.MFASecretEnc == "" {
		return nil, domain.ErrMFANotStarted
	}
	secret, err := s.mfaCipher.Open(user.ID, user.MFASecretEnc)
	if err != nil {
		return nil, fmt.Errorf("opening pending MFA secret: %w", err)
	}
	step, ok := mfa.Verify(secret, strings.TrimSpace(code), s.now())
	if !ok {
		return nil, domain.ErrMFACodeInvalid
	}
	codes, err := mfa.NewRecoveryCodes()
	if err != nil {
		return nil, err
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		norm, _ := mfa.NormalizeRecoveryCode(c)
		hashes[i] = hashToken(norm)
	}
	if err := s.mfaRepo.Activate(ctx, user.ID, step, hashes, s.now()); err != nil {
		return nil, err
	}
	s.log.Info("mfa_enrolled", zap.String("user_id", user.ID), zap.String("role", string(user.Role)))
	return codes, nil
}

// BeginLoginEnrollment starts enrolment for a user whose role requires MFA and who has none.
// It is authorised by the ENROLL challenge alone; there is no session yet.
func (s *AuthService) BeginLoginEnrollment(ctx context.Context, rawChallenge string) (*Enrollment, error) {
	_, user, err := s.loadChallenge(ctx, rawChallenge, domain.ChallengeEnroll)
	if err != nil {
		return nil, err
	}
	return s.beginEnrollment(ctx, user)
}

// ConfirmLoginEnrollment finishes enrolment and, because the user has now proved the factor
// they just set up, completes the sign-in in the same step.
func (s *AuthService) ConfirmLoginEnrollment(ctx context.Context, rawChallenge, code string) (*EnrollmentResult, error) {
	ch, user, err := s.loadChallenge(ctx, rawChallenge, domain.ChallengeEnroll)
	if err != nil {
		return nil, err
	}
	codes, err := s.confirmEnrollment(ctx, user, code)
	if err != nil {
		if err == domain.ErrMFACodeInvalid {
			return nil, s.rejectCode(ctx, ch, user)
		}
		return nil, err
	}
	consumed, err := s.mfaRepo.ConsumeChallenge(ctx, ch.ID)
	if err != nil {
		return nil, err
	}
	if !consumed {
		return nil, domain.ErrMFAChallengeInvalid
	}
	if err := s.clearLockoutState(ctx, user, s.now()); err != nil {
		return nil, err
	}
	tokens, err := s.issueTokenPair(ctx, user, true)
	if err != nil {
		return nil, err
	}
	return &EnrollmentResult{Tokens: tokens, RecoveryCodes: codes}, nil
}

// BeginSelfEnrollment lets a signed-in user (in practice a customer; internal roles cannot be
// signed in without MFA already) set up a second factor. It re-checks the password: a stolen
// access token alone must not be enough to bind someone's account to an attacker's phone.
func (s *AuthService) BeginSelfEnrollment(ctx context.Context, userID, password string) (*Enrollment, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("loading user: %w", err)
	}
	if ok, err := VerifyPassword(password, user.PasswordHash); err != nil {
		return nil, fmt.Errorf("verifying password: %w", err)
	} else if !ok {
		return nil, s.recordPasswordFailure(ctx, user)
	}
	return s.beginEnrollment(ctx, user)
}

// ConfirmSelfEnrollment completes BeginSelfEnrollment and returns the recovery codes.
func (s *AuthService) ConfirmSelfEnrollment(ctx context.Context, userID, code string) ([]string, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("loading user: %w", err)
	}
	if user.IsLocked(s.now()) {
		return nil, domain.ErrAccountLocked
	}
	codes, err := s.confirmEnrollment(ctx, user, code)
	if err == domain.ErrMFACodeInvalid {
		_ = s.recordFailedLogin(ctx, user, s.now())
	}
	return codes, err
}

func (s *AuthService) recordPasswordFailure(ctx context.Context, user *domain.User) error {
	return s.recordFailedLogin(ctx, user, s.now())
}
