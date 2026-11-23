// Audit Service entrypoint. Wiring only — per docs/12-coding-standards.md section 1.1.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/finverse/audit-service/internal/config"
	"github.com/finverse/audit-service/internal/migrate"
	"github.com/finverse/audit-service/internal/repository/postgres"
	"github.com/finverse/audit-service/internal/service"
	transporthttp "github.com/finverse/audit-service/internal/transport/http"
	"github.com/finverse/shared/eventbus"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const serviceName = "audit-service"

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
	svc := service.New(postgres.NewRepository(db))

	consumerCtx, cancelConsumers := context.WithCancel(context.Background())
	for _, stream := range service.Streams {
		cons := eventbus.NewConsumer(redisClient, stream, serviceName, uuid.NewString(), logger)
		go func(stream string) {
			if err := cons.Run(consumerCtx, svc.Handle); err != nil && consumerCtx.Err() == nil {
				logger.Error("consumer stopped", zap.String("stream", stream), zap.Error(err))
			}
		}(stream)
	}

	router := transporthttp.NewRouter(transporthttp.RouterDeps{
		Service: svc, Verifier: verifier, DB: db, Logger: logger, FrontendOrigin: cfg.FrontendBaseURL,
	})
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() {
		logger.Info("audit-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	cancelConsumers()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
