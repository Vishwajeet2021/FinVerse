package mfa

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1, secret "12345678901234567890", eight digits.
func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	vectors := []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, v := range vectors {
		got := codeFor(secret, v.unix/30, 8)
		if got != v.want {
			t.Errorf("T=%d: got %s, want %s", v.unix, got, v.want)
		}
		// The six-digit code is the low six digits of the same value.
		if six := codeFor(secret, v.unix/30, 6); six != v.want[2:] {
			t.Errorf("T=%d: six-digit %s, want %s", v.unix, six, v.want[2:])
		}
	}
}

func TestVerifyAcceptsAdjacentStepsButNotFurther(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1_700_000_000, 0)
	cur := StepAt(now)

	for _, delta := range []int64{-1, 0, 1} {
		code := codeFor(secret, cur+delta, Digits)
		step, ok := Verify(secret, code, now)
		if !ok || step != cur+delta {
			t.Errorf("delta %d: ok=%v step=%d, want step %d", delta, ok, step, cur+delta)
		}
	}
	for _, delta := range []int64{-2, 2, 10} {
		if _, ok := Verify(secret, codeFor(secret, cur+delta, Digits), now); ok {
			t.Errorf("delta %d must be rejected", delta)
		}
	}
}

func TestVerifyRejectsMalformedInput(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1_700_000_000, 0)
	good := Code(secret, now)
	for _, bad := range []string{"", "12345", "1234567", "12345a", " " + good, good + " ", "٠٠٠٠٠٠"} {
		if _, ok := Verify(secret, bad, now); ok {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestProvisioningURI(t *testing.T) {
	uri := ProvisioningURI("FinVerse", "ops@example.com", []byte("12345678901234567890"))
	for _, want := range []string{
		"otpauth://totp/FinVerse:ops@example.com?",
		"secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		"issuer=FinVerse", "algorithm=SHA1", "digits=6", "period=30",
	} {
		if !strings.Contains(uri, want) {
			t.Errorf("URI %q is missing %q", uri, want)
		}
	}
}

func testKey() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func TestCipherRoundTripAndBinding(t *testing.T) {
	c, err := NewCipher(testKey())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Seal("user-1", []byte("top secret"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "top secret") || !strings.HasPrefix(sealed, "v1:") {
		t.Fatalf("unexpected stored form %q", sealed)
	}
	plain, err := c.Open("user-1", sealed)
	if err != nil || string(plain) != "top secret" {
		t.Fatalf("round trip: %q, %v", plain, err)
	}
	// Copied onto another user's row: must not open.
	if _, err := c.Open("user-2", sealed); err == nil {
		t.Fatal("ciphertext opened for a different user")
	}
	// Two seals of the same value differ (random nonce).
	again, _ := c.Seal("user-1", []byte("top secret"))
	if again == sealed {
		t.Fatal("nonce reuse: identical ciphertexts")
	}
	// Tampering is detected.
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := c.Open("user-1", tampered); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err := c.Open("user-1", "plain-text"); err == nil {
		t.Fatal("unversioned value accepted")
	}
}

func TestCipherRejectsBadKeys(t *testing.T) {
	for _, k := range []string{"", "short", base64.StdEncoding.EncodeToString([]byte("too short")), "!!!not base64!!!"} {
		if _, err := NewCipher(k); err == nil {
			t.Errorf("key %q accepted", k)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("got %d codes", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Errorf("unexpected format %q", c)
		}
		norm, ok := NormalizeRecoveryCode(c)
		if !ok || len(norm) != 10 {
			t.Errorf("code %q does not normalise", c)
		}
		if seen[norm] {
			t.Errorf("duplicate code %q", c)
		}
		seen[norm] = true
	}
	if n, ok := NormalizeRecoveryCode(" " + strings.ToLower(codes[0]) + " "); !ok || n == "" {
		t.Error("lower case / padded code must normalise")
	}
	for _, bad := range []string{"", "123456", "ABCDE-FGHI", "ABCDE-FGHI1", "ABCDE-FGH!J"} {
		if _, ok := NormalizeRecoveryCode(bad); ok {
			t.Errorf("%q must not normalise", bad)
		}
	}
}
