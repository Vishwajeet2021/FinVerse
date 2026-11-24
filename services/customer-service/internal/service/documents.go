package service

import (
	"context"
	"fmt"
	"time"

	"github.com/finverse/customer-service/internal/domain"
	"github.com/google/uuid"
)

// AddDocument implements FR-CUST-2: document binaries are saved via the
// Storage interface (never the relational DB) and only metadata is
// persisted here. customerID must already be resolved from the
// authenticated caller's own profile (docs/10-authentication-authorization.md §3.3)
// before this is called.
func (s *CustomerService) AddDocument(ctx context.Context, customerID, docType, filename string, data []byte) (*domain.KYCDocument, error) {
	if _, err := s.customers.GetByID(ctx, customerID); err != nil {
		return nil, err
	}

	ext, err := domain.ValidateDocument(docType, filename, data)
	if err != nil {
		return nil, err
	}

	// The client's filename is never used for storage: only the validated extension is kept.
	storageKey, err := s.storage.Save(ctx, customerID, "document"+ext, data)
	if err != nil {
		return nil, fmt.Errorf("saving document to storage: %w", err)
	}

	doc := &domain.KYCDocument{
		ID:         uuid.NewString(),
		CustomerID: customerID,
		DocType:    docType,
		StorageKey: storageKey,
		UploadedAt: time.Now(),
	}
	if err := s.documents.Create(ctx, doc); err != nil {
		return nil, fmt.Errorf("storing document metadata: %w", err)
	}
	return doc, nil
}

// ListDocuments returns the metadata for every document a customer has
// uploaded (no binary content — that stays behind Storage).
func (s *CustomerService) ListDocuments(ctx context.Context, customerID string) ([]*domain.KYCDocument, error) {
	return s.documents.ListByCustomerID(ctx, customerID)
}
