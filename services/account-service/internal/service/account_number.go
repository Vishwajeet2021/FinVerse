package service

import (
	"crypto/rand"
	"math/big"
)

// maxAccountNumberAttempts bounds OpenAccount's generate-and-retry loop
// (internal/service/account_service.go). 11 random digits give a ~10^11
// number space, so a collision is astronomically unlikely — this bound
// exists only to turn "something is systematically wrong" (e.g. a broken
// RNG) into a clear error instead of an infinite loop.
const maxAccountNumberAttempts = 5

// generateAccountNumber produces a 12-digit numeric account number: 11
// cryptographically random digits plus a Luhn (ISO/IEC 7812) check digit, so
// a single mistyped or transposed digit is locally detectable without a
// database round trip. This is a reasonable, documented choice for a
// portfolio-scale system — a real bank's numbering scheme would also encode
// a branch/routing prefix, which is out of scope here. Actual uniqueness is
// still guaranteed by the database's UNIQUE constraint on
// accounts.account_number (docs/08-database-design.md §5), not by this
// function; OpenAccount retries on domain.ErrAccountNumberCollision.
func generateAccountNumber() (string, error) {
	digits := make([]byte, 11)
	for i := range digits {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		digits[i] = byte('0') + byte(n.Int64()) //nolint:gosec // n is rand.Int(_, big.NewInt(10)), always 0-9
	}
	check := luhnCheckDigit(digits)
	return string(digits) + string(byte('0')+byte(check)), nil //nolint:gosec // luhnCheckDigit always returns 0-9
}

// luhnCheckDigit computes the Luhn check digit for payload (the digits
// preceding it), doubling every second digit counting from the rightmost
// payload digit (the one immediately preceding the check digit) — the
// standard Luhn generation rule.
func luhnCheckDigit(payload []byte) int {
	sum := 0
	for i := len(payload) - 1; i >= 0; i-- {
		n := int(payload[i] - '0')
		if (len(payload)-i)%2 == 1 {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
	}
	return (10 - sum%10) % 10
}
