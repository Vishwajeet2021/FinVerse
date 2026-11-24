package postgres

import "github.com/finverse/auth-service/internal/domain"

// emptyToNil/nilToEmpty convert between the domain's plain string (where ""
// means "not provided") and the model's *string (where nil means SQL NULL) —
// see models.go's comment on userModel.Phone for why this distinction matters.
func emptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nilToEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toUserModel(u *domain.User) *userModel {
	return &userModel{
		ID:               u.ID,
		Email:            u.Email,
		Phone:            emptyToNil(u.Phone),
		PasswordHash:     u.PasswordHash,
		Role:             string(u.Role),
		EmailVerified:    u.EmailVerified,
		Status:           string(u.Status),
		FailedLoginCount: u.FailedLoginCount,
		LockedUntil:      u.LockedUntil,
		CreatedAt:        u.CreatedAt,
		UpdatedAt:        u.UpdatedAt,
	}
}

func fromUserModel(m *userModel) *domain.User {
	return &domain.User{
		ID:               m.ID,
		Email:            m.Email,
		Phone:            nilToEmpty(m.Phone),
		PasswordHash:     m.PasswordHash,
		Role:             domain.Role(m.Role),
		EmailVerified:    m.EmailVerified,
		Status:           domain.UserStatus(m.Status),
		FailedLoginCount: m.FailedLoginCount,
		LockedUntil:      m.LockedUntil,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
		MFAEnabled:       m.MFAEnabled,
		MFASecretEnc:     nilToEmpty(m.MFASecretEnc),
		MFALastStep:      m.MFALastStep,
		MFAEnrolledAt:    m.MFAEnrolledAt,
	}
}

func toRefreshTokenModel(t *domain.RefreshToken) *refreshTokenModel {
	return &refreshTokenModel{
		ID:          t.ID,
		UserID:      t.UserID,
		TokenHash:   t.TokenHash,
		FamilyID:    t.FamilyID,
		Revoked:     t.Revoked,
		MFAVerified: t.MFAVerified,
		ExpiresAt:   t.ExpiresAt,
		CreatedAt:   t.CreatedAt,
	}
}

func fromRefreshTokenModel(m *refreshTokenModel) *domain.RefreshToken {
	return &domain.RefreshToken{
		ID:          m.ID,
		UserID:      m.UserID,
		TokenHash:   m.TokenHash,
		FamilyID:    m.FamilyID,
		Revoked:     m.Revoked,
		MFAVerified: m.MFAVerified,
		ExpiresAt:   m.ExpiresAt,
		CreatedAt:   m.CreatedAt,
	}
}

func toVerificationTokenModel(t *domain.VerificationToken) *verificationTokenModel {
	return &verificationTokenModel{
		ID:        t.ID,
		UserID:    t.UserID,
		TokenHash: t.TokenHash,
		ExpiresAt: t.ExpiresAt,
		UsedAt:    t.UsedAt,
		CreatedAt: t.CreatedAt,
	}
}

func fromVerificationTokenModel(m *verificationTokenModel) *domain.VerificationToken {
	return &domain.VerificationToken{
		ID:        m.ID,
		UserID:    m.UserID,
		TokenHash: m.TokenHash,
		ExpiresAt: m.ExpiresAt,
		UsedAt:    m.UsedAt,
		CreatedAt: m.CreatedAt,
	}
}
