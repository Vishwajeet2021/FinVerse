package http

import (
	"crypto/rsa"
	"time"

	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// RouterDeps is everything routes.go needs to wire the HTTP surface —
// collected in one struct so cmd/server/main.go's composition root stays
// readable (docs/12-coding-standards.md §1.1: cmd/server is wiring only).
type RouterDeps struct {
	Handler        *Handler
	Verifier       AuthMiddleware
	DB             *gorm.DB
	PublicKey      *rsa.PublicKey
	KeyID          string
	Logger         *zap.Logger
	FrontendOrigin string
}

// AuthMiddleware is the subset of shared/go/jwtauth.Verifier routes.go needs —
// declared as an interface here so this package doesn't need to know about
// JWKS fetching, only that it can produce a Gin auth middleware.
type AuthMiddleware interface {
	Middleware() gin.HandlerFunc
}

func NewRouter(deps RouterDeps) *gin.Engine {
	r := gin.New()
	r.Use(telemetry.GinMiddleware("auth-service"))
	r.Use(sharedlogging.Recovery(deps.Logger))
	r.Use(sharedlogging.Middleware(deps.Logger))
	// AllowCredentials is required for the browser to send/receive the
	// refresh-token cookie (cookie.go); that means AllowOrigins can't be "*"
	// (the spec forbids combining wildcard origin with credentials) — a
	// single explicit frontend origin per docs/10-authentication-authorization.md §2.
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
	r.GET("/.well-known/jwks.json", JWKSHandler(deps.PublicKey, deps.KeyID))

	v1 := r.Group("/api/v1/auth")
	{
		v1.POST("/register", deps.Handler.Register)
		v1.POST("/login", deps.Handler.Login)
		v1.POST("/refresh", deps.Handler.Refresh)
		v1.POST("/logout", deps.Handler.Logout)
		v1.POST("/verify-email", deps.Handler.VerifyEmail)
		v1.POST("/password-reset/request", deps.Handler.RequestPasswordReset)
		v1.POST("/password-reset/confirm", deps.Handler.ConfirmPasswordReset)

		authed := v1.Group("")
		authed.Use(deps.Verifier.Middleware())
		authed.POST("/logout-all", deps.Handler.LogoutAll)
	}

	return r
}
