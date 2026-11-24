package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/finverse/customer-service/internal/domain"
	"github.com/google/uuid"
)

// CustomerService implements the FR-CUST-* requirements in
// docs/03-functional-requirements.md §3. It depends only on the interfaces
// in interfaces.go, never on a concrete database or storage backend.
type CustomerService struct {
	customers     CustomerRepository
	documents     DocumentRepository
	beneficiaries BeneficiaryRepository
	storage       Storage
	pan           *PANCipher

	// autoApproveKYC implements the Phase 1 dev convenience documented in
	// docs/19-implementation-roadmap.md Phase 1 ("no real document review
	// logic yet — auto-approve in dev"): SubmitForReview immediately
	// transitions PENDING -> UNDER_REVIEW -> APPROVED (tier 1) instead of
	// waiting for a human reviewer, since there is no Admin Service yet
	// (that lands in Phase 6, per docs/07-microservice-specifications.md §13).
	autoApproveKYC bool
}

func NewCustomerService(
	customers CustomerRepository,
	documents DocumentRepository,
	beneficiaries BeneficiaryRepository,
	storage Storage,
	pan *PANCipher,
	autoApproveKYC bool,
) *CustomerService {
	return &CustomerService{
		customers:      customers,
		documents:      documents,
		beneficiaries:  beneficiaries,
		storage:        storage,
		pan:            pan,
		autoApproveKYC: autoApproveKYC,
	}
}

// CreateProfile implements FR-CUST-1: one profile per user_id. PAN is
// encrypted at rest (AES-256-GCM) with a masked display value computed once
// at write time (docs/08-database-design.md §6, docs/11-security-standards.md §4)
// so nothing downstream ever needs to decrypt it just to show or log it
// (FR-CUST-7).
func (s *CustomerService) CreateProfile(ctx context.Context, userID, fullName string, dob time.Time, pan string) (*domain.Customer, error) {
	if existing, err := s.customers.GetByUserID(ctx, userID); err == nil && existing != nil {
		return nil, domain.ErrProfileAlreadyExists
	} else if err != nil && err != domain.ErrCustomerNotFound {
		return nil, fmt.Errorf("checking existing profile: %w", err)
	}

	pan = strings.ToUpper(strings.TrimSpace(pan))
	fullName = strings.TrimSpace(fullName)
	if err := domain.ValidateFullName(fullName); err != nil {
		return nil, err
	}
	if err := domain.ValidatePAN(pan); err != nil {
		return nil, err
	}
	if err := domain.ValidateDOB(dob, time.Now()); err != nil {
		return nil, err
	}

	encrypted, err := s.pan.Encrypt(pan)
	if err != nil {
		return nil, fmt.Errorf("encrypting PAN: %w", err)
	}

	now := time.Now()
	customer := &domain.Customer{
		ID:           uuid.NewString(),
		UserID:       userID,
		FullName:     fullName,
		DOB:          dob,
		PANEncrypted: encrypted,
		PANMasked:    MaskPAN(pan),
		KYCTier:      domain.KYCTierUnverified,
		KYCStatus:    domain.KYCStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.customers.Create(ctx, customer); err != nil {
		return nil, err // already a domain error (ErrProfileAlreadyExists) or wrapped infra error
	}
	return customer, nil
}

// GetProfile implements the read side of FR-CUST-5.
func (s *CustomerService) GetProfile(ctx context.Context, customerID string) (*domain.Customer, error) {
	return s.customers.GetByID(ctx, customerID)
}

// GetProfileByUserID resolves a customer via the authenticated user's ID
// (JWT claims.Subject), which is how transport/http enforces ownership
// structurally for every `/me` route (docs/10-authentication-authorization.md §3.3)
// rather than trusting a client-supplied customer ID.
func (s *CustomerService) GetProfileByUserID(ctx context.Context, userID string) (*domain.Customer, error) {
	return s.customers.GetByUserID(ctx, userID)
}

// UpdateProfile implements FR-CUST-5: a customer may update fields that
// don't affect KYC tier without re-review. KYC-affecting fields (PAN, DOB,
// full_name — identity fields the original KYC decision was made against)
// are deliberately not accepted here; only FullName is currently mutable
// this way, since Phase 1's profile has no separate contact-preference
// fields yet (docs/06-domain-driven-design.md §2.2 lists no such field for
// this phase).
func (s *CustomerService) UpdateProfile(ctx context.Context, customerID, fullName string) (*domain.Customer, error) {
	fullName = strings.TrimSpace(fullName)
	if err := domain.ValidateFullName(fullName); err != nil {
		return nil, err
	}
	customer, err := s.customers.GetByID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	customer.FullName = fullName
	customer.UpdatedAt = time.Now()
	if err := s.customers.UpdateProfile(ctx, customer); err != nil {
		return nil, fmt.Errorf("updating profile: %w", err)
	}
	return customer, nil
}
