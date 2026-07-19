package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/finverse/auth-service/internal/domain"
	"gorm.io/gorm"
)

type RefreshTokenRepository struct {
	db *gorm.DB
}

func NewRefreshTokenRepository(db *gorm.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, t *domain.RefreshToken) error {
	if err := r.db.WithContext(ctx).Create(toRefreshTokenModel(t)).Error; err != nil {
		return fmt.Errorf("inserting refresh token: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	var m refreshTokenModel
	if err := r.db.WithContext(ctx).First(&m, "token_hash = ?", tokenHash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTokenInvalidOrExpired
		}
		return nil, fmt.Errorf("querying refresh token: %w", err)
	}
	return fromRefreshTokenModel(&m), nil
}

func (r *RefreshTokenRepository) Revoke(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Model(&refreshTokenModel{}).Where("id = ?", id).Update("revoked", true)
	if res.Error != nil {
		return fmt.Errorf("revoking refresh token: %w", res.Error)
	}
	return nil
}

func (r *RefreshTokenRepository) RevokeFamily(ctx context.Context, familyID string) error {
	res := r.db.WithContext(ctx).Model(&refreshTokenModel{}).Where("family_id = ?", familyID).Update("revoked", true)
	if res.Error != nil {
		return fmt.Errorf("revoking refresh token family: %w", res.Error)
	}
	return nil
}

func (r *RefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID string) error {
	res := r.db.WithContext(ctx).Model(&refreshTokenModel{}).Where("user_id = ?", userID).Update("revoked", true)
	if res.Error != nil {
		return fmt.Errorf("revoking all refresh tokens for user: %w", res.Error)
	}
	return nil
}
