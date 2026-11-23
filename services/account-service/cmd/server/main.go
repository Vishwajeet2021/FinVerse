// Account Service entrypoint. Wiring only — per docs/12-coding-standards.md §1.1.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/finverse/account-service/internal/client/customer"
	"github.com/finverse/account-service/internal/client/ledger"
	"github.com/finverse/account-service/internal/config"
	"github.com/finverse/account-service/internal/migrate"
	"github.com/finverse/account-service/internal/repository/postgres"
	"github.com/finverse/account-service/internal/service"
	transporthttp "github.com/finverse/account-service/internal/transport/http"
	"github.com/finverse/shared/eventbus"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	cfg := config.Load()

	level := zapcore.InfoLevel
	if cfg.LogLevel == "debug" {
		level = zapcore.DebugLevel
	}
	logger, err := sharedlogging.New("account-service", level)
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	shutdownTracing, err := telemetry.Init(context.Background(), "account-service")
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

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	publisher := eventbus.NewPublisher(redisClient)

	ledgerClient, err := ledger.NewClient(cfg.TransactionServiceGRPCAddr)
	if err != nil {
		logger.Fatal("connecting to transaction-service", zap.Error(err))
	}
	defer func() { _ = ledgerClient.Close() }()

	// See shared/go/jwtauth/verifier.go: retry-with-backoff so a Compose
	// cold start (Auth Service a moment behind) doesn't crash-loop this service.
	verifierCtx, cancelVerifierInit := context.WithTimeout(context.Background(), 60*time.Second)
	verifier, err := jwtauth.NewVerifierWithRetry(verifierCtx, cfg.AuthJWKSURL, cfg.AuthJWTIssuer, cfg.AuthJWTAudience, 20, 3*time.Second)
	cancelVerifierInit()
	if err != nil {
		logger.Fatal("initializing JWT verifier", zap.Error(err))
	}

	customerClient := customer.NewClient(cfg.CustomerServiceURL)

	repo := postgres.NewAccountRepository(db)
	accountService := service.NewAccountService(repo, ledgerClient)
	handler := transporthttp.NewHandler(accountService, customerClient)

	relay := postgres.NewOutboxRelay(db, publisher, logger)
	relayCtx, cancelRelay := context.WithCancel(context.Background())
	go relay.Run(relayCtx)

	// Consumes TransactionPosted to keep balance_projection in sync
	// (docs/05-system-architecture.md §5) — see
	// internal/service/account_service.go's ConsumeTransactionPosted for the
	// idempotency guarantee this depends on.
	consumer := eventbus.NewConsumer(redisClient, "TransactionPosted", "account-service", uuid.NewString(), logger)
	consumerCtx, cancelConsumer := context.WithCancel(context.Background())
	go func() {
		err := consumer.Run(consumerCtx, func(ctx context.Context, env eventbus.Envelope) error {
			var payload service.TransactionPostedPayload
			if err := json.Unmarshal(env.Data, &payload); err != nil {
				return err
			}
			return accountService.ConsumeTransactionPosted(ctx, env.EventID, payload)
		})
		if err != nil && consumerCtx.Err() == nil {
			logger.Error("TransactionPosted consumer stopped unexpectedly", zap.Error(err))
		}
	}()

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
		logger.Info("account-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	cancelRelay()
	cancelConsumer()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
}
