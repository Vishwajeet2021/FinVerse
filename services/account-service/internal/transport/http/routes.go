package http

import (
	"github.com/finverse/shared/svcauth"
	"time"

	"github.com/finverse/shared/httpsec"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type RouterDeps struct {
	Handler         *Handler
	Verifier        AuthMiddleware
	DB              *gorm.DB
	Logger          *zap.Logger
	FrontendOrigin  string
	ServiceVerifier *svcauth.Verifier
}

type AuthMiddleware interface {
	Middleware() gin.HandlerFunc
}

func NewRouter(deps RouterDeps) *gin.Engine {
	r := gin.New()
	r.Use(telemetry.GinMiddleware("account-service"))
	r.Use(httpsec.Headers(), httpsec.BodyLimit(httpsec.DefaultBodyLimit))
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
	r.GET("/metrics", telemetry.MetricsHandler())
	r.GET("/ready", ReadyHandler(deps.DB))
	r.GET("/internal/accounts/by-number/:accountNumber", deps.ServiceVerifier.Middleware("payment-service"), deps.Handler.ResolveAccountNumber)

	r.GET("/internal/account-status/:id", deps.ServiceVerifier.Middleware("payment-service", "wallet-service"), deps.Handler.AccountStatus)

	admin := r.Group("/internal/admin/accounts", deps.ServiceVerifier.Middleware("admin-service"))
	{
		admin.GET("/:id", deps.Handler.AdminGetAccount)
		admin.POST("/:id/freeze", deps.Handler.AdminFreeze)
		admin.POST("/:id/unfreeze", deps.Handler.AdminUnfreeze)
	}

	v1 := r.Group("/api/v1/accounts")
	v1.Use(deps.Verifier.Middleware())
	{
		v1.POST("", jwtauth.RequireScopes("accounts:manage"), deps.Handler.OpenAccount)
		v1.GET("", jwtauth.RequireScopes("accounts:read"), deps.Handler.ListMyAccounts)
		v1.GET("/:id", jwtauth.RequireScopes("accounts:read"), deps.Handler.GetAccount)
		v1.GET("/:id/balance", jwtauth.RequireScopes("accounts:read"), deps.Handler.GetBalance)
		v1.POST("/:id/freeze", jwtauth.RequireScopes("accounts:manage"), deps.Handler.Freeze)
		v1.POST("/:id/unfreeze", jwtauth.RequireScopes("accounts:manage"), deps.Handler.Unfreeze)
		v1.DELETE("/:id", jwtauth.RequireScopes("accounts:manage"), deps.Handler.Close)
		v1.GET("/:id/statement", jwtauth.RequireScopes("accounts:read"), deps.Handler.GetStatement)
	}

	return r
}
