// Package httpsec holds the HTTP hardening every FinVerse API shares:
// response security headers and request body limits (docs/22 F-1, F-2).
package httpsec

import (
	"net/http"

	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
)

// DefaultBodyLimit is generous for every JSON endpoint in the platform (the largest
// legitimate payload is a payment or profile body of a few hundred bytes) while
// stopping a client from making a service buffer an arbitrarily large body.
const DefaultBodyLimit int64 = 1 << 20 // 1 MiB

// Headers sets the response headers that apply to an API (JSON, never framed, never
// sniffed, never cached by shared caches). HSTS is deliberately not set here: it is only
// meaningful over TLS and is applied at the TLS-terminating edge (docs/10, API Gateway or
// CloudFront in Phase 4); sending it over plain HTTP is ignored by browsers.
func Headers() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		// An API never serves documents; this stops a browser that is somehow tricked
		// into rendering a response from executing anything in it.
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")
		c.Next()
	}
}

// BodyLimit rejects bodies larger than max with 413. Content-Length is checked up front
// for a cheap early answer, and the body reader is capped so a lying or chunked client
// cannot exceed it either.
func BodyLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > max {
			apiresponse.Error(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "The request is too large.", nil)
			c.Abort()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		c.Next()
	}
}
