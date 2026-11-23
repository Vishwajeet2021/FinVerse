// Payment Service entrypoint. Wiring only — per docs/12-coding-standards.md §1.1.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	accountclient "github.com/finverse/payment-service/internal/client/account"
	fraudclient "github.com/finverse/payment-service/internal/client/fraud"
	"github.com/finverse/payment-service/internal/client/ledger"
	"github.com/finverse/payment-service/internal/config"
	"github.com/finverse/payment-service/internal/migrate"
	"github.com/finverse/payment-service/internal/repository/postgres"
	"github.com/finverse/payment-service/internal/service"
	transporthttp "github.com/finverse/payment-service/internal/transport/http"
	"github.com/finverse/shared/eventbus"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
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
	logger, err := sharedlogging.New("payment-service", level)
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	shutdownTracing, err := telemetry.Init(context.Background(), "payment-service")
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

	verifierCtx, cancelVerifierInit := context.WithTimeout(context.Background(), 60*time.Second)
	verifier, err := jwtauth.NewVerifierWithRetry(verifierCtx, cfg.AuthJWKSURL, cfg.AuthJWTIssuer, cfg.AuthJWTAudience, 20, 3*time.Second)
	cancelVerifierInit()
	if err != nil {
		logger.Fatal("initializing JWT verifier", zap.Error(err))
	}

	accountClient := accountclient.NewClient(cfg.AccountServiceURL)

	fraudClient, err := fraudclient.NewClient(cfg.FraudServiceGRPCAddr)
	if err != nil {
		logger.Fatal("connecting to fraud-service", zap.Error(err))
	}
	defer func() { _ = fraudClient.Close() }()

	repo := postgres.NewPaymentRepository(db)
	paymentService := service.NewPaymentService(repo, ledgerClient, accountClient, fraudClient, cfg.FraudFailOpen).
		WithRails(service.RailConfig{NeftBatch: time.Duration(cfg.NeftBatchSeconds) * time.Second, FailureRate: cfg.RailFailureRate}).
		WithSchedules(postgres.NewScheduleRepository(db))
	handler := transporthttp.NewHandler(paymentService, accountClient)

	// Pollers: settle due async-rail payments and fire due schedules. Both claim
	// work with leases (FOR UPDATE SKIP LOCKED), so extra replicas are safe.
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-t.C:
				if _, err := paymentService.SettleDue(workerCtx); err != nil {
					logger.Warn("settling due payments", zap.Error(err))
				}
				if _, err := paymentService.RunDueSchedules(workerCtx); err != nil {
					logger.Warn("running due schedules", zap.Error(err))
				}
			}
		}
	}()

	// Payment Service is a pure orchestrator (docs/06-domain-driven-design.md §2.6)
	// — it publishes events via the outbox relay but consumes none itself, unlike
	// Account/Wallet Service, which both maintain a balance projection.
	relay := postgres.NewOutboxRelay(db, publisher, logger)
	relayCtx, cancelRelay := context.WithCancel(context.Background())
	go relay.Run(relayCtx)

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
		logger.Info("payment-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	cancelRelay()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
}
