package jwtauth

import (
	"crypto/rsa"
	"encoding/base64"
	"math/big"
)

// JWK is a single RSA public key in JWK format (RFC 7517).
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKS is the document served at /.well-known/jwks.json
// (docs/10-authentication-authorization.md; docs/09-api-standards.md).
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// PublicKeyToJWKS builds the JWKS document Auth Service serves for other
// services to verify tokens without ever seeing the private key.
func PublicKeyToJWKS(pub *rsa.PublicKey, kid string) JWKS {
	return JWKS{Keys: []JWK{
		{
			Kty: "RSA",
			Use: "sig",
			Kid: kid,
			Alg: "RS256",
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		},
	}}
}
