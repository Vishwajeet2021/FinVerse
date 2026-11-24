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
	RequestsPerMin int64
}

func NewRouter(deps RouterDeps) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(telemetry.GinMiddleware("reporting-service"))
	r.Use(httpsec.Headers(), httpsec.BodyLimit(httpsec.DefaultBodyLimit))
	r.Use(sharedlogging.Recovery(deps.Logger))
	r.Use(sharedlogging.Middleware(deps.Logger))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{deps.FrontendOrigin},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Request-Id"},
		ExposeHeaders:    []string{"Content-Disposition", "Location"}, // the browser needs these to save the file
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	r.GET("/health", HealthHandler)
	r.GET("/metrics", telemetry.MetricsHandler())
	r.GET("/ready", ReadyHandler(deps.DB))

	h := deps.Handler
	v1 := r.Group("/api/v1/reports")
	v1.Use(deps.Verifier.Middleware())

	// Requests are throttled per user; creating a report costs ledger reads, so Redis being
	// down refuses the request (fail closed) rather than lifting the bound.
	perUser := func(c *gin.Context) string { return "user:" + caller(c) }
	throttle := deps.Limiter.Middleware("report-create", deps.RequestsPerMin, time.Minute, perUser, false)

	v1.GET("", jwtauth.RequireScopes("reports:read"), h.List)
	v1.GET("/:id", jwtauth.RequireScopes("reports:read"), h.Get)
	v1.GET("/:id/download", jwtauth.RequireScopes("reports:read"), h.Download)
	v1.POST("/statements", jwtauth.RequireScopes("reports:write"), throttle, h.RequestStatement)
	v1.POST("/fraud-summary", jwtauth.RequireScopes("reports:admin"), jwtauth.RequireMFA(), throttle, h.RequestFraudSummary)
	return r
}
