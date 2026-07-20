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
	// AllowCredentials required even though this service sets no cookie
	// itself — the frontend's fetch wrapper sends credentials:'include'
	// unconditionally (docs/10-authentication-authorization.md §2), and a
	// browser blocks a credentialed request's response without this.
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{deps.FrontendOrigin},
		AllowMethods:     []string{"GET", "POST", "PATCH", "DELETE"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/health", HealthHandler)
	r.GET("/ready", ReadyHandler(deps.DB))

	v1 := r.Group("/api/v1/accounts")
	v1.Use(deps.Verifier.Middleware())
	{
		v1.POST("", jwtauth.RequireScopes("accounts:manage"), deps.Handler.OpenAccount)
		v1.GET("", jwtauth.RequireScopes("accounts:read"), deps.Handler.ListMyAccounts)
		v1.GET("/:id", jwtauth.RequireScopes("accounts:read"), deps.Handler.GetAccount)
		v1.GET("/:id/balance", jwtauth.RequireScopes("accounts:read"), deps.Handler.GetBalance)
		v1.POST("/:id/freeze", jwtauth.RequireScopes("accounts:manage"), deps.Handler.Freeze)
		v1.POST("/:id/unfreeze", jwtauth.RequireScopes("accounts:manage"), deps.Handler.Unfreeze)
		v1.GET("/:id/statement", jwtauth.RequireScopes("accounts:read"), deps.Handler.GetStatement)
	}

	return r
}
