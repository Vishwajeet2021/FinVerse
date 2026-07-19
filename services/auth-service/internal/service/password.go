package service

import (
	"github.com/alexedwards/argon2id"
)

// MinPasswordLength implements the policy in docs/11-security-standards.md §8
// and docs/10-authentication-authorization.md §8.
const MinPasswordLength = 12

// HashPassword and VerifyPassword wrap Argon2id per docs/11-security-standards.md §3 —
// this is the one place in the service that touches password hashing, so it's
// done correctly once rather than re-implemented per call site.

func HashPassword(plaintext string) (string, error) {
	return argon2id.CreateHash(plaintext, argon2id.DefaultParams)
}

func VerifyPassword(plaintext, hash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(plaintext, hash)
}
