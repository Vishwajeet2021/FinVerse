package http

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// refreshCookieName and refreshCookiePath implement the storage mechanism in
// docs/10-authentication-authorization.md §2: "HttpOnly, Secure, SameSite=Strict
// cookie". The path is scoped to the auth routes specifically — a bit
// stricter than the doc requires, so the cookie is never sent on unrelated
// requests to this service.
const (
	refreshCookieName = "finverse_refresh_token"
	refreshCookiePath = "/api/v1/auth"
)

// CookieConfig carries the one piece of environment-dependent behavior:
// Secure must be false for local http development (browsers drop Secure
// cookies over plain http) and true everywhere a real TLS edge exists.
type CookieConfig struct {
	Secure bool
}

func setRefreshCookie(c *gin.Context, cfg CookieConfig, token string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(refreshCookieName, token, maxAge, refreshCookiePath, "", cfg.Secure, true)
}

func clearRefreshCookie(c *gin.Context, cfg CookieConfig) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(refreshCookieName, "", -1, refreshCookiePath, "", cfg.Secure, true)
}

func readRefreshCookie(c *gin.Context) (string, error) {
	return c.Cookie(refreshCookieName)
}
