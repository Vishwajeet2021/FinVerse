package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/finverse/auth-service/internal/domain"
	"gorm.io/gorm"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, u *domain.User) error {
	if err := r.db.WithContext(ctx).Create(toUserModel(u)).Error; err != nil {
		if mapped := mapUniqueViolation(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("inserting user: %w", err)
	}
	return nil
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	var m userModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("querying user by id: %w", err)
	}
	return fromUserModel(&m), nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var m userModel
	if err := r.db.WithContext(ctx).First(&m, "email = ?", email).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("querying user by email: %w", err)
	}
	return fromUserModel(&m), nil
}

// Update writes every field back, via Save rather than a struct-based
// Updates(). GORM's Updates(struct) silently skips zero-value fields (e.g.
// FailedLoginCount=0, LockedUntil=nil), which would break the lockout-reset
// path in internal/service/login.go — Save() is a full row write and doesn't
// have that failure mode.
func (r *UserRepository) Update(ctx context.Context, u *domain.User) error {
	if err := r.db.WithContext(ctx).Save(toUserModel(u)).Error; err != nil {
		if mapped := mapUniqueViolation(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("updating user: %w", err)
	}
	return nil
}
