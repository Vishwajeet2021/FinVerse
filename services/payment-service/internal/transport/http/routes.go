package http

import (
	"time"

	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
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
}

type AuthMiddleware interface {
	Middleware() gin.HandlerFunc
}

func NewRouter(deps RouterDeps) *gin.Engine {
	r := gin.New()
	r.Use(sharedlogging.Recovery(deps.Logger))
	r.Use(sharedlogging.Middleware(deps.Logger))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{deps.FrontendOrigin},
		AllowMethods:     []string{"GET", "POST", "PATCH", "DELETE"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Request-Id", "Idempotency-Key"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/health", HealthHandler)
	r.GET("/ready", ReadyHandler(deps.DB))

	v1 := r.Group("/api/v1/payments")
	// Authentication is required (docs/00-master-ai-instructions.md §5); the
	// deferred check is ownership of fromAccountID specifically — see
	// internal/service/payment_service.go's comment on InitiatePayment for
	// why that's a documented Phase 3 gap, not a missing auth check.
	v1.Use(deps.Verifier.Middleware())
	{
		v1.POST("", jwtauth.RequireScopes("payments:write"), deps.Handler.InitiatePayment)
		v1.GET("/:id", jwtauth.RequireScopes("payments:read"), deps.Handler.GetPayment)
	}

	return r
}
