package service

import (
	"context"
	"time"

	"github.com/finverse/auth-service/internal/domain"
)

// In-memory fakes for the repository/mailer interfaces, used by the unit
// tests in this package so business logic is verified without a real
// database — per docs/17-testing-strategy.md §2 ("never mock the thing
// you're testing"; these are the boundary, not the logic under test).

type fakeUserRepo struct {
	byID    map[string]*domain.User
	byEmail map[string]*domain.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{byID: map[string]*domain.User{}, byEmail: map[string]*domain.User{}}
}

func (f *fakeUserRepo) Create(_ context.Context, u *domain.User) error {
	if _, exists := f.byEmail[u.Email]; exists {
		return domain.ErrEmailAlreadyRegistered
	}
	cp := *u
	f.byID[u.ID] = &cp
	f.byEmail[u.Email] = &cp
	return nil
}

func (f *fakeUserRepo) GetByID(_ context.Context, id string) (*domain.User, error) {
	if u, ok := f.byID[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, domain.ErrUserNotFound
}

func (f *fakeUserRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	if u, ok := f.byEmail[email]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, domain.ErrUserNotFound
}

func (f *fakeUserRepo) Update(_ context.Context, u *domain.User) error {
	cp := *u
	// Mirror the real repository: the MFA columns are read-only for Update (see models.go), so a
	// stale struct cannot overwrite them.
	if cur, ok := f.byID[u.ID]; ok {
		cp.MFAEnabled, cp.MFASecretEnc, cp.MFALastStep, cp.MFAEnrolledAt =
			cur.MFAEnabled, cur.MFASecretEnc, cur.MFALastStep, cur.MFAEnrolledAt
	}
	f.byID[u.ID] = &cp
	f.byEmail[u.Email] = &cp
	return nil
}

type fakeRefreshTokenRepo struct {
	byID   map[string]*domain.RefreshToken
	byHash map[string]*domain.RefreshToken
}

func newFakeRefreshTokenRepo() *fakeRefreshTokenRepo {
	return &fakeRefreshTokenRepo{byID: map[string]*domain.RefreshToken{}, byHash: map[string]*domain.RefreshToken{}}
}

func (f *fakeRefreshTokenRepo) Create(_ context.Context, t *domain.RefreshToken) error {
	cp := *t
	f.byID[t.ID] = &cp
	f.byHash[t.TokenHash] = &cp
	return nil
}

func (f *fakeRefreshTokenRepo) GetByTokenHash(_ context.Context, hash string) (*domain.RefreshToken, error) {
	if t, ok := f.byHash[hash]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, domain.ErrTokenInvalidOrExpired
}

func (f *fakeRefreshTokenRepo) Revoke(_ context.Context, id string) error {
	if t, ok := f.byID[id]; ok {
		t.Revoked = true
	}
	return nil
}

func (f *fakeRefreshTokenRepo) RevokeFamily(_ context.Context, familyID string) error {
	for _, t := range f.byID {
		if t.FamilyID == familyID {
			t.Revoked = true
		}
	}
	return nil
}

func (f *fakeRefreshTokenRepo) RevokeAllForUser(_ context.Context, userID string) error {
	for _, t := range f.byID {
		if t.UserID == userID {
			t.Revoked = true
		}
	}
	return nil
}

type fakeVerificationTokenRepo struct {
	byID   map[string]*domain.VerificationToken
	byHash map[string]*domain.VerificationToken
}

func newFakeVerificationTokenRepo() *fakeVerificationTokenRepo {
	return &fakeVerificationTokenRepo{byID: map[string]*domain.VerificationToken{}, byHash: map[string]*domain.VerificationToken{}}
}

func (f *fakeVerificationTokenRepo) Create(_ context.Context, t *domain.VerificationToken) error {
	cp := *t
	f.byID[t.ID] = &cp
	f.byHash[t.TokenHash] = &cp
	return nil
}

func (f *fakeVerificationTokenRepo) GetByTokenHash(_ context.Context, hash string) (*domain.VerificationToken, error) {
	if t, ok := f.byHash[hash]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, domain.ErrTokenInvalidOrExpired
}

func (f *fakeVerificationTokenRepo) MarkUsed(_ context.Context, id string) error {
	if t, ok := f.byID[id]; ok {
		now := t.CreatedAt
		t.UsedAt = &now
	}
	return nil
}

type fakeMailer struct {
	verificationSentTo []string
	resetSentTo        []string

	// lastVerificationToken/lastResetToken capture the raw token a real
	// mailer would have put in the email link — the fake exposes it
	// directly so tests can drive the full verify/reset flow without
	// needing to intercept an actual email.
	lastVerificationToken string
	lastResetToken        string
}

func (m *fakeMailer) SendVerificationEmail(_ context.Context, toEmail, token string) error {
	m.verificationSentTo = append(m.verificationSentTo, toEmail)
	m.lastVerificationToken = token
	return nil
}

func (m *fakeMailer) SendPasswordResetEmail(_ context.Context, toEmail, token string) error {
	m.resetSentTo = append(m.resetSentTo, toEmail)
	m.lastResetToken = token
	return nil
}

// fakeMFARepo reproduces the conditional-write semantics of the real MFARepository, acting on the
// same user records the fake user repository holds.
type fakeMFARepo struct {
	users      *fakeUserRepo
	recovery   map[string]map[string]bool // userID -> hash -> used
	challenges map[string]*domain.MFAChallenge
	byHash     map[string]*domain.MFAChallenge
}

func newFakeMFARepo(users *fakeUserRepo) *fakeMFARepo {
	return &fakeMFARepo{
		users:      users,
		recovery:   map[string]map[string]bool{},
		challenges: map[string]*domain.MFAChallenge{},
		byHash:     map[string]*domain.MFAChallenge{},
	}
}

func (f *fakeMFARepo) user(id string) *domain.User { return f.users.byID[id] }

func (f *fakeMFARepo) sync(u *domain.User) { f.users.byEmail[u.Email] = u }

func (f *fakeMFARepo) SetPendingSecret(_ context.Context, userID, enc string) error {
	u := f.user(userID)
	if u.MFAEnabled {
		return domain.ErrMFAAlreadyEnabled
	}
	u.MFASecretEnc = enc
	f.sync(u)
	return nil
}

func (f *fakeMFARepo) Activate(_ context.Context, userID string, step int64, hashes []string, now time.Time) error {
	u := f.user(userID)
	if u.MFAEnabled || u.MFASecretEnc == "" {
		return domain.ErrMFAAlreadyEnabled
	}
	u.MFAEnabled, u.MFALastStep, u.MFAEnrolledAt = true, step, &now
	f.sync(u)
	f.recovery[userID] = map[string]bool{}
	for _, h := range hashes {
		f.recovery[userID][h] = false
	}
	return nil
}

func (f *fakeMFARepo) AdvanceStep(_ context.Context, userID string, step int64) (bool, error) {
	u := f.user(userID)
	if !u.MFAEnabled || u.MFALastStep >= step {
		return false, nil
	}
	u.MFALastStep = step
	f.sync(u)
	return true, nil
}

func (f *fakeMFARepo) ConsumeRecoveryCode(_ context.Context, userID, hash string) (bool, error) {
	used, exists := f.recovery[userID][hash]
	if !exists || used {
		return false, nil
	}
	f.recovery[userID][hash] = true
	return true, nil
}

func (f *fakeMFARepo) CreateChallenge(_ context.Context, c *domain.MFAChallenge) error {
	cp := *c
	f.challenges[c.ID] = &cp
	f.byHash[c.TokenHash] = &cp
	return nil
}

func (f *fakeMFARepo) GetChallengeByHash(_ context.Context, hash string) (*domain.MFAChallenge, error) {
	if c, ok := f.byHash[hash]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, domain.ErrMFAChallengeInvalid
}

func (f *fakeMFARepo) RecordChallengeAttempt(_ context.Context, id string) (int, error) {
	f.challenges[id].Attempts++
	return f.challenges[id].Attempts, nil
}

func (f *fakeMFARepo) ConsumeChallenge(_ context.Context, id string) (bool, error) {
	c := f.challenges[id]
	if c.ConsumedAt != nil {
		return false, nil
	}
	now := time.Now()
	c.ConsumedAt = &now
	return true, nil
}
