package service

import (
	"context"
	"fmt"

	"github.com/finverse/customer-service/internal/domain"
)

// SubmitForReview implements FR-CUST-3's PENDING -> UNDER_REVIEW transition.
// Any other starting status is rejected as an invalid transition — this is a
// real invariant (docs/06-domain-driven-design.md §4: "KYC tier can only
// move forward through the documented review states, never skip states"),
// not just documentation.
//
// When autoApproveKYC is set (docs/19-implementation-roadmap.md Phase 1 dev
// convenience), the transition doesn't stop at UNDER_REVIEW — it continues
// straight to APPROVED at tier 1, standing in for a human reviewer until
// Admin Service exists (Phase 6).
func (s *CustomerService) SubmitForReview(ctx context.Context, customerID string) (*domain.Customer, error) {
	customer, err := s.customers.GetByID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	if customer.KYCStatus != domain.KYCStatusPending {
		return nil, domain.ErrInvalidKYCTransition
	}

	nextStatus := domain.KYCStatusUnderReview
	nextTier := customer.KYCTier
	if s.autoApproveKYC {
		nextStatus = domain.KYCStatusApproved
		nextTier = domain.KYCTierBasic
	}

	if err := s.customers.UpdateKYC(ctx, customerID, nextStatus, nextTier); err != nil {
		return nil, fmt.Errorf("submitting KYC for review: %w", err)
	}

	customer.KYCStatus = nextStatus
	customer.KYCTier = nextTier
	return customer, nil
}

// ReviewKYC implements FR-CUST-3's UNDER_REVIEW -> APPROVED(tier)|REJECTED
// transition. This is the admin-only decision path
// (docs/10-authentication-authorization.md §3.3 — see
// internal/transport/http/routes.go for why this is the one route that isn't
// an ownership check). Any starting status other than UNDER_REVIEW is
// rejected, including a direct PENDING -> APPROVED/REJECTED skip.
func (s *CustomerService) ReviewKYC(ctx context.Context, customerID string, approve bool, tier domain.KYCTier) (*domain.Customer, error) {
	customer, err := s.customers.GetByID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	if customer.KYCStatus != domain.KYCStatusUnderReview {
		return nil, domain.ErrInvalidKYCTransition
	}

	nextStatus := domain.KYCStatusRejected
	nextTier := domain.KYCTierUnverified
	if approve {
		if tier != domain.KYCTierBasic && tier != domain.KYCTierFull {
			return nil, domain.ErrInvalidKYCTransition
		}
		nextStatus = domain.KYCStatusApproved
		nextTier = tier
	}

	if err := s.customers.UpdateKYC(ctx, customerID, nextStatus, nextTier); err != nil {
		return nil, fmt.Errorf("recording KYC review decision: %w", err)
	}

	customer.KYCStatus = nextStatus
	customer.KYCTier = nextTier
	return customer, nil
}
