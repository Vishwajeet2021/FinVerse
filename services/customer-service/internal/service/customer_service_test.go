package service

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/finverse/customer-service/internal/domain"
)

func testPANCipher(t *testing.T) *PANCipher {
	t.Helper()
	key := make([]byte, PANKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generating test PAN key: %v", err)
	}
	cipher, err := NewPANCipher(key)
	if err != nil {
		t.Fatalf("constructing test PAN cipher: %v", err)
	}
	return cipher
}

func newTestCustomerService(t *testing.T, autoApproveKYC bool) (*CustomerService, *fakeCustomerRepo) {
	t.Helper()
	customers := newFakeCustomerRepo()
	documents := newFakeDocumentRepo()
	beneficiaries := newFakeBeneficiaryRepo()
	storage := newFakeStorage()
	svc := NewCustomerService(customers, documents, beneficiaries, storage, testPANCipher(t), autoApproveKYC)
	return svc, customers
}

var testDOB = time.Date(1990, 1, 15, 0, 0, 0, 0, time.UTC)

func TestCreateProfile(t *testing.T) {
	cases := []struct {
		name        string
		userID      string
		preexisting bool
		wantErr     error
	}{
		{name: "success", userID: "user-1", wantErr: nil},
		{name: "duplicate user id", userID: "user-2", preexisting: true, wantErr: domain.ErrProfileAlreadyExists},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, customers := newTestCustomerService(t, false)
			ctx := context.Background()

			if tc.preexisting {
				if _, err := svc.CreateProfile(ctx, tc.userID, "Existing User", testDOB, "ABCDE1234F"); err != nil {
					t.Fatalf("seeding preexisting profile: %v", err)
				}
			}

			customer, err := svc.CreateProfile(ctx, tc.userID, "Jane Doe", testDOB, "ABCDE1234F")

			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("got err %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if customer.KYCStatus != domain.KYCStatusPending {
				t.Errorf("got status %v, want PENDING", customer.KYCStatus)
			}
			if customer.KYCTier != domain.KYCTierUnverified {
				t.Errorf("got tier %v, want 0", customer.KYCTier)
			}
			if customer.PANMasked == "" || customer.PANMasked == "ABCDE1234F" {
				t.Errorf("PAN masked value looks wrong: %q", customer.PANMasked)
			}
			if len(customer.PANEncrypted) == 0 {
				t.Errorf("expected PAN to be encrypted at rest")
			}
			if _, ok := customers.byUserID[tc.userID]; !ok {
				t.Errorf("customer was not persisted")
			}
		})
	}
}

func TestGetProfileByUserID(t *testing.T) {
	svc, _ := newTestCustomerService(t, false)
	ctx := context.Background()

	created, err := svc.CreateProfile(ctx, "user-lookup", "Jane Doe", testDOB, "ABCDE1234F")
	if err != nil {
		t.Fatalf("creating profile: %v", err)
	}

	found, err := svc.GetProfileByUserID(ctx, "user-lookup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("got customer %s, want %s", found.ID, created.ID)
	}

	if _, err := svc.GetProfileByUserID(ctx, "nonexistent-user"); err != domain.ErrCustomerNotFound {
		t.Errorf("got %v, want ErrCustomerNotFound", err)
	}
}

func TestUpdateProfile_DoesNotAffectKYC(t *testing.T) {
	svc, _ := newTestCustomerService(t, false)
	ctx := context.Background()

	customer, err := svc.CreateProfile(ctx, "user-update", "Jane Doe", testDOB, "ABCDE1234F")
	if err != nil {
		t.Fatalf("creating profile: %v", err)
	}

	updated, err := svc.UpdateProfile(ctx, customer.ID, "Jane A. Doe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.FullName != "Jane A. Doe" {
		t.Errorf("got full name %q, want %q", updated.FullName, "Jane A. Doe")
	}
	if updated.KYCStatus != domain.KYCStatusPending || updated.KYCTier != domain.KYCTierUnverified {
		t.Errorf("profile update must not affect KYC state, got status=%v tier=%v", updated.KYCStatus, updated.KYCTier)
	}
}

func TestAddBeneficiary_StartsUnverified(t *testing.T) {
	svc, _ := newTestCustomerService(t, false)
	ctx := context.Background()

	customer, err := svc.CreateProfile(ctx, "user-beneficiary", "Jane Doe", testDOB, "ABCDE1234F")
	if err != nil {
		t.Fatalf("creating profile: %v", err)
	}

	beneficiary, err := svc.AddBeneficiary(ctx, customer.ID, "John Payee", "ACC-123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if beneficiary.Verified {
		t.Errorf("newly added beneficiary must start unverified (FR-CUST-4)")
	}

	list, err := svc.ListBeneficiaries(ctx, customer.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].ID != beneficiary.ID {
		t.Fatalf("expected the newly added beneficiary to be listed, got %+v", list)
	}

	if err := svc.VerifyBeneficiary(ctx, customer.ID, beneficiary.ID); err != nil {
		t.Fatalf("unexpected error verifying beneficiary: %v", err)
	}
	verifiedList, err := svc.ListBeneficiaries(ctx, customer.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verifiedList[0].Verified {
		t.Errorf("expected beneficiary to be verified after VerifyBeneficiary")
	}
}
