package jwtauth

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireMFA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const iss, aud = "https://auth.test", "api"
	signer := NewSigner(key, "kid", iss, aud)
	verifier := NewVerifierFromPublicKey(&key.PublicKey, iss, aud)

	r := gin.New()
	r.GET("/privileged", verifier.Middleware(), RequireScopes("admin:x"), RequireMFA(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	call := func(mfa bool) int {
		tok, err := signer.Sign(AccessTokenInput{UserID: "u1", Role: "admin", Scopes: []string{"admin:x"}, MFA: mfa})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/privileged", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if got := call(true); got != http.StatusOK {
		t.Errorf("token with mfa=true: got %d, want 200", got)
	}
	if got := call(false); got != http.StatusForbidden {
		t.Errorf("token with mfa=false: got %d, want 403", got)
	}
}
