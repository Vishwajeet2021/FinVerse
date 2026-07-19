package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/finverse/customer-service/internal/domain"
	"gorm.io/gorm"
)

type BeneficiaryRepository struct {
	db *gorm.DB
}

func NewBeneficiaryRepository(db *gorm.DB) *BeneficiaryRepository {
	return &BeneficiaryRepository{db: db}
}

func (r *BeneficiaryRepository) Create(ctx context.Context, b *domain.Beneficiary) error {
	if err := r.db.WithContext(ctx).Create(toBeneficiaryModel(b)).Error; err != nil {
		return fmt.Errorf("inserting beneficiary: %w", err)
	}
	return nil
}

func (r *BeneficiaryRepository) GetByID(ctx context.Context, id string) (*domain.Beneficiary, error) {
	var m beneficiaryModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrBeneficiaryNotFound
		}
		return nil, fmt.Errorf("querying beneficiary by id: %w", err)
	}
	return fromBeneficiaryModel(&m), nil
}

func (r *BeneficiaryRepository) ListByCustomerID(ctx context.Context, customerID string) ([]*domain.Beneficiary, error) {
	var models []beneficiaryModel
	if err := r.db.WithContext(ctx).Where("customer_id = ?", customerID).Order("created_at desc").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("listing beneficiaries: %w", err)
	}

	beneficiaries := make([]*domain.Beneficiary, 0, len(models))
	for i := range models {
		beneficiaries = append(beneficiaries, fromBeneficiaryModel(&models[i]))
	}
	return beneficiaries, nil
}

// MarkVerified updates only the `verified` column via a targeted Update
// call — the same "don't clobber other fields with a struct-based Updates"
// discipline as CustomerRepository.UpdateKYC, even though `verified` going
// true is never itself a zero value here; kept consistent so this file
// doesn't become the one place someone copies the wrong pattern from.
func (r *BeneficiaryRepository) MarkVerified(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Model(&beneficiaryModel{}).Where("id = ?", id).Update("verified", true)
	if result.Error != nil {
		return fmt.Errorf("marking beneficiary verified: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrBeneficiaryNotFound
	}
	return nil
}
