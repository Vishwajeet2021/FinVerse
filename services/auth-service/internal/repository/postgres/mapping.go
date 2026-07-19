package postgres

import "github.com/finverse/auth-service/internal/domain"

func toUserModel(u *domain.User) *userModel {
	return &userModel{
		ID:               u.ID,
		Email:            u.Email,
		Phone:            u.Phone,
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
		Phone:            m.Phone,
		PasswordHash:     m.PasswordHash,
		Role:             domain.Role(m.Role),
		EmailVerified:    m.EmailVerified,
		Status:           domain.UserStatus(m.Status),
		FailedLoginCount: m.FailedLoginCount,
		LockedUntil:      m.LockedUntil,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}

func toRefreshTokenModel(t *domain.RefreshToken) *refreshTokenModel {
	return &refreshTokenModel{
		ID:        t.ID,
		UserID:    t.UserID,
		TokenHash: t.TokenHash,
		FamilyID:  t.FamilyID,
		Revoked:   t.Revoked,
		ExpiresAt: t.ExpiresAt,
		CreatedAt: t.CreatedAt,
	}
}

func fromRefreshTokenModel(m *refreshTokenModel) *domain.RefreshToken {
	return &domain.RefreshToken{
		ID:        m.ID,
		UserID:    m.UserID,
		TokenHash: m.TokenHash,
		FamilyID:  m.FamilyID,
		Revoked:   m.Revoked,
		ExpiresAt: m.ExpiresAt,
		CreatedAt: m.CreatedAt,
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
