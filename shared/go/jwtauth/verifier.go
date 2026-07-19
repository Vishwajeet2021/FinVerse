package jwtauth

import (
	"context"
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// Verifier validates access tokens issued by Auth Service, per
// docs/10-authentication-authorization.md §4: every service re-verifies the
// token itself rather than trusting the API Gateway alone.
type Verifier struct {
	keyFunc  jwt.Keyfunc
	issuer   string
	audience string
}

// NewVerifier fetches and caches the JWKS from jwksURL (Auth Service's
// /.well-known/jwks.json), refreshing it in the background on the library's
// default schedule so a key rotation on Auth Service's side doesn't require
// restarting every downstream service. Used by every service other than
// Auth Service itself.
func NewVerifier(ctx context.Context, jwksURL, issuer, audience string) (*Verifier, error) {
	kf, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("fetching JWKS from %s: %w", jwksURL, err)
	}
	return &Verifier{keyFunc: kf.Keyfunc, issuer: issuer, audience: audience}, nil
}

// NewVerifierWithRetry wraps NewVerifier with retry-with-backoff, per the
// resilience pattern in docs/05-system-architecture.md §7. It exists because
// a plain startup-time JWKS fetch turns "Auth Service takes an extra second
// to start" (or, in production, "Auth Service is mid-redeploy") into every
// other service crash-looping — every service that verifies tokens depends
// on Auth Service being reachable at its own boot time, which orchestration
// (Docker Compose locally, ECS in AWS) does not guarantee. maxAttempts <= 0
// means "retry forever until ctx is cancelled".
func NewVerifierWithRetry(ctx context.Context, jwksURL, issuer, audience string, maxAttempts int, backoff time.Duration) (*Verifier, error) {
	var lastErr error
	for attempt := 1; maxAttempts <= 0 || attempt <= maxAttempts; attempt++ {
		v, err := NewVerifier(ctx, jwksURL, issuer, audience)
		if err == nil {
			return v, nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled while waiting for JWKS at %s: %w", jwksURL, ctx.Err())
		case <-time.After(backoff):
		}
	}
	return nil, fmt.Errorf("giving up on JWKS at %s after %d attempts: %w", jwksURL, maxAttempts, lastErr)
}

// NewVerifierFromPublicKey builds a Verifier directly from an in-memory
// public key, with no HTTP fetch. Auth Service uses this for its own
// protected routes (e.g. logout-all) — fetching its own JWKS over the
// network would be a pointless round trip (and a bootstrapping problem: the
// server isn't listening yet at the point it's wired up in cmd/server/main.go).
func NewVerifierFromPublicKey(pub *rsa.PublicKey, issuer, audience string) *Verifier {
	return &Verifier{
		keyFunc: func(_ *jwt.Token) (any, error) { return pub, nil },
		issuer:  issuer,
		audience: audience,
	}
}

// Verify parses and validates a raw access token, returning its claims.
func (v *Verifier) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, v.keyFunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)
	if err != nil {
		return nil, fmt.Errorf("verifying token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("token is not valid")
	}
	return claims, nil
}
