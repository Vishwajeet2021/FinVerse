package service

import (
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/finverse/auth-service/internal/domain"
	"github.com/finverse/auth-service/internal/mfa"
)

const testPassword = "correct-horse-battery"

func testCipher(t *testing.T) *mfa.Cipher {
	t.Helper()
	c, err := mfa.NewCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// mfaHarness bundles a service with a movable clock, so tests can step through TOTP windows.
type mfaHarness struct {
	t     *testing.T
	svc   *AuthService
	users *fakeUserRepo
	rt    *fakeRefreshTokenRepo
	mfa   *fakeMFARepo
	clock time.Time
}

func newMFAHarness(t *testing.T) *mfaHarness {
	t.Helper()
	users := newFakeUserRepo()
	rt := newFakeRefreshTokenRepo()
	repo := newFakeMFARepo(users)
	svc := NewAuthService(users, rt, newFakeVerificationTokenRepo(), newFakeVerificationTokenRepo(),
		testSigner(t), &fakeMailer{}, repo, testCipher(t))
	h := &mfaHarness{t: t, svc: svc, users: users, rt: rt, mfa: repo, clock: time.Unix(1_700_000_010, 0)}
	svc.now = func() time.Time { return h.clock }
	return h
}

func (h *mfaHarness) advance(d time.Duration) { h.clock = h.clock.Add(d) }

// newUser registers a user and gives them a role.
func (h *mfaHarness) newUser(email string, role domain.Role) *domain.User {
	h.t.Helper()
	u, err := h.svc.Register(context.Background(), email, "", testPassword)
	if err != nil {
		h.t.Fatalf("register: %v", err)
	}
	h.users.byID[u.ID].Role = role
	return h.users.byID[u.ID]
}

// enrol takes a user through first-login enrolment and returns their secret and recovery codes.
func (h *mfaHarness) enrol(email string) (secret []byte, recovery []string) {
	h.t.Helper()
	ctx := context.Background()
	res, err := h.svc.Login(ctx, email, testPassword)
	if err != nil || res.Challenge == nil || res.Challenge.Purpose != domain.ChallengeEnroll {
		h.t.Fatalf("expected an ENROLL challenge, got %+v, %v", res, err)
	}
	enr, err := h.svc.BeginLoginEnrollment(ctx, res.Challenge.Token)
	if err != nil {
		h.t.Fatalf("begin enrolment: %v", err)
	}
	secret, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enr.Secret)
	if err != nil {
		h.t.Fatal(err)
	}
	out, err := h.svc.ConfirmLoginEnrollment(ctx, res.Challenge.Token, mfa.Code(secret, h.clock))
	if err != nil {
		h.t.Fatalf("confirm enrolment: %v", err)
	}
	if out.Tokens == nil || len(out.RecoveryCodes) != mfa.RecoveryCodeCount {
		h.t.Fatalf("unexpected enrolment result: %+v", out)
	}
	return secret, out.RecoveryCodes
}

func mfaClaim(t *testing.T, accessToken string) bool {
	t.Helper()
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", accessToken)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		MFA bool `json:"mfa"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	return claims.MFA
}

func TestInternalRolesNeverGetTokensWithoutMFA(t *testing.T) {
	for _, role := range []domain.Role{
		domain.RoleSupport, domain.RoleFraudAnalyst, domain.RoleAuditor, domain.RoleAdmin, domain.RoleSuperAdmin,
	} {
		t.Run(string(role), func(t *testing.T) {
			h := newMFAHarness(t)
			h.newUser("staff@finverse.dev", role)
			res, err := h.svc.Login(context.Background(), "staff@finverse.dev", testPassword)
			if err != nil {
				t.Fatal(err)
			}
			if res.Tokens != nil {
				t.Fatal("an internal role received tokens without a second factor")
			}
			if res.Challenge == nil || res.Challenge.Purpose != domain.ChallengeEnroll {
				t.Fatalf("expected an enrolment challenge, got %+v", res)
			}
			if len(h.rt.byID) != 0 {
				t.Fatal("a refresh token was stored before the second factor")
			}
		})
	}
}

