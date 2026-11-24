package jwtauth

import (
	"strings"

	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
)

const claimsContextKey = "claims"

// Middleware authenticates every request via the Authorization: Bearer header
// (docs/09-api-standards.md §2.8) and stores the verified claims in the Gin
// context for RequireScopes and handlers to use.
func (v *Verifier) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			apiresponse.Unauthorized(c, "MISSING_TOKEN", "Authentication is required for this request.")
			return
		}

		claims, err := v.Verify(strings.TrimPrefix(header, prefix))
		if err != nil {
			apiresponse.Unauthorized(c, "INVALID_TOKEN", "Your session is invalid or has expired. Please log in again.")
			return
		}

		c.Set(claimsContextKey, claims)
		c.Next()
	}
}

// ClaimsFromContext retrieves the claims Middleware attached to this request.
// Handlers call this after Middleware has run; it panics if called on a route
// that isn't behind Middleware, which is intentional — an authorization check
// with no claims to check is a bug, not a graceful case.
func ClaimsFromContext(c *gin.Context) *Claims {
	return c.MustGet(claimsContextKey).(*Claims)
}

// RequireScopes rejects the request with 403 unless the token carries every
// listed scope, per docs/10-authentication-authorization.md §3.2. This is the
// "kind of thing this role can do" check; ownership ("which specific row")
// is a separate check every handler must still perform itself (§3.3).
func RequireScopes(scopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ClaimsFromContext(c)
		for _, scope := range scopes {
			if !claims.HasScope(scope) {
				apiresponse.Forbidden(c, "INSUFFICIENT_SCOPE", "You don't have permission to perform this action.")
				return
			}
		}
		c.Next()
	}
}

// RequireMFA rejects the request with 403 unless the access token was issued to a session that
// passed the second factor (the "mfa" claim). Auth Service never issues a token to an internal
// role without it, so on privileged routes this is defence in depth: it keeps those routes closed
// even if a token were ever minted by a path that skipped the check, and it makes the rule
// visible where the privileged action is, not only in the issuer (docs/24-admin-mfa.md D-M8).
func RequireMFA() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !ClaimsFromContext(c).MFA {
			apiresponse.Forbidden(c, "MFA_REQUIRED", "This action requires two-step verification. Please log in again and complete it.")
			return
		}
		c.Next()
	}
}
