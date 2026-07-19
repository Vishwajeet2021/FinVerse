package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/finverse/customer-service/internal/domain"
	"gorm.io/gorm"
)

type CustomerRepository struct {
	db *gorm.DB
}

func NewCustomerRepository(db *gorm.DB) *CustomerRepository {
	return &CustomerRepository{db: db}
}

func (r *CustomerRepository) Create(ctx context.Context, c *domain.Customer) error {
	if err := r.db.WithContext(ctx).Create(toCustomerModel(c)).Error; err != nil {
		if mapped := mapUniqueViolation(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("inserting customer: %w", err)
	}
	return nil
}

func (r *CustomerRepository) GetByID(ctx context.Context, id string) (*domain.Customer, error) {
	var m customerModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrCustomerNotFound
		}
		return nil, fmt.Errorf("querying customer by id: %w", err)
	}
	return fromCustomerModel(&m), nil
}

func (r *CustomerRepository) GetByUserID(ctx context.Context, userID string) (*domain.Customer, error) {
	var m customerModel
	if err := r.db.WithContext(ctx).First(&m, "user_id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrCustomerNotFound
		}
		return nil, fmt.Errorf("querying customer by user id: %w", err)
	}
	return fromCustomerModel(&m), nil
}

// UpdateProfile writes every field back via Save, mirroring Auth Service's
// user_repo.go Update: the caller always loads the full record first
// (internal/service/customer_service.go's UpdateProfile), so Save's full-row
// write is safe here and avoids GORM's Updates(struct) skipping legitimate
// zero-value fields.
func (r *CustomerRepository) UpdateProfile(ctx context.Context, c *domain.Customer) error {
	if err := r.db.WithContext(ctx).Save(toCustomerModel(c)).Error; err != nil {
		return fmt.Errorf("updating customer profile: %w", err)
	}
	return nil
}

// UpdateKYC updates only kyc_status and kyc_tier via a column map rather
// than Save() or Updates(struct) — kyc_tier legitimately needs to be set to
// 0 (KYCTierUnverified) on rejection, which a struct-based Updates() call
// would silently skip as a zero value (the same failure mode documented in
// Auth Service's internal/repository/postgres/user_repo.go). A map-based
// Updates() call always writes exactly the columns named, zero value or not.
func (r *CustomerRepository) UpdateKYC(ctx context.Context, customerID string, status domain.KYCStatus, tier domain.KYCTier) error {
	result := r.db.WithContext(ctx).Model(&customerModel{}).Where("id = ?", customerID).Updates(map[string]any{
		"kyc_status": string(status),
		"kyc_tier":   int(tier),
		"updated_at": gorm.Expr("now()"),
	})
	if result.Error != nil {
		return fmt.Errorf("updating KYC state: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrCustomerNotFound
	}
	return nil
}
