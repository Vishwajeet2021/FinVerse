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

// RouterDeps is everything routes.go needs to wire the HTTP surface —
// collected in one struct so cmd/server/main.go's composition root stays
// readable (docs/12-coding-standards.md §1.1: cmd/server is wiring only).
type RouterDeps struct {
	Handler        *Handler
	Verifier       AuthMiddleware
	DB             *gorm.DB
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
	r.Use(sharedlogging.Recovery(deps.Logger))
	r.Use(sharedlogging.Middleware(deps.Logger))
	// AllowCredentials must be true even though this service never sets a
	// cookie itself: the frontend's fetch wrapper sends credentials:'include'
	// unconditionally (it needs to, for Auth Service's refresh-token cookie —
	// see docs/10-authentication-authorization.md §2), and a browser will
	// block the response entirely if a credentialed request's CORS response
	// doesn't echo AllowCredentials, regardless of whether this service
	// happens to use that latitude. AllowOrigins can't be "*" in that case,
	// which is why it's the single explicit frontend origin, same as auth-service.
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{deps.FrontendOrigin},
		AllowMethods:     []string{"GET", "POST", "PATCH", "DELETE"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/health", HealthHandler)
	r.GET("/ready", ReadyHandler(deps.DB))

	// Internal, service-to-service only — see GetKYCTierByUserID's comment
	// in handlers.go. Not routed through the public API Gateway.
	r.GET("/internal/customers/by-user/:userId/kyc-tier", deps.Handler.GetKYCTierByUserID)

	v1 := r.Group("/api/v1/customers")
	v1.Use(deps.Verifier.Middleware())
	{
		// Every route below sits behind the JWT verifier
		// (docs/09-api-standards.md §2.8); `/me` routes additionally resolve
		// "which customer" from the caller's own JWT subject
		// (resolveOwnCustomer in handlers.go) rather than trusting a
		// client-supplied ID, per docs/10-authentication-authorization.md §3.3.
		v1.POST("", jwtauth.RequireScopes("customers:write"), deps.Handler.CreateProfile)
		v1.GET("/me", jwtauth.RequireScopes("customers:read"), deps.Handler.GetMe)
		v1.PATCH("/me", jwtauth.RequireScopes("customers:write"), deps.Handler.UpdateMe)
		v1.POST("/me/kyc/submit", jwtauth.RequireScopes("customers:write"), deps.Handler.SubmitKYC)
		v1.POST("/me/documents", jwtauth.RequireScopes("customers:write"), deps.Handler.AddDocument)
		v1.POST("/me/beneficiaries", jwtauth.RequireScopes("customers:write"), deps.Handler.AddBeneficiary)
		v1.GET("/me/beneficiaries", jwtauth.RequireScopes("customers:read"), deps.Handler.ListBeneficiaries)

		// Admin-only, not an ownership route by design — see the comment on
		// Handler.ReviewKYC in handlers.go.
		v1.POST("/:id/kyc/review", jwtauth.RequireScopes("customers:kyc:review"), deps.Handler.ReviewKYC)
	}

	return r
}
