package service

import (
	"context"
	"testing"

	"github.com/finverse/customer-service/internal/domain"
)

// TestKYCStateMachine_ValidTransitions exercises the two legal end-to-end
// paths in FR-CUST-3: PENDING -> UNDER_REVIEW -> APPROVED(tier) and
// PENDING -> UNDER_REVIEW -> REJECTED. autoApproveKYC is off here so the
// state machine's own transition logic is what's under test, not the
// Phase 1 dev shortcut (that's TestSubmitForReview_AutoApproveInDev below).
func TestKYCStateMachine_ValidTransitions(t *testing.T) {
	cases := []struct {
		name      string
		approve   bool
		tier      domain.KYCTier
		wantTier  domain.KYCTier
		wantState domain.KYCStatus
	}{
		{name: "approved tier 1", approve: true, tier: domain.KYCTierBasic, wantTier: domain.KYCTierBasic, wantState: domain.KYCStatusApproved},
		{name: "approved tier 2", approve: true, tier: domain.KYCTierFull, wantTier: domain.KYCTierFull, wantState: domain.KYCStatusApproved},
		{name: "rejected", approve: false, wantTier: domain.KYCTierUnverified, wantState: domain.KYCStatusRejected},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newTestCustomerService(t, false)
			ctx := context.Background()

			customer, err := svc.CreateProfile(ctx, "user-"+tc.name, "Jane Doe", testDOB, "ABCDE1234F")
			if err != nil {
				t.Fatalf("creating profile: %v", err)
			}

			afterSubmit, err := svc.SubmitForReview(ctx, customer.ID)
			if err != nil {
				t.Fatalf("submitting for review: %v", err)
			}
			if afterSubmit.KYCStatus != domain.KYCStatusUnderReview {
				t.Fatalf("got status %v after submit, want UNDER_REVIEW", afterSubmit.KYCStatus)
			}

			reviewed, err := svc.ReviewKYC(ctx, customer.ID, tc.approve, tc.tier)
			if err != nil {
				t.Fatalf("unexpected error reviewing KYC: %v", err)
			}
			if reviewed.KYCStatus != tc.wantState {
				t.Errorf("got status %v, want %v", reviewed.KYCStatus, tc.wantState)
			}
			if reviewed.KYCTier != tc.wantTier {
				t.Errorf("got tier %v, want %v", reviewed.KYCTier, tc.wantTier)
			}
		})
	}
}

// TestKYCStateMachine_RejectsSkippedStates is the real invariant this state
// machine exists to protect (docs/06-domain-driven-design.md §4: "KYC tier
// can only move forward through the documented review states, never skip
// states").
func TestKYCStateMachine_RejectsSkippedStates(t *testing.T) {
	t.Run("cannot review directly from PENDING", func(t *testing.T) {
		svc, _ := newTestCustomerService(t, false)
		ctx := context.Background()

		customer, err := svc.CreateProfile(ctx, "user-skip-review", "Jane Doe", testDOB, "ABCDE1234F")
		if err != nil {
			t.Fatalf("creating profile: %v", err)
		}

		if _, err := svc.ReviewKYC(ctx, customer.ID, true, domain.KYCTierBasic); err != domain.ErrInvalidKYCTransition {
			t.Fatalf("got %v, want ErrInvalidKYCTransition", err)
		}
	})

	t.Run("cannot submit for review twice", func(t *testing.T) {
		svc, _ := newTestCustomerService(t, false)
		ctx := context.Background()

		customer, err := svc.CreateProfile(ctx, "user-double-submit", "Jane Doe", testDOB, "ABCDE1234F")
		if err != nil {
			t.Fatalf("creating profile: %v", err)
		}
		if _, err := svc.SubmitForReview(ctx, customer.ID); err != nil {
			t.Fatalf("first submit: %v", err)
		}
		if _, err := svc.SubmitForReview(ctx, customer.ID); err != domain.ErrInvalidKYCTransition {
			t.Fatalf("got %v, want ErrInvalidKYCTransition", err)
		}
	})

	t.Run("cannot review a customer twice", func(t *testing.T) {
		svc, _ := newTestCustomerService(t, false)
		ctx := context.Background()

		customer, err := svc.CreateProfile(ctx, "user-double-review", "Jane Doe", testDOB, "ABCDE1234F")
		if err != nil {
			t.Fatalf("creating profile: %v", err)
		}
		if _, err := svc.SubmitForReview(ctx, customer.ID); err != nil {
			t.Fatalf("submit: %v", err)
		}
		if _, err := svc.ReviewKYC(ctx, customer.ID, true, domain.KYCTierBasic); err != nil {
			t.Fatalf("first review: %v", err)
		}
		if _, err := svc.ReviewKYC(ctx, customer.ID, true, domain.KYCTierBasic); err != domain.ErrInvalidKYCTransition {
			t.Fatalf("got %v, want ErrInvalidKYCTransition", err)
		}
	})

	t.Run("cannot approve at tier 0", func(t *testing.T) {
		svc, _ := newTestCustomerService(t, false)
		ctx := context.Background()

		customer, err := svc.CreateProfile(ctx, "user-bad-tier", "Jane Doe", testDOB, "ABCDE1234F")
		if err != nil {
			t.Fatalf("creating profile: %v", err)
		}
		if _, err := svc.SubmitForReview(ctx, customer.ID); err != nil {
			t.Fatalf("submit: %v", err)
		}
		if _, err := svc.ReviewKYC(ctx, customer.ID, true, domain.KYCTierUnverified); err != domain.ErrInvalidKYCTransition {
			t.Fatalf("got %v, want ErrInvalidKYCTransition", err)
		}
	})
}

// TestSubmitForReview_AutoApproveInDev covers the Phase 1 dev convenience
// documented in internal/service/kyc.go and
// docs/19-implementation-roadmap.md Phase 1: with AutoApproveKYC on,
// SubmitForReview itself lands on APPROVED/tier 1, not UNDER_REVIEW.
func TestSubmitForReview_AutoApproveInDev(t *testing.T) {
	svc, _ := newTestCustomerService(t, true)
	ctx := context.Background()

	customer, err := svc.CreateProfile(ctx, "user-auto-approve", "Jane Doe", testDOB, "ABCDE1234F")
	if err != nil {
		t.Fatalf("creating profile: %v", err)
	}

	result, err := svc.SubmitForReview(ctx, customer.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.KYCStatus != domain.KYCStatusApproved {
		t.Errorf("got status %v, want APPROVED", result.KYCStatus)
	}
	if result.KYCTier != domain.KYCTierBasic {
		t.Errorf("got tier %v, want tier 1 (basic)", result.KYCTier)
	}
}
