package service

import "testing"

// luhnValid re-implements the standard Luhn validation independently of
// luhnCheckDigit's generation logic, so this test actually catches a wrong
// generation formula rather than checking the code against itself.
func luhnValid(number string) bool {
	sum := 0
	parity := len(number) % 2
	for i, r := range number {
		n := int(r - '0')
		if i%2 == parity {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
	}
	return sum%10 == 0
}

func TestGenerateAccountNumber(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		num, err := generateAccountNumber()
		if err != nil {
			t.Fatalf("generateAccountNumber() error: %v", err)
		}
		if len(num) != 12 {
			t.Fatalf("generateAccountNumber() = %q, want length 12", num)
		}
		if !luhnValid(num) {
			t.Errorf("generateAccountNumber() = %q, fails Luhn validation", num)
		}
		if seen[num] {
			t.Fatalf("generateAccountNumber() produced a duplicate in %d draws: %q", i+1, num)
		}
		seen[num] = true
	}
}

func TestLuhnCheckDigit_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"all zeros", "00000000000"},
		{"ascending digits", "12345678901"},
		{"all nines", "99999999999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := luhnCheckDigit([]byte(tc.payload))
			full := tc.payload + string(byte('0'+check)) //nolint:gosec // luhnCheckDigit always returns 0-9
			if !luhnValid(full) {
				t.Errorf("payload %q with check digit %d = %q, fails Luhn validation", tc.payload, check, full)
			}
		})
	}
}
