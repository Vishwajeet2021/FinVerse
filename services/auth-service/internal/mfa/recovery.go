package mfa

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// Recovery codes are the way back in when the phone is lost. Each is single use and is stored
// only as a hash (the caller hashes; see service.hashToken), so they behave like passwords:
// shown once at enrolment, never retrievable afterwards.
//
// Ten characters of base32 is 50 bits. That is far beyond what the attempt limits (five tries
// per challenge, account lockout after five failures) would let anyone test online, and short
// enough to copy by hand from a printed sheet.
const (
	RecoveryCodeCount = 10
	recoveryChars     = 10
)

// NewRecoveryCodes returns RecoveryCodeCount fresh codes formatted as XXXXX-XXXXX.
func NewRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, RecoveryCodeCount)
	for len(codes) < RecoveryCodeCount {
		buf := make([]byte, recoveryChars)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("generating recovery code: %w", err)
		}
		var sb strings.Builder
		for i, b := range buf {
			if i == recoveryChars/2 {
				sb.WriteByte('-')
			}
			// 32 symbols and a 5-bit mask: every symbol is equally likely, no modulo bias.
			sb.WriteByte(b32alphabet[b&0x1f])
		}
		codes = append(codes, sb.String())
	}
	return codes, nil
}

const b32alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

// NormalizeRecoveryCode makes a typed code comparable: upper case, without separators.
// It reports false if what is left cannot be a recovery code, so callers can reject malformed
// input without a database lookup.
func NormalizeRecoveryCode(s string) (string, bool) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
	if len(s) != recoveryChars {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(b32alphabet, rune(s[i])) {
			return "", false
		}
	}
	return s, true
}
