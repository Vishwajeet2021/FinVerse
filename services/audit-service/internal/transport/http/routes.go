package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/finverse/audit-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type AuthMiddleware interface{ Middleware() gin.HandlerFunc }

type RouterDeps struct {
	Service        *service.Service
	Verifier       AuthMiddleware
	DB             *gorm.DB
	Logger         *zap.Logger
	FrontendOrigin string
}

func NewRouter(deps RouterDeps) *gin.Engine {
	r := gin.New()
	r.Use(telemetry.GinMiddleware("audit-service"))
	r.Use(sharedlogging.Recovery(deps.Logger))
	r.Use(sharedlogging.Middleware(deps.Logger))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{deps.FrontendOrigin},
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	r.GET("/health", HealthHandler)
	r.GET("/metrics", telemetry.MetricsHandler())
	r.GET("/ready", ReadyHandler(deps.DB))

	// Read-only by construction: there is deliberately no write endpoint
	// (docs/07-microservice-specifications.md section 12) - records arrive only via the bus.
	v1 := r.Group("/api/v1/audit")
	v1.Use(deps.Verifier.Middleware())
	v1.GET("/records", jwtauth.RequireScopes("audit:read"), func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		before, _ := strconv.ParseInt(c.Query("beforeId"), 10, 64)
		recs, err := deps.Service.List(c.Request.Context(), service.Query{EventType: c.Query("eventType"), BeforeID: before, Limit: limit})
		if err != nil {
			apiresponse.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not read audit records.", nil)
			return
		}
		apiresponse.OK(c, recs)
	})
	return r
}
