package http

import (
	"time"

	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type RouterDeps struct {
	Handler        *Handler
	Verifier       AuthMiddleware
	DB             *gorm.DB
	Logger         *zap.Logger
	FrontendOrigin string
	Environment    string
}

type AuthMiddleware interface {
	Middleware() gin.HandlerFunc
}

func NewRouter(deps RouterDeps) *gin.Engine {
	r := gin.New()
	r.Use(telemetry.GinMiddleware("notification-service"))
	r.Use(sharedlogging.Recovery(deps.Logger))
	r.Use(sharedlogging.Middleware(deps.Logger))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{deps.FrontendOrigin},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/health", HealthHandler)
	r.GET("/metrics", telemetry.MetricsHandler())
	r.GET("/ready", ReadyHandler(deps.DB))

	v1 := r.Group("/api/v1/notifications")
	v1.Use(deps.Verifier.Middleware())
	{
		v1.GET("", jwtauth.RequireScopes("notifications:read"), deps.Handler.ListMine)

		admin := v1.Group("/admin", jwtauth.RequireScopes("notifications:admin"))
		admin.GET("/all", deps.Handler.ListAll)
		admin.GET("/stats", deps.Handler.Stats)
		admin.GET("/dlq", deps.Handler.DLQList)
		admin.POST("/dlq/:stream/:id/replay", deps.Handler.DLQReplay)
		admin.DELETE("/dlq/:stream/:id", deps.Handler.DLQDiscard)
		if deps.Environment != "production" {
			admin.PUT("/provider-outage", deps.Handler.SetProviderOutage)
		}
	}
	return r
}
