// Package http is Transaction Service's minimal internal HTTP surface —
// health/readiness (FR-X-4) and an on-demand daily-closing trigger
// (FR-TXN-5). Everything customer/admin-facing about the ledger goes
// through gRPC (internal/transport/grpc) or another service's REST API,
// never this one directly, per docs/07-microservice-specifications.md §6.
package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func ReadyHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sqlDB, err := db.DB()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "reason": "db handle unavailable"})
			return
		}
		if err := sqlDB.PingContext(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "reason": "db unreachable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
