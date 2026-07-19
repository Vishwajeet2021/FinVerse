// Package service holds Customer Service's business/use-case logic. It
// depends only on domain types and the repository/storage interfaces below —
// never on Gin or GORM directly — per docs/12-coding-standards.md §1.1.
package service

import (
	"context"

	"github.com/finverse/customer-service/internal/domain"
)

// CustomerRepository is implemented by internal/repository/postgres.
type CustomerRepository interface {
	Create(ctx context.Context, c *domain.Customer) error
	GetByID(ctx context.Context, id string) (*domain.Customer, error)
	GetByUserID(ctx context.Context, userID string) (*domain.Customer, error)
	UpdateProfile(ctx context.Context, c *domain.Customer) error
	UpdateKYC(ctx context.Context, customerID string, status domain.KYCStatus, tier domain.KYCTier) error
}

// DocumentRepository is implemented by internal/repository/postgres.
type DocumentRepository interface {
	Create(ctx context.Context, d *domain.KYCDocument) error
	ListByCustomerID(ctx context.Context, customerID string) ([]*domain.KYCDocument, error)
}

// BeneficiaryRepository is implemented by internal/repository/postgres.
type BeneficiaryRepository interface {
	Create(ctx context.Context, b *domain.Beneficiary) error
	GetByID(ctx context.Context, id string) (*domain.Beneficiary, error)
	ListByCustomerID(ctx context.Context, customerID string) ([]*domain.Beneficiary, error)
	MarkVerified(ctx context.Context, id string) error
}

// Storage abstracts where KYC document bytes physically live. docs/07-microservice-specifications.md
// §3 names S3 as the eventual store, but that's an AWS-phase concern
// (docs/19-implementation-roadmap.md Phase 4) — this interface boundary is
// what matters now, so swapping in an S3 implementation later doesn't touch
// any caller. LocalDiskStorage (internal/repository/localstorage) is the
// Phase 1 implementation.
type Storage interface {
	Save(ctx context.Context, customerID, filename string, data []byte) (storageKey string, err error)
}