func TestCustomerWithoutMFAIsUnaffected(t *testing.T) {
	h := newMFAHarness(t)
	h.newUser("cust@finverse.dev", domain.RoleCustomer)
	res, err := h.svc.Login(context.Background(), "cust@finverse.dev", testPassword)
	if err != nil || res.Tokens == nil || res.Challenge != nil {
		t.Fatalf("customer login changed: %+v, %v", res, err)
	}
	if mfaClaim(t, res.Tokens.AccessToken) {
		t.Fatal("a customer who never used MFA must not carry mfa=true")
	}
}

func TestEnrolmentThenLoginRequiresCode(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	u := h.newUser("admin@finverse.dev", domain.RoleAdmin)
	secret, _ := h.enrol("admin@finverse.dev")

	// The stored secret is ciphertext, not the key itself.
	if strings.Contains(u.MFASecretEnc, mfa.EncodeSecret(secret)) || !strings.HasPrefix(u.MFASecretEnc, "v1:") {
		t.Fatalf("secret not stored encrypted: %q", u.MFASecretEnc)
	}

	h.advance(time.Minute)
	res, err := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if err != nil || res.Challenge == nil || res.Challenge.Purpose != domain.ChallengeLogin || res.Tokens != nil {
		t.Fatalf("expected a LOGIN challenge, got %+v, %v", res, err)
	}
	tokens, method, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, mfa.Code(secret, h.clock))
	if err != nil || method != MethodTOTP {
		t.Fatalf("complete login: %v (%s)", err, method)
	}
	if !mfaClaim(t, tokens.AccessToken) {
		t.Fatal("access token after MFA must carry mfa=true")
	}
}

func TestEnrolmentSessionCarriesMFAClaim(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	h.newUser("admin@finverse.dev", domain.RoleAdmin)
	res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	enr, _ := h.svc.BeginLoginEnrollment(ctx, res.Challenge.Token)
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enr.Secret)
	out, err := h.svc.ConfirmLoginEnrollment(ctx, res.Challenge.Token, mfa.Code(secret, h.clock))
	if err != nil {
		t.Fatal(err)
	}
	if !mfaClaim(t, out.Tokens.AccessToken) {
		t.Fatal("session opened by enrolment must carry mfa=true")
	}
}

func TestTOTPCodeCannotBeReplayed(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	h.newUser("admin@finverse.dev", domain.RoleAdmin)
	secret, _ := h.enrol("admin@finverse.dev")

	// The code used to enrol belongs to a step the account has already consumed.
	h.advance(time.Second)
	res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, mfa.Code(secret, h.clock)); err != domain.ErrMFACodeInvalid {
		t.Fatalf("the enrolment code was accepted again: %v", err)
	}

	// A fresh step works once, and then not again, even inside its 90-second validity.
	h.advance(30 * time.Second)
	code := mfa.Code(secret, h.clock)
	res, _ = h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, code); err != nil {
		t.Fatalf("first use: %v", err)
	}
	res, _ = h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, code); err != domain.ErrMFACodeInvalid {
		t.Fatalf("replay accepted: %v", err)
	}
}

func TestWrongCodesExhaustTheChallengeAndTheAccount(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	h.newUser("admin@finverse.dev", domain.RoleAdmin)
	secret, _ := h.enrol("admin@finverse.dev")
	h.advance(time.Minute)
	good := mfa.Code(secret, h.clock)
	bad := "000000"
	if bad == good {
		bad = "111111"
	}

	// Five wrong codes on one challenge: the sixth attempt is refused even with the right code.
	res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	for i := 0; i < domain.MFAChallengeMaxAttempts-1; i++ {
		if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, bad); err != domain.ErrMFACodeInvalid {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, bad); err != domain.ErrMFACodeInvalid &&
		err != domain.ErrAccountLocked {
		t.Fatalf("final wrong attempt: %v", err)
	}
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, good); err == nil {
		t.Fatal("an exhausted challenge accepted the right code")
	}

	// Opening a new challenge must not hand back a fresh guess budget: the account is now locked.
	if _, err := h.svc.Login(ctx, "admin@finverse.dev", testPassword); err != domain.ErrAccountLocked {
		t.Fatalf("expected the account to be locked after repeated wrong codes, got %v", err)
	}
}

