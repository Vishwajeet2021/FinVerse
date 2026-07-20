package domain

import "testing"

func TestProductType_Valid(t *testing.T) {
	cases := []struct {
		name string
		pt   ProductType
		want bool
	}{
		{"savings is valid", ProductSavings, true},
		{"current is valid", ProductCurrent, true},
		{"joint is valid", ProductJoint, true},
		{"virtual is valid", ProductVirtual, true},
		{"empty is invalid", ProductType(""), false},
		{"unknown is invalid", ProductType("CREDIT_CARD"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pt.Valid(); got != tc.want {
				t.Errorf("ProductType(%q).Valid() = %v, want %v", tc.pt, got, tc.want)
			}
		})
	}
}

func TestAccount_CanDebit(t *testing.T) {
	cases := []struct {
		name    string
		status  AccountStatus
		wantErr error
	}{
		{"active account accepts debit", StatusActive, nil},
		{"frozen account rejects debit", StatusFrozen, ErrAccountFrozen},
		{"closed account rejects debit", StatusClosed, ErrAccountClosed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Status: tc.status}
			if err := a.CanDebit(); err != tc.wantErr {
				t.Errorf("CanDebit() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
