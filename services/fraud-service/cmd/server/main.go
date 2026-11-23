// Fraud Service entrypoint. Wiring only — per docs/12-coding-standards.md §1.1.
package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/finverse/fraud-service/internal/config"
	"github.com/finverse/fraud-service/internal/migrate"
	"github.com/finverse/fraud-service/internal/repository/postgres"
	"github.com/finverse/fraud-service/internal/rules"
	"github.com/finverse/fraud-service/internal/service"
	transportgrpc "github.com/finverse/fraud-service/internal/transport/grpc"
	transporthttp "github.com/finverse/fraud-service/internal/transport/http"
	"github.com/finverse/shared/eventbus"
	fraudv1 "github.com/finverse/shared/genproto/fraud/v1"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
)

const serviceName = "fraud-service"

func main() {
	cfg := config.Load()

	level := zapcore.InfoLevel
	if cfg.LogLevel == "debug" {
		level = zapcore.DebugLevel
	}
	logger, err := sharedlogging.New(serviceName, level)
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	shutdownTracing, err := telemetry.Init(context.Background(), serviceName)
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

	verifierCtx, cancelVerifierInit := context.WithTimeout(context.Background(), 60*time.Second)
	verifier, err := jwtauth.NewVerifierWithRetry(verifierCtx, cfg.AuthJWKSURL, cfg.AuthJWTIssuer, cfg.AuthJWTAudience, 20, 3*time.Second)
	cancelVerifierInit()
	if err != nil {
		logger.Fatal("initializing JWT verifier", zap.Error(err))
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	engine := rules.NewEngine(rules.Config{
		SingleTxnLimitMinor: cfg.SingleTxnLimitMinor, ReviewThresholdMinor: cfg.ReviewThresholdMinor,
		DailyLimitMinor: cfg.DailyLimitMinor, VelocityMaxPerMin: cfg.VelocityMaxPerMin,
		RTGSSingleTxnLimitMinor: cfg.RTGSSingleTxnLimitMinor, RTGSDailyLimitMinor: cfg.RTGSDailyLimitMinor,
	}, rules.NewRedisCounters(redisClient))

	repo := postgres.NewRepository(db)
	svc := service.New(engine, repo, logger)

	bgCtx, cancelBg := context.WithCancel(context.Background())
	go svc.RunCaseWriter(bgCtx)
	go postgres.NewOutboxRelay(db, eventbus.NewPublisher(redisClient), logger).Run(bgCtx)

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		logger.Fatal("listening for gRPC", zap.Error(err))
	}
	grpcServer := grpc.NewServer(grpc.StatsHandler(telemetry.GRPCServerStats()))
	fraudv1.RegisterFraudServiceServer(grpcServer, transportgrpc.NewServer(svc))
	go func() {
		logger.Info("fraud-service gRPC listening", zap.String("port", cfg.GRPCPort))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	router := transporthttp.NewRouter(transporthttp.RouterDeps{
		Handler: transporthttp.NewHandler(svc), Verifier: verifier, DB: db, Logger: logger, FrontendOrigin: cfg.FrontendBaseURL,
	})
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() {
		logger.Info("fraud-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	cancelBg()
	grpcServer.GracefulStop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
