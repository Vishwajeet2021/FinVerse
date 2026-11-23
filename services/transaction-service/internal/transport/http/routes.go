package http

import (
	"net/http"

	"github.com/finverse/shared/apiresponse"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/finverse/transaction-service/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// NewRouter wires health/readiness and the daily-closing trigger. There is
// deliberately no API Gateway route to this service (docs/07-microservice-specifications.md §6) —
// this router only needs to be reachable from inside the private network for
// ops/cron use, not from the public internet.
func NewRouter(ledger *service.LedgerService, db *gorm.DB, logger *zap.Logger) *gin.Engine {
	r := gin.New()
	r.Use(telemetry.GinMiddleware("transaction-service"))
	r.Use(sharedlogging.Recovery(logger))
	r.Use(sharedlogging.Middleware(logger))

	r.GET("/health", HealthHandler)
	r.GET("/metrics", telemetry.MetricsHandler())
	r.GET("/ready", ReadyHandler(db))

	// FR-TXN-5. Triggered manually or by a scheduler in later phases
	// (docs/14-aws-infrastructure.md §7) — Phase 2's job is correctness of
	// the closing logic itself, not the scheduling mechanism.
	r.POST("/internal/daily-closing/run", func(c *gin.Context) {
		if err := ledger.RunDailyClosing(c.Request.Context()); err != nil {
			apiresponse.Internal(c, "DAILY_CLOSING_FAILED", "Daily closing failed to complete.")
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "completed"})
	})

	return r
}
