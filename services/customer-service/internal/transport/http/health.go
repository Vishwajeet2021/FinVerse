package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HealthHandler is liveness only — "is the process up" — per
// docs/16-observability.md §9. It never touches the database.
func HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ReadyHandler is readiness — "can this instance serve traffic right now" —
// checked via a DB ping, per docs/16-observability.md §9 and FR-X-4. The ALB
// target group uses this one for routing decisions, not HealthHandler.
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
