package jwtauth

import (
	"crypto/rsa"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessTokenTTL and RefreshTokenTTL implement the lifetimes documented in
// docs/10-authentication-authorization.md §2. Admin refresh lifetime is
// shorter (§6) and is passed explicitly by callers that need it, rather than
// hardcoded here.
const (
	AccessTokenTTL          = 15 * time.Minute
	CustomerRefreshTokenTTL = 7 * 24 * time.Hour
	AdminRefreshTokenTTL    = 24 * time.Hour
)

// Signer mints RS256 access tokens. Only Auth Service holds a Signer — the
// private key never leaves it (docs/11-security-standards.md §3).
type Signer struct {
	privateKey *rsa.PrivateKey
	kid        string
	issuer     string
	audience   string
}

func NewSigner(privateKey *rsa.PrivateKey, kid, issuer, audience string) *Signer {
	return &Signer{privateKey: privateKey, kid: kid, issuer: issuer, audience: audience}
}

// AccessTokenInput carries everything needed to mint a token for one subject.
type AccessTokenInput struct {
	UserID     string
	CustomerID string
	Role       string
	Scopes     []string
	KYCTier    int
	MFA        bool
}

// Sign mints a short-lived access token per docs/10-authentication-authorization.md §2.1.
func (s *Signer) Sign(in AccessTokenInput) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   in.UserID,
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
			ID:        newJTI(),
		},
		CustomerID: in.CustomerID,
		Role:       in.Role,
		Scopes:     in.Scopes,
		KYCTier:    in.KYCTier,
		MFA:        in.MFA,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid
	return token.SignedString(s.privateKey)
}
