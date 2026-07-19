package service

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestPANCipher_RoundTrip(t *testing.T) {
	cipher := testPANCipher(t)

	ciphertext, err := cipher.Encrypt("ABCDE1234F")
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	if len(ciphertext) == 0 {
		t.Fatalf("expected non-empty ciphertext")
	}

	plaintext, err := cipher.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypting: %v", err)
	}
	if plaintext != "ABCDE1234F" {
		t.Errorf("got %q, want %q", plaintext, "ABCDE1234F")
	}
}

func TestPANCipher_RejectsWrongKeySize(t *testing.T) {
	if _, err := NewPANCipher(make([]byte, 16)); err == nil {
		t.Fatalf("expected an error for a non-32-byte key")
	}
}

func TestMaskPAN_NeverReturnsPlaintext(t *testing.T) {
	masked := MaskPAN("ABCDE1234F")
	if masked == "ABCDE1234F" {
		t.Fatalf("MaskPAN must not return the unmasked PAN")
	}
	if len(masked) != len("ABCDE1234F") {
		t.Errorf("masked value should preserve length for display purposes, got %q", masked)
	}
}

func TestResolvePANKey(t *testing.T) {
	t.Run("generates an ephemeral key when unset", func(t *testing.T) {
		key, generated, err := ResolvePANKey("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !generated {
			t.Errorf("expected generated=true when no key is configured")
		}
		if len(key) != PANKeySize {
			t.Errorf("got key length %d, want %d", len(key), PANKeySize)
		}
	})

	t.Run("rejects a key that doesn't decode to 32 bytes", func(t *testing.T) {
		if _, _, err := ResolvePANKey("dG9vc2hvcnQ="); err == nil {
			t.Fatalf("expected an error for a too-short key")
		}
	})

	t.Run("uses the configured key when present", func(t *testing.T) {
		raw := make([]byte, PANKeySize)
		if _, err := rand.Read(raw); err != nil {
			t.Fatalf("generating raw key: %v", err)
		}
		b64 := base64.StdEncoding.EncodeToString(raw)

		key, generated, err := ResolvePANKey(b64)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if generated {
			t.Errorf("expected generated=false when a key is configured")
		}
		if string(key) != string(raw) {
			t.Errorf("decoded key does not match input")
		}
	})
}
