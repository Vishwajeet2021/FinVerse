package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/finverse/customer-service/internal/domain"
	"github.com/google/uuid"
)

// AddBeneficiary implements FR-CUST-4: newly added beneficiaries always
// start unverified, requiring confirmation before first transfer — that
// confirmation flow itself belongs to Payment Service (docs/06-domain-driven-design.md §2.2);
// Customer Service only owns the verification flag's state.
func (s *CustomerService) AddBeneficiary(ctx context.Context, customerID, name, accountRef string) (*domain.Beneficiary, error) {
	name = strings.TrimSpace(name)
	if err := domain.ValidateFullName(name); err != nil {
		return nil, err
	}
	if _, err := s.customers.GetByID(ctx, customerID); err != nil {
		return nil, err
	}

	beneficiary := &domain.Beneficiary{
		ID:         uuid.NewString(),
		CustomerID: customerID,
		Name:       name,
		AccountRef: accountRef,
		Verified:   false,
		CreatedAt:  time.Now(),
	}
	if err := s.beneficiaries.Create(ctx, beneficiary); err != nil {
		return nil, fmt.Errorf("adding beneficiary: %w", err)
	}
	return beneficiary, nil
}

// ListBeneficiaries implements the read side of FR-CUST-4.
func (s *CustomerService) ListBeneficiaries(ctx context.Context, customerID string) ([]*domain.Beneficiary, error) {
	return s.beneficiaries.ListByCustomerID(ctx, customerID)
}

// VerifyBeneficiary transitions a beneficiary from unverified to verified.
// Phase 1 has no independent confirmation channel (e.g. a micro-deposit or
// OTP step-up) wired in yet, so this is exposed as a direct service method
// for the transport layer / future Payment Service integration to call once
// that confirmation mechanism exists.
func (s *CustomerService) VerifyBeneficiary(ctx context.Context, customerID, beneficiaryID string) error {
	beneficiary, err := s.beneficiaries.GetByID(ctx, beneficiaryID)
	if err != nil {
		return err
	}
	if beneficiary.CustomerID != customerID {
		return domain.ErrBeneficiaryNotFound
	}
	return s.beneficiaries.MarkVerified(ctx, beneficiaryID)
}
