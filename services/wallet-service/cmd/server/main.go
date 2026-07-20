// Wallet Service entrypoint. Wiring only — per docs/12-coding-standards.md §1.1.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/finverse/shared/eventbus"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/wallet-service/internal/client/ledger"
	"github.com/finverse/wallet-service/internal/config"
	"github.com/finverse/wallet-service/internal/migrate"
	"github.com/finverse/wallet-service/internal/repository/postgres"
	"github.com/finverse/wallet-service/internal/service"
	transporthttp "github.com/finverse/wallet-service/internal/transport/http"
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
	logger, err := sharedlogging.New("wallet-service", level)
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

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	publisher := eventbus.NewPublisher(redisClient)

	ledgerClient, err := ledger.NewClient(cfg.TransactionServiceGRPCAddr)
	if err != nil {
		logger.Fatal("connecting to transaction-service", zap.Error(err))
	}
	defer func() { _ = ledgerClient.Close() }()

	verifierCtx, cancelVerifierInit := context.WithTimeout(context.Background(), 60*time.Second)
	verifier, err := jwtauth.NewVerifierWithRetry(verifierCtx, cfg.AuthJWKSURL, cfg.AuthJWTIssuer, cfg.AuthJWTAudience, 20, 3*time.Second)
	cancelVerifierInit()
	if err != nil {
		logger.Fatal("initializing JWT verifier", zap.Error(err))
	}

	repo := postgres.NewWalletRepository(db)
	walletService := service.NewWalletService(repo, ledgerClient)
	handler := transporthttp.NewHandler(walletService)

	relay := postgres.NewOutboxRelay(db, publisher, logger)
	relayCtx, cancelRelay := context.WithCancel(context.Background())
	go relay.Run(relayCtx)

	consumer := eventbus.NewConsumer(redisClient, "TransactionPosted", "wallet-service", uuid.NewString(), logger)
	consumerCtx, cancelConsumer := context.WithCancel(context.Background())
	go func() {
		err := consumer.Run(consumerCtx, func(ctx context.Context, env eventbus.Envelope) error {
			var payload service.TransactionPostedPayload
			if err := json.Unmarshal(env.Data, &payload); err != nil {
				return err
			}
			return walletService.ConsumeTransactionPosted(ctx, env.EventID, payload)
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
		logger.Info("wallet-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
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
