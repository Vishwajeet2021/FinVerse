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

type AuthMiddleware interface{ Middleware() gin.HandlerFunc }

type RouterDeps struct {
	Handler        *Handler
	Verifier       AuthMiddleware
	DB             *gorm.DB
	Logger         *zap.Logger
	FrontendOrigin string
}

func NewRouter(deps RouterDeps) *gin.Engine {
	r := gin.New()
	r.Use(telemetry.GinMiddleware("fraud-service"))
	r.Use(sharedlogging.Recovery(deps.Logger))
	r.Use(sharedlogging.Middleware(deps.Logger))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{deps.FrontendOrigin},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	r.GET("/health", HealthHandler)
	r.GET("/metrics", telemetry.MetricsHandler())
	r.GET("/ready", ReadyHandler(deps.DB))

	v1 := r.Group("/api/v1/fraud")
	v1.Use(deps.Verifier.Middleware())
	v1.GET("/cases", jwtauth.RequireScopes("fraud:read"), deps.Handler.ListCases)
	v1.POST("/cases/:id/resolve", jwtauth.RequireScopes("fraud:resolve"), deps.Handler.ResolveCase)
	return r
}
