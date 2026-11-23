// Auth Service entrypoint. This file is wiring only — no business logic —
// per docs/12-coding-standards.md §1.1.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/finverse/auth-service/internal/config"
	"github.com/finverse/auth-service/internal/migrate"
	"github.com/finverse/auth-service/internal/repository/postgres"
	"github.com/finverse/auth-service/internal/service"
	transporthttp "github.com/finverse/auth-service/internal/transport/http"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	cfg := config.Load()

	level := zapcore.InfoLevel
	if cfg.LogLevel == "debug" {
		level = zapcore.DebugLevel
	}
	logger, err := sharedlogging.New("auth-service", level)
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	shutdownTracing, err := telemetry.Init(context.Background(), "auth-service")
	if err != nil {
		logger.Fatal("initializing tracing", zap.Error(err))
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	if err := migrate.Up(cfg.DatabaseURL); err != nil {
		logger.Fatal("applying migrations", zap.Error(err))
	}

	db, err := postgres.NewDB(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("connecting to database", zap.Error(err))
	}

	privateKey, err := config.LoadOrGenerateRSAKey(cfg.JWTPrivateKeyPath)
	if err != nil {
		logger.Fatal("loading JWT signing key", zap.Error(err))
	}

	signer := jwtauth.NewSigner(privateKey, cfg.JWTKeyID, cfg.JWTIssuer, cfg.JWTAudience)

	// Auth Service verifies its own tokens (e.g. for /logout-all) straight
	// from the in-memory key rather than fetching its own JWKS over HTTP —
	// every other service uses jwtauth.NewVerifier against Auth's published
	// JWKS instead (docs/10-authentication-authorization.md §4).
	verifier := jwtauth.NewVerifierFromPublicKey(&privateKey.PublicKey, cfg.JWTIssuer, cfg.JWTAudience)

	userRepo := postgres.NewUserRepository(db)
	refreshTokenRepo := postgres.NewRefreshTokenRepository(db)
	emailVerificationRepo := postgres.NewVerificationTokenRepository(db, "email_verification_tokens")
	passwordResetRepo := postgres.NewVerificationTokenRepository(db, "password_reset_tokens")
	mailer := service.NewConsoleMailer(logger, cfg.FrontendBaseURL)

	authService := service.NewAuthService(userRepo, refreshTokenRepo, emailVerificationRepo, passwordResetRepo, signer, mailer)
	handler := transporthttp.NewHandler(authService, transporthttp.CookieConfig{Secure: cfg.CookieSecure()})

	router := transporthttp.NewRouter(transporthttp.RouterDeps{
		Handler:        handler,
		Verifier:       verifier,
		DB:             db,
		PublicKey:      &privateKey.PublicKey,
		KeyID:          cfg.JWTKeyID,
		Logger:         logger,
		FrontendOrigin: cfg.FrontendBaseURL,
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("auth-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
}
