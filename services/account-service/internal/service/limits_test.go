package service

import "testing"

func TestCheckWithinTierLimits(t *testing.T) {
	cases := []struct {
		name        string
		tier        int
		dailySoFar  int64
		proposed    int64
		wantErr     error
	}{
		{"tier 1 within limit", 1, 0, 10_000_00, nil},
		{"tier 1 exactly at limit", 1, 0, 50_000_00, nil},
		{"tier 1 exceeds limit", 1, 0, 50_000_01, ErrTierLimitExceeded},
		{"tier 1 exceeds limit cumulatively", 1, 40_000_00, 10_000_01, ErrTierLimitExceeded},
		{"tier 2 allows a larger amount than tier 1", 2, 0, 100_000_00, nil},
		{"tier 0 rejects any amount", 0, 0, 1, ErrTierLimitExceeded},
		{"unknown tier falls back to tier 0's zero limit", 99, 0, 1, ErrTierLimitExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckWithinTierLimits(tc.tier, tc.dailySoFar, tc.proposed)
			if err != tc.wantErr {
				t.Errorf("CheckWithinTierLimits(%d, %d, %d) = %v, want %v", tc.tier, tc.dailySoFar, tc.proposed, err, tc.wantErr)
			}
		})
	}
}
