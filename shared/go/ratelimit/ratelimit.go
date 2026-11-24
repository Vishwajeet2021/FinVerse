// Package ratelimit throttles requests with a fixed window counter in Redis (docs/22 D-S4, F-6).
//
// Redis is already part of the stack and is shared by every replica of a service, so limits
// hold no matter which instance a client reaches. A fixed window is deliberately simple: it
// permits a short burst of up to 2x the limit across a window boundary, which is acceptable
// for abuse control (it is not a billing meter).
package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	client *redis.Client
	now    func() time.Time
}

func New(client *redis.Client) *Limiter { return &Limiter{client: client, now: time.Now} }

// Allow counts one hit against key in the current window. retryAfter is how long until the
// window resets when the hit is refused.
func (l *Limiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (allowed bool, retryAfter time.Duration, err error) {
	// One window per bucket number, so the key itself expires and no cleanup job is needed.
	bucket := l.now().UnixNano() / int64(window)
	redisKey := fmt.Sprintf("rl:%s:%d", key, bucket)

	pipe := l.client.Pipeline()
	incr := pipe.Incr(ctx, redisKey)
	pipe.Expire(ctx, redisKey, window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, fmt.Errorf("rate limit counter: %w", err)
	}
	if incr.Val() <= limit {
		return true, 0, nil
	}
	next := time.Unix(0, (bucket+1)*int64(window))
	return false, next.Sub(l.now()), nil
}

// KeyFunc derives the identity a limit applies to (client IP, user id, ...).
type KeyFunc func(*gin.Context) string

// ByIP limits per client address. Gin's ClientIP honours X-Forwarded-For only from proxies the
// router trusts; services call SetTrustedProxies so a client cannot choose its own address.
func ByIP(c *gin.Context) string { return "ip:" + c.ClientIP() }

// Middleware limits a route to limit requests per window per key. name namespaces the counter
// so two routes do not share one budget.
//
// failOpen decides what happens when Redis is unreachable: true lets the request through
// (availability for read paths), false refuses it (routes where abuse costs more than a brief
// outage, such as registration and password reset).
func (l *Limiter) Middleware(name string, limit int64, window time.Duration, key KeyFunc, failOpen bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 200*time.Millisecond)
		defer cancel()

		allowed, retryAfter, err := l.Allow(ctx, name+":"+key(c), limit, window)
		if err != nil {
			if failOpen {
				c.Next()
				return
			}
			apiresponse.Error(c, http.StatusServiceUnavailable, "TEMPORARILY_UNAVAILABLE", "Please try again in a moment.", nil)
			c.Abort()
			return
		}
		if !allowed {
			secs := int(retryAfter.Seconds()) + 1
			c.Header("Retry-After", strconv.Itoa(secs))
			apiresponse.Error(c, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests. Please wait and try again.", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}
