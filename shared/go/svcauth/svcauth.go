// Package svcauth authenticates service-to-service calls (docs/22 D-S1, closing F-3 and F-4).
//
// Every internal call carries a token of the form
//
//	<service>.<unix-seconds>.<hex HMAC-SHA256(key, service "|" unix-seconds)>
//
// The receiver recomputes the MAC in constant time, requires the timestamp to be within
// MaxSkew of its own clock (which bounds how long a captured token can be replayed), and can
// restrict which named services may call a given endpoint.
//
// This is deliberately simple enough to run in local Compose without a certificate authority.
// It authenticates "a FinVerse service holding the key", and the name in the token is trusted
// only as far as the key holder is. When the platform is deployed, mTLS (per-service
// certificates) replaces this with per-service identity; the call sites stay the same.
package svcauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// HTTPHeader and MetadataKey carry the token on HTTP and gRPC respectively.
	HTTPHeader  = "X-Service-Token"
	MetadataKey = "x-service-token"

	// MaxSkew is how far a token's timestamp may differ from the receiver's clock.
	MaxSkew = 60 * time.Second

	// MinKeyBytes rejects keys too short to resist guessing.
	MinKeyBytes = 32

	envKey = "SERVICE_AUTH_KEY"
)

var (
	ErrMissingToken = errors.New("svcauth: missing service token")
	ErrBadToken     = errors.New("svcauth: invalid service token")
	ErrExpired      = errors.New("svcauth: service token outside the allowed clock skew")
)

// KeyFromEnv loads SERVICE_AUTH_KEY and refuses to continue without a strong one. There is
// no insecure default: an unauthenticated internal API is exactly what this package removes.
func KeyFromEnv() ([]byte, error) {
	k := os.Getenv(envKey)
	if len(k) < MinKeyBytes {
		return nil, fmt.Errorf("%s must be set to at least %d characters", envKey, MinKeyBytes)
	}
	return []byte(k), nil
}

func mac(key []byte, service, ts string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(service + "|" + ts))
	return hex.EncodeToString(h.Sum(nil))
}

// Signer mints tokens identifying one calling service.
type Signer struct {
	service string
	key     []byte
	now     func() time.Time
}

func NewSigner(service string, key []byte) *Signer {
	return &Signer{service: service, key: key, now: time.Now}
}

// Token returns a fresh token. It is cheap (one HMAC), so it is minted per call.
func (s *Signer) Token() string {
	ts := strconv.FormatInt(s.now().Unix(), 10)
	return s.service + "." + ts + "." + mac(s.key, s.service, ts)
}

// SignRequest attaches a token to an outgoing HTTP request.
func (s *Signer) SignRequest(req *http.Request) { req.Header.Set(HTTPHeader, s.Token()) }

// UnaryClientInterceptor attaches a token to every outgoing unary gRPC call.
func (s *Signer) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataKey, s.Token())
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// Verifier checks tokens on the receiving side.
type Verifier struct {
	key []byte
	now func() time.Time
}

func NewVerifier(key []byte) *Verifier { return &Verifier{key: key, now: time.Now} }

// Verify returns the calling service's name if the token is authentic and fresh.
func (v *Verifier) Verify(token string) (string, error) {
	if token == "" {
		return "", ErrMissingToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" {
		return "", ErrBadToken
	}
	service, ts, sig := parts[0], parts[1], parts[2]
	if !hmac.Equal([]byte(sig), []byte(mac(v.key, service, ts))) {
		return "", ErrBadToken
	}
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", ErrBadToken
	}
	skew := v.now().Sub(time.Unix(secs, 0))
	if skew > MaxSkew || skew < -MaxSkew {
		return "", ErrExpired
	}
	return service, nil
}

func permitted(service string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if a == service {
			return true
		}
	}
	return false
}

// Middleware protects HTTP routes. An empty allowed list accepts any authenticated service.
// Failures are answered 401 with no detail about why, so the endpoint reveals nothing to a
// caller that does not hold the key.
func (v *Verifier) Middleware(allowed ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		service, err := v.Verify(c.GetHeader(HTTPHeader))
		if err != nil || !permitted(service, allowed) {
			apiresponse.Error(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required.", nil)
			c.Abort()
			return
		}
		c.Set("calling_service", service)
		c.Next()
	}
}

// UnaryServerInterceptor protects every unary method of a gRPC server.
func (v *Verifier) UnaryServerInterceptor(allowed ...string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		var token string
		if vals := md.Get(MetadataKey); len(vals) > 0 {
			token = vals[0]
		}
		service, err := v.Verify(token)
		if err != nil || !permitted(service, allowed) {
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}
		return handler(ctx, req)
	}
}
