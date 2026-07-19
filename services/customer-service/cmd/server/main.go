// Customer Service entrypoint. This file is wiring only — no business
// logic — per docs/12-coding-standards.md §1.1.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/finverse/customer-service/internal/config"
	"github.com/finverse/customer-service/internal/migrate"
	"github.com/finverse/customer-service/internal/repository/localstorage"
	"github.com/finverse/customer-service/internal/repository/postgres"
	"github.com/finverse/customer-service/internal/service"
	transporthttp "github.com/finverse/customer-service/internal/transport/http"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	cfg := config.Load()

	level := zapcore.InfoLevel
	if cfg.LogLevel == "debug" {
		level = zapcore.DebugLevel
	}
	logger, err := sharedlogging.New("customer-service", level)
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	if err := migrate.Up(cfg.DatabaseURL); err != nil {
		logger.Fatal("applying migrations", zap.Error(err))
	}

	db, err := postgres.NewDB(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("connecting to database", zap.Error(err))
	}

	panKey, generated, err := service.ResolvePANKey(cfg.PANEncryptionKeyB64)
	if err != nil {
		logger.Fatal("resolving PAN encryption key", zap.Error(err))
	}
	if generated {
		// See internal/service/pan_crypto.go: this is a documented
		// dev-only convenience. In staging/prod CUSTOMER_PAN_ENCRYPTION_KEY
		// must be set from Secrets Manager (docs/11-security-standards.md §3) —
		// a generated key here is never persisted and changes every restart,
		// which would make previously encrypted PAN data permanently
		// unreadable.
		logger.Warn("CUSTOMER_PAN_ENCRYPTION_KEY not set; generated an ephemeral in-memory key for this process only — set a stable 32-byte base64 key outside development")
	}
	panCipher, err := service.NewPANCipher(panKey)
	if err != nil {
		logger.Fatal("initializing PAN cipher", zap.Error(err))
	}

	// Customer Service verifies tokens against Auth Service's published
	// JWKS (docs/10-authentication-authorization.md §4) — unlike Auth
	// Service itself, which uses jwtauth.NewVerifierFromPublicKey for its
	// own routes (see shared/go/jwtauth/verifier.go's comment on why that
	// constructor is Auth's special case only).
	//
	// NewVerifierWithRetry, not NewVerifier: Auth Service being a few
	// hundred ms behind on startup (Docker Compose cold start) or mid-deploy
	// (ECS rolling update) must not crash-loop every service that depends on
	// its JWKS, per the retry-with-backoff pattern in
	// docs/05-system-architecture.md §7.
	ctx, cancelVerifierInit := context.WithTimeout(context.Background(), 60*time.Second)
	verifier, err := jwtauth.NewVerifierWithRetry(ctx, cfg.AuthJWKSURL, cfg.AuthJWTIssuer, cfg.AuthJWTAudience, 20, 3*time.Second)
	cancelVerifierInit()
	if err != nil {
		logger.Fatal("initializing JWT verifier", zap.Error(err))
	}

	customerRepo := postgres.NewCustomerRepository(db)
	documentRepo := postgres.NewDocumentRepository(db)
	beneficiaryRepo := postgres.NewBeneficiaryRepository(db)
	storage := localstorage.NewLocalDiskStorage(cfg.DocumentsStoragePath)

	customerService := service.NewCustomerService(customerRepo, documentRepo, beneficiaryRepo, storage, panCipher, cfg.AutoApproveKYC)
	handler := transporthttp.NewHandler(customerService)

	router := transporthttp.NewRouter(transporthttp.RouterDeps{
		Handler:        handler,
		Verifier:       verifier,
		DB:             db,
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
		logger.Info("customer-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
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
