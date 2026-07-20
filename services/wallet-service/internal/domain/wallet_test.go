package domain

import "testing"

func TestWallet_IsActive(t *testing.T) {
	tests := []struct {
		name   string
		status WalletStatus
		want   bool
	}{
		{"active wallet can move money", WalletStatusActive, true},
		{"closed wallet cannot move money", WalletStatusClosed, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Wallet{Status: tt.status}
			if got := w.IsActive(); got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBalanceProjection_HasSufficientBalance(t *testing.T) {
	tests := []struct {
		name        string
		balance     int64
		debitAmount int64
		want        bool
	}{
		{"exact balance covers exact debit", 500_00, 500_00, true},
		{"balance covers a smaller debit", 500_00, 100_00, true},
		{"balance does not cover a larger debit", 100_00, 500_00, false},
		{"zero balance cannot cover any positive debit", 0, 1, false},
		{"zero balance trivially covers a zero debit", 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &BalanceProjection{BalanceMinor: tt.balance}
			if got := b.HasSufficientBalance(tt.debitAmount); got != tt.want {
				t.Errorf("HasSufficientBalance(%d) with balance %d = %v, want %v", tt.debitAmount, tt.balance, got, tt.want)
			}
		})
	}
}
