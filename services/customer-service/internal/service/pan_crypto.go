package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// PANKeySize is the required key length for AES-256-GCM
// (docs/11-security-standards.md §4).
const PANKeySize = 32

// ResolvePANKey decodes CUSTOMER_PAN_ENCRYPTION_KEY (base64, 32 raw bytes).
// If b64 is empty, a random key is generated for this process only — never
// persisted to disk, unlike Auth Service's LoadOrGenerateRSAKey
// (internal/config/keys.go), because persisting a generated PAN key would be
// actively wrong: a key that changes across restarts would just be a
// different key, and one that's written next to already-encrypted rows would
// create a false sense that old data stays readable. In staging/prod this
// must be a stable secret sourced from Secrets Manager
// (docs/11-security-standards.md §3) — the caller (cmd/server/main.go) is
// responsible for logging a loud warning when generated=true, since this
// package has no logger dependency.
func ResolvePANKey(b64 string) (key []byte, generated bool, err error) {
	if b64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, false, fmt.Errorf("decoding CUSTOMER_PAN_ENCRYPTION_KEY: %w", err)
		}
		if len(decoded) != PANKeySize {
			return nil, false, fmt.Errorf("CUSTOMER_PAN_ENCRYPTION_KEY must decode to %d bytes, got %d", PANKeySize, len(decoded))
		}
		return decoded, false, nil
	}

	generatedKey := make([]byte, PANKeySize)
	if _, err := rand.Read(generatedKey); err != nil {
		return nil, false, fmt.Errorf("generating ephemeral PAN encryption key: %w", err)
	}
	return generatedKey, true, nil
}

// PANCipher implements the AES-256-GCM field-level encryption required for
// Restricted-class data (docs/11-security-standards.md §4). It never logs or
// returns anything derived from plaintext except through Encrypt/Decrypt
// themselves — callers must use MaskPAN, not a decrypted value, for any
// display or log line (FR-CUST-7).
type PANCipher struct {
	key []byte
}

// NewPANCipher constructs a cipher from a 32-byte AES-256 key.
func NewPANCipher(key []byte) (*PANCipher, error) {
	if len(key) != PANKeySize {
		return nil, fmt.Errorf("PAN encryption key must be %d bytes, got %d", PANKeySize, len(key))
	}
	return &PANCipher{key: key}, nil
}

// Encrypt returns nonce||ciphertext||tag, suitable for storing directly in
// the `pan_encrypted BYTEA` column (docs/08-database-design.md §6).
func (p *PANCipher) Encrypt(plaintext string) ([]byte, error) {
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return nil, fmt.Errorf("initializing AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initializing GCM mode: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt reverses Encrypt. It is called only on the narrow, audited path
// that genuinely needs the plaintext PAN (docs/11-security-standards.md §4) —
// Customer Service's Phase 1 scope has no such path yet; this exists for
// future admin/compliance use and is exercised directly by tests.
func (p *PANCipher) Decrypt(ciphertext []byte) (string, error) {
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", fmt.Errorf("initializing AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("initializing GCM mode: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext shorter than nonce size")
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}
	return string(plaintext), nil
}

// MaskPAN computes the display-safe masked form stored in `pan_masked`
// (docs/08-database-design.md §6) and used for every UI/log/audit need so
// decryption is never required just to show or log a PAN (FR-CUST-7). A PAN
// is 10 characters (5-letter prefix, 4 digits, 1 check letter); this mirrors
// the worked example in docs/08-database-design.md §6 ("ABCXX1234X"): the
// first 3 letters of the prefix and all 4 digits stay visible, the last 2
// letters of the prefix (indices 3,4) and the trailing check letter are
// masked.
func MaskPAN(pan string) string {
	runes := []rune(pan)
	n := len(runes)
	if n == 0 {
		return ""
	}

	masked := make([]rune, n)
	copy(masked, runes)
	for _, idx := range [3]int{3, 4, n - 1} {
		if idx >= 0 && idx < n {
			masked[idx] = 'X'
		}
	}
	return string(masked)
}
