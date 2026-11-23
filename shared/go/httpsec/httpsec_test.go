package httpsec

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func router(limit int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Headers(), BodyLimit(limit))
	r.POST("/echo", func(c *gin.Context) {
		var v map[string]any
		if err := c.ShouldBindJSON(&v); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusOK)
	})
	return r
}

func TestHeadersAreSet(t *testing.T) {
	w := httptest.NewRecorder()
	router(1024).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{}`)))
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer", "Cache-Control": "no-store",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Error("missing Content-Security-Policy")
	}
}

func TestBodyWithinLimitAccepted(t *testing.T) {
	w := httptest.NewRecorder()
	router(1024).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":1}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestDeclaredOversizeRejectedWith413(t *testing.T) {
	w := httptest.NewRecorder()
	router(10).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":"`+strings.Repeat("x", 100)+`"}`)))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", w.Code)
	}
}

func TestStreamedOversizeIsCutOff(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":"`+strings.Repeat("x", 100)+`"}`))
	req.ContentLength = -1 // chunked: no declared length to check up front
	w := httptest.NewRecorder()
	router(10).ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatal("an oversized streamed body must not be accepted")
	}
}