func TestPasswordSuccessDoesNotResetFailuresForMFAAccounts(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	h.newUser("admin@finverse.dev", domain.RoleAdmin)
	secret, _ := h.enrol("admin@finverse.dev")
	h.advance(time.Minute)

	// Two wrong codes on each of two challenges: four failures in total.
	for round := 0; round < 2; round++ {
		res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
		for i := 0; i < 2; i++ {
			_, _, _ = h.svc.CompleteLogin(ctx, res.Challenge.Token, "000000")
		}
	}
	u, _ := h.users.GetByEmail(ctx, "admin@finverse.dev")
	if u.FailedLoginCount != 4 {
		t.Fatalf("failures were reset by a correct password: count=%d, want 4", u.FailedLoginCount)
	}

	// A full success finally clears them.
	h.advance(30 * time.Second)
	res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, mfa.Code(secret, h.clock)); err != nil {
		t.Fatal(err)
	}
	u, _ = h.users.GetByEmail(ctx, "admin@finverse.dev")
	if u.FailedLoginCount != 0 {
		t.Fatalf("a successful sign-in must clear failures, count=%d", u.FailedLoginCount)
	}
}

func TestRecoveryCodeWorksOnce(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	h.newUser("admin@finverse.dev", domain.RoleAdmin)
	_, recovery := h.enrol("admin@finverse.dev")
	h.advance(time.Minute)

	res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	tokens, method, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, strings.ToLower(recovery[0]))
	if err != nil || method != MethodRecovery || !mfaClaim(t, tokens.AccessToken) {
		t.Fatalf("recovery login: %v (%s)", err, method)
	}
	res, _ = h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, recovery[0]); err != domain.ErrMFACodeInvalid {
		t.Fatalf("a recovery code was accepted twice: %v", err)
	}
	// A different code still works.
	res, _ = h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, recovery[1]); err != nil {
		t.Fatalf("second recovery code: %v", err)
	}
}

func TestChallengesAreSingleUseExpiringAndPurposeBound(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	h.newUser("admin@finverse.dev", domain.RoleAdmin)
	secret, _ := h.enrol("admin@finverse.dev")
	h.advance(time.Minute)

	// Used once, then dead.
	res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, mfa.Code(secret, h.clock)); err != nil {
		t.Fatal(err)
	}
	h.advance(30 * time.Second)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, mfa.Code(secret, h.clock)); err != domain.ErrMFAChallengeInvalid {
		t.Fatalf("consumed challenge reused: %v", err)
	}

	// Expires.
	res, _ = h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	h.advance(domain.MFAChallengeTTL + time.Second)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, mfa.Code(secret, h.clock)); err != domain.ErrMFAChallengeInvalid {
		t.Fatalf("expired challenge accepted: %v", err)
	}

	// A LOGIN challenge cannot be used to (re)enrol; an unknown token is just invalid.
	res, _ = h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if _, err := h.svc.BeginLoginEnrollment(ctx, res.Challenge.Token); err != domain.ErrMFAChallengeInvalid {
		t.Fatalf("LOGIN challenge used for enrolment: %v", err)
	}
	if _, _, err := h.svc.CompleteLogin(ctx, "no-such-token", "123456"); err != domain.ErrMFAChallengeInvalid {
		t.Fatalf("unknown challenge: %v", err)
	}
}

func TestEnrolChallengeCannotLogIn(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	h.newUser("admin@finverse.dev", domain.RoleAdmin)
	res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	if _, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, "123456"); err != domain.ErrMFAChallengeInvalid {
		t.Fatalf("ENROLL challenge accepted by CompleteLogin: %v", err)
	}
}

