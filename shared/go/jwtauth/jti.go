package jwtauth

import "github.com/google/uuid"

// newJTI generates the unique token ID used for targeted revocation
// (docs/10-authentication-authorization.md §2.1, §2.3).
func newJTI() string {
	return uuid.NewString()
}
