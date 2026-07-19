package service

import (
	"context"

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
