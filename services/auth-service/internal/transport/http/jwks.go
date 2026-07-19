package http

import (
	"crypto/rsa"
	"net/http"

	"github.com/finverse/shared/jwtauth"
	"github.com/gin-gonic/gin"
)

// JWKSHandler serves Auth Service's public key so every other service can
// verify tokens without a synchronous call to Auth
// (docs/10-authentication-authorization.md §4, FR-AUTH-10).
func JWKSHandler(pub *rsa.PublicKey, kid string) gin.HandlerFunc {
	doc := jwtauth.PublicKeyToJWKS(pub, kid)
	return func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=300")
		c.JSON(http.StatusOK, doc)
	}
}
