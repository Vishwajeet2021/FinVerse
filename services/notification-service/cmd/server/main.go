// Notification Service entrypoint. Wiring only — per docs/12-coding-standards.md §1.1.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/finverse/notification-service/internal/config"
	"github.com/finverse/notification-service/internal/migrate"
	"github.com/finverse/notification-service/internal/render"
	"github.com/finverse/notification-service/internal/repository/postgres"
	"github.com/finverse/notification-service/internal/sender"
	"github.com/finverse/notification-service/internal/service"
	transporthttp "github.com/finverse/notification-service/internal/transport/http"
	"github.com/finverse/shared/eventbus"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/telemetry"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const serviceName = "notification-service"

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
	logSender := sender.NewLogSender(logger)
	svc := service.New(postgres.NewRepository(db), logSender)

	// One consumer (and so one DLQ) per event stream, all in this service's
	// consumer group — the per-service queue + DLQ from
	// docs/07-microservice-specifications.md §11.
	consumerCtx, cancelConsumers := context.WithCancel(context.Background())
	bus := transporthttp.Bus{Consumers: map[string]*eventbus.Consumer{}}
	for _, stream := range render.Streams {
		cons := eventbus.NewConsumer(redisClient, stream, serviceName, uuid.NewString(), logger,
			eventbus.WithMaxDeliveries(cfg.MaxDeliveries),
			eventbus.WithBackoff(time.Duration(cfg.BackoffBaseMs)*time.Millisecond, time.Duration(cfg.BackoffMaxMs)*time.Millisecond))
		bus.Consumers[stream] = cons
		go func(stream string) {
			if err := cons.Run(consumerCtx, svc.Handle); err != nil && consumerCtx.Err() == nil {
				logger.Error("consumer stopped", zap.String("stream", stream), zap.Error(err))
			}
		}(stream)
	}

	router := transporthttp.NewRouter(transporthttp.RouterDeps{
		Handler:        transporthttp.NewHandler(svc, bus, logSender),
		Verifier:       verifier,
		DB:             db,
		Logger:         logger,
		FrontendOrigin: cfg.FrontendBaseURL,
		Environment:    cfg.Environment,
	})

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() {
		logger.Info("notification-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
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
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
	_ = shutdownTracing(shutdownCtx)
}
