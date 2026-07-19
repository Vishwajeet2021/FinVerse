package postgres

import (
	"context"
	"fmt"

	"github.com/finverse/customer-service/internal/domain"
	"gorm.io/gorm"
)

type DocumentRepository struct {
	db *gorm.DB
}

func NewDocumentRepository(db *gorm.DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

func (r *DocumentRepository) Create(ctx context.Context, d *domain.KYCDocument) error {
	if err := r.db.WithContext(ctx).Create(toDocumentModel(d)).Error; err != nil {
		return fmt.Errorf("inserting document metadata: %w", err)
	}
	return nil
}

func (r *DocumentRepository) ListByCustomerID(ctx context.Context, customerID string) ([]*domain.KYCDocument, error) {
	var models []kycDocumentModel
	if err := r.db.WithContext(ctx).Where("customer_id = ?", customerID).Order("uploaded_at desc").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("listing documents: %w", err)
	}

	docs := make([]*domain.KYCDocument, 0, len(models))
	for i := range models {
		docs = append(docs, fromDocumentModel(&models[i]))
	}
	return docs, nil
}
