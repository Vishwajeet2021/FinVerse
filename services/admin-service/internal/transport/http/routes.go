package http

import (
	"time"

	"github.com/finverse/shared/httpsec"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/ratelimit"
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
	Limiter        *ratelimit.Limiter
	ActionsPerMin  int64
}

func NewRouter(deps RouterDeps) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(telemetry.GinMiddleware("admin-service"))
	r.Use(httpsec.Headers(), httpsec.BodyLimit(httpsec.DefaultBodyLimit))
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

	h := deps.Handler
	v1 := r.Group("/api/v1/admin")
	v1.Use(deps.Verifier.Middleware())

	// Commands are throttled per administrator and fail closed if Redis is down: a privileged
	// action is worth a brief refusal, and the limit bounds the damage of a stolen session.
	perUser := func(c *gin.Context) string { return "admin:" + actorID(c) }
	throttle := deps.Limiter.Middleware("admin-actions", deps.ActionsPerMin, time.Minute, perUser, false)

	v1.GET("/dashboard", jwtauth.RequireScopes("admin:dashboard:read"), h.Dashboard)
	v1.GET("/actions", jwtauth.RequireScopes("admin:dashboard:read"), h.ListActions)
	v1.POST("/accounts/:id/freeze", jwtauth.RequireScopes("admin:accounts:freeze"), throttle, h.Freeze)
	v1.POST("/accounts/:id/unfreeze", jwtauth.RequireScopes("admin:accounts:freeze"), throttle, h.Unfreeze)
	v1.POST("/fraud/cases/:id/resolve", jwtauth.RequireScopes("fraud:resolve"), throttle, h.ResolveFraudCase)
	v1.POST("/customers/:id/kyc-review", jwtauth.RequireScopes("customers:kyc:review"), throttle, h.ReviewKYC)
	return r
}
