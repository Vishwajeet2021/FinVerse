package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Runs against a real Redis (REDIS_TEST_ADDR=localhost:6379), like the event bus tests. Each
// test uses unique key names, so it does not interfere with data already in that Redis.
func limiter(t *testing.T) *Limiter {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	return New(redis.NewClient(&redis.Options{Addr: addr}))
}

func TestAllowsUpToLimitThenRefuses(t *testing.T) {
	l := limiter(t)
	key := "t:" + uuid.NewString()
	for i := 1; i <= 3; i++ {
		ok, _, err := l.Allow(context.Background(), key, 3, time.Minute)
		if err != nil || !ok {
			t.Fatalf("hit %d: (%v, %v), want allowed", i, ok, err)
		}
	}
	ok, retry, err := l.Allow(context.Background(), key, 3, time.Minute)
	if err != nil || ok {
		t.Fatalf("4th hit: (%v, %v), want refused", ok, err)
	}
	if retry <= 0 || retry > time.Minute {
		t.Errorf("retryAfter = %v, want within the window", retry)
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := limiter(t)
	a, b := "t:"+uuid.NewString(), "t:"+uuid.NewString()
	l.Allow(context.Background(), a, 1, time.Minute)
	if ok, _, _ := l.Allow(context.Background(), a, 1, time.Minute); ok {
		t.Fatal("second hit on key a must be refused")
	}
	if ok, _, _ := l.Allow(context.Background(), b, 1, time.Minute); !ok {
		t.Fatal("key b has its own budget")
	}
}

func TestWindowResets(t *testing.T) {
	l := limiter(t)
	key := "t:" + uuid.NewString()
	base := time.Now()
	l.now = func() time.Time { return base }
	l.Allow(context.Background(), key, 1, time.Minute)
	if ok, _, _ := l.Allow(context.Background(), key, 1, time.Minute); ok {
		t.Fatal("over limit in the same window")
	}
	l.now = func() time.Time { return base.Add(time.Minute) }
	if ok, _, _ := l.Allow(context.Background(), key, 1, time.Minute); !ok {
		t.Fatal("a new window must start with a fresh budget")
	}
}

func serve(l *Limiter, failOpen bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	name := "m" + uuid.NewString()
	r.POST("/x", l.Middleware(name, 2, time.Minute, func(*gin.Context) string { return "same" }, failOpen),
		func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestMiddlewareReturns429WithRetryAfter(t *testing.T) {
	r := serve(limiter(t), true)
	var last *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		last = httptest.NewRecorder()
		r.ServeHTTP(last, httptest.NewRequest(http.MethodPost, "/x", nil))
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
}

func TestRedisDownFailOpenVersusClosed(t *testing.T) {
	down := New(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1}))
	for _, c := range []struct {
		failOpen bool
		want     int
	}{{true, http.StatusOK}, {false, http.StatusServiceUnavailable}} {
		w := httptest.NewRecorder()
		serve(down, c.failOpen).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
		if w.Code != c.want {
			t.Errorf("failOpen=%v: status %d, want %d", c.failOpen, w.Code, c.want)
		}
	}
}
