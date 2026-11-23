// Transaction Service entrypoint. Wiring only — per docs/12-coding-standards.md §1.1.
package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/finverse/shared/eventbus"
	ledgerv1 "github.com/finverse/shared/genproto/ledger/v1"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/finverse/transaction-service/internal/config"
	"github.com/finverse/transaction-service/internal/migrate"
	"github.com/finverse/transaction-service/internal/repository/postgres"
	"github.com/finverse/transaction-service/internal/service"
	transportgrpc "github.com/finverse/transaction-service/internal/transport/grpc"
	transporthttp "github.com/finverse/transaction-service/internal/transport/http"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
)

func main() {
	cfg := config.Load()

	level := zapcore.InfoLevel
	if cfg.LogLevel == "debug" {
		level = zapcore.DebugLevel
	}
	logger, err := sharedlogging.New("transaction-service", level)
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	shutdownTracing, err := telemetry.Init(context.Background(), "transaction-service")
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

	repo := postgres.NewTransactionRepository(db)
	ledger := service.NewLedgerService(repo)

	relay := postgres.NewOutboxRelay(db, publisher, logger)
	relayCtx, cancelRelay := context.WithCancel(context.Background())
	go relay.Run(relayCtx)

	// gRPC server — the sync path Payment/Wallet/Account Service call
	// (docs/05-system-architecture.md §4).
	grpcServer := grpc.NewServer(grpc.StatsHandler(telemetry.GRPCServerStats()), grpc.ChainUnaryInterceptor(sharedlogging.UnaryServerInterceptor(logger)))
	ledgerv1.RegisterLedgerServiceServer(grpcServer, transportgrpc.NewServer(ledger))
	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		logger.Fatal("listening for gRPC", zap.Error(err))
	}
	go func() {
		logger.Info("transaction-service gRPC listening", zap.String("port", cfg.GRPCPort))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	// HTTP server — health/readiness + the daily-closing trigger only.
	router := transporthttp.NewRouter(ledger, db, logger)
	httpServer := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		logger.Info("transaction-service HTTP listening", zap.String("port", cfg.HTTPPort), zap.String("environment", cfg.Environment))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("HTTP server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	cancelRelay()
	grpcServer.GracefulStop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful HTTP shutdown failed", zap.Error(err))
	}
}
