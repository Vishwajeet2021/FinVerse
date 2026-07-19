package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/finverse/auth-service/internal/domain"
	"github.com/finverse/shared/jwtauth"
)

func testSigner(t *testing.T) *jwtauth.Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test RSA key: %v", err)
	}
	return jwtauth.NewSigner(key, "test-kid", "https://auth.finverse.local", "finverse-api")
}

func newTestAuthService(t *testing.T) (*AuthService, *fakeUserRepo, *fakeRefreshTokenRepo, *fakeMailer) {
	t.Helper()
	users := newFakeUserRepo()
	refreshTokens := newFakeRefreshTokenRepo()
	emailVerifications := newFakeVerificationTokenRepo()
	passwordResets := newFakeVerificationTokenRepo()
	mailer := &fakeMailer{}
	svc := NewAuthService(users, refreshTokens, emailVerifications, passwordResets, testSigner(t), mailer)
	return svc, users, refreshTokens, mailer
}

func TestRegister(t *testing.T) {
	cases := []struct {
		name        string
		email       string
		password    string
		preexisting bool
		wantErr     error
	}{
		{name: "success", email: "new@finverse.dev", password: "correct-horse-battery", wantErr: nil},
		{name: "password too short", email: "short@finverse.dev", password: "short1", wantErr: domain.ErrWeakPassword},
		{name: "duplicate email", email: "dup@finverse.dev", password: "correct-horse-battery", preexisting: true, wantErr: domain.ErrEmailAlreadyRegistered},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, users, _, mailer := newTestAuthService(t)
			ctx := context.Background()

			if tc.preexisting {
				if _, err := svc.Register(ctx, tc.email, "", "correct-horse-battery"); err != nil {
					t.Fatalf("seeding preexisting user: %v", err)
				}
			}

			user, err := svc.Register(ctx, tc.email, "", tc.password)

			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("got err %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if user.Role != domain.RoleCustomer {
				t.Errorf("got role %v, want customer", user.Role)
			}
			if user.EmailVerified {
				t.Errorf("new user should not be email-verified yet")
			}
			if _, ok := users.byEmail[tc.email]; !ok {
				t.Errorf("user was not persisted")
			}
			if len(mailer.verificationSentTo) != 1 || mailer.verificationSentTo[0] != tc.email {
				t.Errorf("verification email not sent to %s: %v", tc.email, mailer.verificationSentTo)
			}
		})
	}
}

func TestLogin_LockoutAfterMaxFailedAttempts(t *testing.T) {
	svc, _, _, _ := newTestAuthService(t)
	ctx := context.Background()

	const email = "lockout@finverse.dev"
	const correctPassword = "correct-horse-battery"
	if _, err := svc.Register(ctx, email, "", correctPassword); err != nil {
		t.Fatalf("registering: %v", err)
	}

	var lastErr error
	for i := 0; i < domain.MaxFailedLoginAttempts; i++ {
		_, lastErr = svc.Login(ctx, email, "wrong-password")
		if lastErr != domain.ErrInvalidCredentials {
			t.Fatalf("attempt %d: got %v, want ErrInvalidCredentials", i, lastErr)
		}
	}

	// One more attempt, even with the CORRECT password, must now be locked out.
	if _, err := svc.Login(ctx, email, correctPassword); err != domain.ErrAccountLocked {
		t.Fatalf("got %v, want ErrAccountLocked", err)
	}
}

func TestLogin_Success(t *testing.T) {
	svc, _, _, _ := newTestAuthService(t)
	ctx := context.Background()

	const email = "login-success@finverse.dev"
	const password = "correct-horse-battery"
	if _, err := svc.Register(ctx, email, "", password); err != nil {
		t.Fatalf("registering: %v", err)
	}

	pair, err := svc.Login(ctx, email, password)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatalf("expected non-empty token pair, got %+v", pair)
	}
}

func TestRefresh_ReuseOfRevokedTokenKillsWholeFamily(t *testing.T) {
	svc, _, refreshTokens, _ := newTestAuthService(t)
	ctx := context.Background()

	const email = "reuse@finverse.dev"
	const password = "correct-horse-battery"
	if _, err := svc.Register(ctx, email, "", password); err != nil {
		t.Fatalf("registering: %v", err)
	}
	firstPair, err := svc.Login(ctx, email, password)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Legitimate rotation: refresh once, the old token is now revoked.
	secondPair, err := svc.Refresh(ctx, firstPair.RefreshToken)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if secondPair.RefreshToken == firstPair.RefreshToken {
		t.Fatalf("refresh must rotate the token, got the same value back")
	}

	// An attacker (or a client with a stale copy) replays the now-revoked
	// first token — this must fail AND revoke the whole family, so the
	// legitimate second token must also stop working afterwards.
	if _, err := svc.Refresh(ctx, firstPair.RefreshToken); err != domain.ErrRefreshTokenReused {
		t.Fatalf("got %v, want ErrRefreshTokenReused", err)
	}

	// The second token was itself revoked by the family kill above, so
	// presenting it now hits the same "revoked token presented" reuse path —
	// which is the correct, fail-safe behavior: the service can't distinguish
	// "revoked by normal rotation" from "revoked by a reuse response" and
	// deliberately treats both identically rather than leaking that distinction.
	if _, err := svc.Refresh(ctx, secondPair.RefreshToken); err != domain.ErrRefreshTokenReused {
		t.Fatalf("expected the legitimate second token to also be rejected as reused, got %v", err)
	}

	// Belt-and-suspenders: confirm every token in the family is actually
	// marked revoked in storage, not just individually rejected by chance.
	revokedCount := 0
	for _, rt := range refreshTokens.byID {
		if rt.Revoked {
			revokedCount++
		}
	}
	if revokedCount != 2 {
		t.Fatalf("expected 2 revoked tokens in the family, got %d", revokedCount)
	}
}

func TestResetPassword_RevokesExistingSessionsAndRotatesCredential(t *testing.T) {
	svc, _, refreshTokens, mailer := newTestAuthService(t)
	ctx := context.Background()

	const email = "reset@finverse.dev"
	const oldPassword = "correct-horse-battery"
	const newPassword = "another-strong-password"

	if _, err := svc.Register(ctx, email, "", oldPassword); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := svc.Login(ctx, email, oldPassword); err != nil {
		t.Fatalf("login: %v", err)
	}

	if err := svc.RequestPasswordReset(ctx, email); err != nil {
		t.Fatalf("requesting reset: %v", err)
	}
	if mailer.lastResetToken == "" {
		t.Fatalf("expected a reset token to have been generated")
	}

	if err := svc.ResetPassword(ctx, mailer.lastResetToken, newPassword); err != nil {
		t.Fatalf("resetting password: %v", err)
	}

	// FR/NFR: a password reset must revoke every session that predates it
	// (docs/10-authentication-authorization.md §2.2) — verify every issued
	// refresh token actually ends up revoked in storage, not just that the
	// specific token used in this test happens to fail.
	if len(refreshTokens.byID) == 0 {
		t.Fatalf("expected at least one refresh token to have been issued by Login")
	}
	for id, rt := range refreshTokens.byID {
		if !rt.Revoked {
			t.Errorf("expected refresh token %s to be revoked after password reset", id)
		}
	}

	if _, err := svc.Login(ctx, email, oldPassword); err != domain.ErrInvalidCredentials {
		t.Fatalf("old password should be rejected after reset, got %v", err)
	}
	if _, err := svc.Login(ctx, email, newPassword); err != nil {
		t.Fatalf("new password should work after reset, got %v", err)
	}
}
