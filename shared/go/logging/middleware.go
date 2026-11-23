package logging

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type ctxKey int

const loggerCtxKey ctxKey = iota

// Middleware assigns/propagates a request ID (X-Request-Id in and out), attaches
// a request-scoped logger to both the Gin context and the request's
// context.Context (so internal/service and internal/repository code — which
// takes context.Context, not *gin.Context, per docs/12-coding-standards.md §1.4 —
// can log with the same correlation fields), and logs one line per completed
// request per docs/16-observability.md §5.
func Middleware(base *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		reqID := c.GetHeader("X-Request-Id")
		if reqID == "" {
			reqID = uuid.NewString()
		}
		c.Writer.Header().Set("X-Request-Id", reqID)
		c.Set("request_id", reqID)

		reqLogger := base.With(zap.String("request_id", reqID))
		// The tracing middleware runs first, so a span is already active; carrying
		// its ids lets Grafana jump from a log line to the Jaeger trace.
		if sc := trace.SpanContextFromContext(c.Request.Context()); sc.IsValid() {
			reqLogger = reqLogger.With(zap.String("trace_id", sc.TraceID().String()), zap.String("span_id", sc.SpanID().String()))
		}
		c.Set("logger", reqLogger)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), loggerCtxKey, reqLogger))

		c.Next()

		reqLogger.Info("request completed",
			zap.String("method", c.Request.Method),
			zap.String("path", c.FullPath()),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("duration", time.Since(start)),
		)
	}
}

// FromContext returns the request-scoped logger stored by Middleware, falling
// back to a no-op logger if none is present (e.g. in a unit test that doesn't
// go through the middleware) so callers never need a nil check.
func FromContext(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(loggerCtxKey).(*zap.Logger); ok && l != nil {
		return l
	}
	return zap.NewNop()
}
