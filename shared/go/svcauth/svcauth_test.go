package svcauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var key = []byte(strings.Repeat("k", 32))

func TestRoundTrip(t *testing.T) {
	svc, err := NewVerifier(key).Verify(NewSigner("payment-service", key).Token())
	if err != nil || svc != "payment-service" {
		t.Fatalf("got (%q, %v), want payment-service", svc, err)
	}
}

func TestRejections(t *testing.T) {
	v := NewVerifier(key)
	good := NewSigner("payment-service", key).Token()
	other := NewSigner("payment-service", []byte(strings.Repeat("z", 32))).Token()
	tamperedName := "wallet-service" + good[strings.Index(good, "."):]
	cases := map[string]string{
		"empty": "", "garbage": "abc", "wrong key": other, "name swapped": tamperedName,
		"two parts": "a.b", "empty name": ".1." + strings.Repeat("0", 64),
	}
	for name, tok := range cases {
		if _, err := v.Verify(tok); err == nil {
			t.Errorf("%s: token accepted", name)
		}
	}
}

func TestExpiredAndFutureTokensRejected(t *testing.T) {
	for _, offset := range []time.Duration{-2 * MaxSkew, 2 * MaxSkew} {
		s := NewSigner("payment-service", key)
		offset := offset
		s.now = func() time.Time { return time.Now().Add(offset) }
		if _, err := NewVerifier(key).Verify(s.Token()); err != ErrExpired {
			t.Errorf("offset %v: err = %v, want ErrExpired", offset, err)
		}
	}
}

func TestShortKeyRejected(t *testing.T) {
	t.Setenv(envKey, "short")
	if _, err := KeyFromEnv(); err == nil {
		t.Fatal("a short key must be refused")
	}
	t.Setenv(envKey, strings.Repeat("a", 32))
	if _, err := KeyFromEnv(); err != nil {
		t.Fatalf("a 32-char key must be accepted: %v", err)
	}
}

func serve(v *Verifier, allowed ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/internal/x", v.Middleware(allowed...), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func do(r *gin.Engine, token string) int {
	req := httptest.NewRequest(http.MethodPost, "/internal/x", nil)
	if token != "" {
		req.Header.Set(HTTPHeader, token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestHTTPMiddleware(t *testing.T) {
	v := NewVerifier(key)
	if got := do(serve(v), ""); got != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", got)
	}
	if got := do(serve(v), NewSigner("payment-service", key).Token()); got != http.StatusOK {
		t.Errorf("valid token: %d, want 200", got)
	}
	r := serve(v, "account-service")
	if got := do(r, NewSigner("payment-service", key).Token()); got != http.StatusUnauthorized {
		t.Errorf("service not on allow-list: %d, want 401", got)
	}
	if got := do(r, NewSigner("account-service", key).Token()); got != http.StatusOK {
		t.Errorf("allow-listed service: %d, want 200", got)
	}
}

func TestGRPCInterceptors(t *testing.T) {
	v := NewVerifier(key)
	info := &grpc.UnaryServerInfo{FullMethod: "/ledger.v1.LedgerService/PostTransaction"}
	ok := func(ctx context.Context, req any) (any, error) { return "done", nil }
	intercept := v.UnaryServerInterceptor("payment-service")

	_, err := intercept(context.Background(), nil, info, ok)
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("no metadata: %v, want Unauthenticated", err)
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKey, NewSigner("payment-service", key).Token()))
	if resp, err := intercept(ctx, nil, info, ok); err != nil || resp != "done" {
		t.Errorf("valid caller: (%v, %v)", resp, err)
	}

	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKey, NewSigner("wallet-service", key).Token()))
	if _, err := intercept(ctx, nil, info, ok); status.Code(err) != codes.Unauthenticated {
		t.Errorf("caller not on allow-list: %v, want Unauthenticated", err)
	}
}
