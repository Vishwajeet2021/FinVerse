// Package mfa implements the second factor: TOTP (RFC 6238), encryption of the TOTP secret at
// rest, and single-use recovery codes. It imports nothing from the rest of the service so it
// can be read and tested on its own (docs/24-admin-mfa.md).
//
// TOTP is written against the standard library rather than a third-party module on purpose: it
// is about forty lines, it can be checked directly against the RFC's published test vectors
// (totp_test.go), and a credential primitive is not a place to take on a dependency's churn.
package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 / RFC 4226 define HOTP over HMAC-SHA1; every authenticator app implements exactly this.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// Period and Digits are the values every mainstream authenticator app assumes. They are
	// deliberately not configurable: a non-default value silently fails to enrol in many apps.
	Period = 30 * time.Second
	Digits = 6
	// secretBytes is 160 bits, the length RFC 4226 §4 recommends for an HMAC-SHA1 key.
	secretBytes = 20
	// skewSteps accepts the previous and next 30-second window as well as the current one, to
	// absorb clock drift between the phone and the server. It is the widest window that is
	// still ordinary; widening it would only help an attacker.
	skewSteps = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns a fresh random TOTP key.
func NewSecret() ([]byte, error) {
	secret := make([]byte, secretBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generating TOTP secret: %w", err)
	}
	return secret, nil
}

// EncodeSecret renders a key the way authenticator apps expect to be typed in (base32, no padding).
func EncodeSecret(secret []byte) string { return b32.EncodeToString(secret) }

// StepAt returns the 30-second counter for a point in time.
func StepAt(t time.Time) int64 { return t.Unix() / int64(Period/time.Second) }

// codeFor computes the HOTP value (RFC 4226 §5.3) for a counter with the given digit count.
func codeFor(secret []byte, counter int64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter)) //nolint:gosec // counter is a non-negative time step.
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

// Code is the code an authenticator app would show for the step containing t.
func Code(secret []byte, t time.Time) string { return codeFor(secret, StepAt(t), Digits) }

// Verify checks a submitted code against the steps around now. It returns the matching step so
// the caller can refuse to accept the same step twice (replay): a code that was valid when it
// was typed stays valid for up to 90 seconds, which is ample time to be reused by someone who
// watched it being entered.
//
// Every candidate step is compared, in constant time, regardless of an earlier match, so the
// time taken does not reveal which step (or how many digits) were right.
func Verify(secret []byte, submitted string, now time.Time) (step int64, ok bool) {
	if !IsTOTPShape(submitted) {
		return 0, false
	}
	current := StepAt(now)
	var matched int64
	found := 0
	for delta := -skewSteps; delta <= skewSteps; delta++ {
		candidate := current + int64(delta)
		if candidate < 0 {
			continue
		}
		want := codeFor(secret, candidate, Digits)
		if subtle.ConstantTimeCompare([]byte(want), []byte(submitted)) == 1 {
			matched = candidate
			found = 1
		}
	}
	return matched, found == 1
}

// IsTOTPShape reports whether s looks like a six-digit authenticator code.
func IsTOTPShape(s string) bool {
	if len(s) != Digits {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ProvisioningURI is the otpauth:// link a QR code carries. The issuer appears twice (as the
// label prefix and as a parameter) because older apps read one and newer apps the other.
func ProvisioningURI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", EncodeSecret(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(Digits))
	q.Set("period", fmt.Sprint(int(Period/time.Second)))
	return "otpauth://totp/" + label + "?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}