func TestEnrolmentRequiresAValidCodeAndCannotBeRepeated(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	h.newUser("admin@finverse.dev", domain.RoleAdmin)
	res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)

	if _, err := h.svc.ConfirmLoginEnrollment(ctx, res.Challenge.Token, "123456"); err != domain.ErrMFANotStarted {
		t.Fatalf("confirm before begin: %v", err)
	}
	if _, err := h.svc.BeginLoginEnrollment(ctx, res.Challenge.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ConfirmLoginEnrollment(ctx, res.Challenge.Token, "000000"); err != domain.ErrMFACodeInvalid {
		t.Fatalf("wrong confirm code: %v", err)
	}
	if u := h.users.byEmail["admin@finverse.dev"]; u.MFAEnabled {
		t.Fatal("MFA enabled without a valid code")
	}

	// Once enabled, a live secret cannot be replaced by starting again.
	h2 := newMFAHarness(t)
	h2.newUser("a@finverse.dev", domain.RoleAdmin)
	h2.enrol("a@finverse.dev")
	if _, err := h2.svc.BeginSelfEnrollment(ctx, h2.users.byEmail["a@finverse.dev"].ID, testPassword); err != domain.ErrMFAAlreadyEnabled {
		t.Fatalf("re-enrolment over a live secret: %v", err)
	}
}

func TestRefreshKeepsMFAAndRefusesUnverifiedInternalSessions(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	h.newUser("admin@finverse.dev", domain.RoleAdmin)
	secret, _ := h.enrol("admin@finverse.dev")
	h.advance(time.Minute)

	res, _ := h.svc.Login(ctx, "admin@finverse.dev", testPassword)
	tokens, _, err := h.svc.CompleteLogin(ctx, res.Challenge.Token, mfa.Code(secret, h.clock))
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := h.svc.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if !mfaClaim(t, refreshed.AccessToken) {
		t.Fatal("refresh dropped the mfa claim")
	}

	// A session from before MFA existed: an internal role, a refresh token with mfa_verified=false.
	legacy := &domain.RefreshToken{
		ID: "legacy", UserID: h.users.byEmail["admin@finverse.dev"].ID, TokenHash: hashToken("legacy-token"),
		FamilyID: "legacy-family", ExpiresAt: h.clock.Add(time.Hour), CreatedAt: h.clock,
	}
	_ = h.rt.Create(ctx, legacy)
	if _, err := h.svc.Refresh(ctx, "legacy-token"); err != domain.ErrMFARequired {
		t.Fatalf("an internal session without MFA was renewed: %v", err)
	}
	if !h.rt.byID["legacy"].Revoked {
		t.Fatal("the unverified session was not revoked")
	}
}

func TestSelfEnrolmentNeedsThePassword(t *testing.T) {
	h := newMFAHarness(t)
	ctx := context.Background()
	u := h.newUser("cust@finverse.dev", domain.RoleCustomer)

	if _, err := h.svc.BeginSelfEnrollment(ctx, u.ID, "not-the-password"); err != domain.ErrInvalidCredentials {
		t.Fatalf("wrong password: %v", err)
	}
	enr, err := h.svc.BeginSelfEnrollment(ctx, u.ID, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enr.Secret)
	codes, err := h.svc.ConfirmSelfEnrollment(ctx, u.ID, mfa.Code(secret, h.clock))
	if err != nil || len(codes) != mfa.RecoveryCodeCount {
		t.Fatalf("confirm: %v (%d codes)", err, len(codes))
	}

	// From now on this customer's login asks for a code.
	h.advance(time.Minute)
	res, err := h.svc.Login(ctx, "cust@finverse.dev", testPassword)
	if err != nil || res.Challenge == nil || res.Challenge.Purpose != domain.ChallengeLogin {
		t.Fatalf("customer with MFA should be challenged: %+v, %v", res, err)
	}
}

func TestStaleUserWriteDoesNotRollBackReplayProtection(t *testing.T) {
	// Mirrors the real repository: Update must not touch mfa_last_step.
	h := newMFAHarness(t)
	ctx := context.Background()
	u := h.newUser("admin@finverse.dev", domain.RoleAdmin)
	secret, _ := h.enrol("admin@finverse.dev")
	stale, _ := h.users.GetByID(ctx, u.ID)
	stale.MFALastStep = 0
	stale.MFAEnabled = false
	if err := h.users.Update(ctx, stale); err != nil {
		t.Fatal(err)
	}
	now, _ := h.users.GetByID(ctx, u.ID)
	if !now.MFAEnabled || now.MFALastStep != mfa.StepAt(h.clock) {
		t.Fatalf("a whole-row save changed the MFA columns: %+v", now)
	}
	_ = secret
}
