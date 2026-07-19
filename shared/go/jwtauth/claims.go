// Package jwtauth implements the JWT issuance/verification contract defined
// in docs/10-authentication-authorization.md. Auth Service uses Signer to
// mint tokens; every other service uses Verifier (backed by Auth Service's
// JWKS endpoint) to validate them without a synchronous call to Auth.
package jwtauth

import "github.com/golang-jwt/jwt/v5"

// Claims is the JWT payload shape from docs/10-authentication-authorization.md §2.1.
type Claims struct {
	jwt.RegisteredClaims

	CustomerID string   `json:"customer_id,omitempty"`
	Role       string   `json:"role"`
	Scopes     []string `json:"scopes"`
	KYCTier    int      `json:"kyc_tier"`
	MFA        bool     `json:"mfa"`
}

// HasScope reports whether the token carries the given scope. Handlers must
// use this — never Role — as the authorization decision, per
// docs/10-authentication-authorization.md §2.1: role is UI-facing metadata,
// scopes are the actual grant.
func (c Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}
