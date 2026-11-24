package http

import (
	"github.com/finverse/shared/ratelimit"
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
	Handler        *Handler
	Verifier       AuthMiddleware
	DB             *gorm.DB
	Logger         *zap.Logger
	FrontendOrigin string
	Limiter        *ratelimit.Limiter
	PaymentsPerMin int64
}

type AuthMiddleware interface {
	Middleware() gin.HandlerFunc
}

func NewRouter(deps RouterDeps) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(telemetry.GinMiddleware("payment-service"))
	r.Use(httpsec.Headers(), httpsec.BodyLimit(httpsec.DefaultBodyLimit))
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
	r.GET("/metrics", telemetry.MetricsHandler())
	r.GET("/ready", ReadyHandler(deps.DB))

	v1 := r.Group("/api/v1/payments")
	v1.Use(deps.Verifier.Middleware())
	{
		h := deps.Handler
		// Money-moving writes are throttled per authenticated user. Fraud velocity rules are the
		// business control; this is the cheap outer bound that keeps a runaway client from
		// reaching them at all.
		perUser := func(c *gin.Context) string { return "user:" + callerID(c) }
		throttle := deps.Limiter.Middleware("payments-write", deps.PaymentsPerMin, time.Minute, perUser, true)
		w, rd := jwtauth.RequireScopes("payments:write"), jwtauth.RequireScopes("payments:read")
		v1.POST("", w, throttle, h.InitiatePayment)
		v1.POST("/scheduled", w, throttle, h.CreateScheduled)
		v1.GET("/scheduled", rd, h.ListScheduled)
		v1.DELETE("/scheduled/:id", w, h.CancelScheduled)
		v1.POST("/standing-instructions", w, throttle, h.CreateStanding)
		v1.GET("/standing-instructions", rd, h.ListStanding)
		v1.POST("/standing-instructions/:id/pause", w, h.PauseStanding)
		v1.POST("/standing-instructions/:id/resume", w, h.ResumeStanding)
		v1.DELETE("/standing-instructions/:id", w, h.CancelStanding)

		v1.GET("/:id", rd, h.GetPayment)
	}

	return r
}
