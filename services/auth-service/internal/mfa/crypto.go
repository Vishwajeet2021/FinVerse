package mfa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// A TOTP secret is a shared secret, not a password: the server must be able to read it back to
// check codes, so it cannot be hashed. The best available protection is encryption at rest, so
// that a copy of the database alone (a backup, a replica, a SQL injection read) does not hand
// over every administrator's second factor.
//
// The key is held outside the database (an environment variable here; a secrets manager in
// Phase 4). The stored form is "v1:" + base64(nonce || ciphertext). The version prefix lets a
// future key rotation tell old and new records apart.
const cipherVersion = "v1:"

var (
	ErrCipherKey     = errors.New("MFA encryption key must be 32 bytes, base64 encoded")
	ErrBadCiphertext = errors.New("MFA secret could not be decrypted")
)

// Cipher seals and opens TOTP secrets with AES-256-GCM.
type Cipher struct{ aead cipher.AEAD }

// NewCipher builds a Cipher from a base64-encoded 32-byte key.
func NewCipher(keyB64 string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(keyB64))
	if err != nil || len(key) != 32 {
		return nil, ErrCipherKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal encrypts secret. userID is bound in as additional authenticated data, so a ciphertext
// copied from one user's row to another's will not decrypt: someone with write access to the
// table cannot swap in a secret they know.
func (c *Cipher) Seal(userID string, secret []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, secret, []byte(userID))
	return cipherVersion + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal for the same userID.
func (c *Cipher) Open(userID, stored string) ([]byte, error) {
	if !strings.HasPrefix(stored, cipherVersion) {
		return nil, ErrBadCiphertext
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, cipherVersion))
	if err != nil || len(raw) < c.aead.NonceSize()+c.aead.Overhead() {
		return nil, ErrBadCiphertext
	}
	nonce, body := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, body, []byte(userID))
	if err != nil {
		return nil, ErrBadCiphertext
	}
	return plain, nil
}
