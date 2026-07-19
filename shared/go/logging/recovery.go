package logging

import (
	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Recovery replaces Gin's default recovery middleware so a panic in a handler
// is logged with a stack trace and turned into the standard error envelope
// (docs/09-api-standards.md §2.4) instead of a bare connection reset. Per
// docs/12-coding-standards.md §1.3, a panic reaching this middleware is
// always a bug to be fixed, never a substitute for real error handling.
func Recovery(base *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				FromContext(c.Request.Context()).Error("panic recovered",
					zap.Any("panic", err),
					zap.String("path", c.FullPath()),
				)
				apiresponse.Internal(c, "INTERNAL_ERROR", "Something went wrong on our end. Please try again.")
			}
		}()
		c.Next()
	}
}
