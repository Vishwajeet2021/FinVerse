package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/auth-service/internal/domain"
	"gorm.io/gorm"
)

// VerificationTokenRepository backs both email_verification_tokens and
// password_reset_tokens (docs/08-database-design.md §7) — the table is
// injected at construction so the identical query logic isn't duplicated
// per table (see cmd/server/main.go for the two instances).
type VerificationTokenRepository struct {
	db    *gorm.DB
	table string
}

func NewVerificationTokenRepository(db *gorm.DB, table string) *VerificationTokenRepository {
	return &VerificationTokenRepository{db: db, table: table}
}

func (r *VerificationTokenRepository) Create(ctx context.Context, t *domain.VerificationToken) error {
	if err := r.db.WithContext(ctx).Table(r.table).Create(toVerificationTokenModel(t)).Error; err != nil {
		return fmt.Errorf("inserting verification token into %s: %w", r.table, err)
	}
	return nil
}

func (r *VerificationTokenRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.VerificationToken, error) {
	var m verificationTokenModel
	if err := r.db.WithContext(ctx).Table(r.table).First(&m, "token_hash = ?", tokenHash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTokenInvalidOrExpired
		}
		return nil, fmt.Errorf("querying verification token in %s: %w", r.table, err)
	}
	return fromVerificationTokenModel(&m), nil
}

func (r *VerificationTokenRepository) MarkUsed(ctx context.Context, id string) error {
	now := time.Now()
	res := r.db.WithContext(ctx).Table(r.table).Where("id = ?", id).Update("used_at", now)
	if res.Error != nil {
		return fmt.Errorf("marking verification token used in %s: %w", r.table, res.Error)
	}
	return nil
}
