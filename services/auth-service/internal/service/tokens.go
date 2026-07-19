package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// generateOpaqueToken produces a URL-safe random token for refresh/verification/
// reset tokens. Only its hash is ever persisted (docs/08-database-design.md §7,
// §Auth Service — "raw token never stored") so a database read alone can never
// yield a usable token.
func generateOpaqueToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken is a fast, deterministic hash suitable for looking up a token by
// its value — this is not a password, so a slow KDF (Argon2id) would be the
// wrong tool; SHA-256 over a high-entropy random token is the correct and
// standard approach.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
