// Admin Service entrypoint. Wiring only — per docs/12-coding-standards.md section 1.1.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/finverse/admin-service/internal/client"
	"github.com/finverse/admin-service/internal/config"
	"github.com/finverse/admin-service/internal/migrate"
	"github.com/finverse/admin-service/internal/repository/postgres"
	"github.com/finverse/admin-service/internal/service"
	transporthttp "github.com/finverse/admin-service/internal/transport/http"
	"github.com/finverse/shared/eventbus"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/ratelimit"
	"github.com/finverse/shared/svcauth"
	"github.com/finverse/shared/telemetry"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const serviceName = "admin-service"

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

	svcKey, err := svcauth.KeyFromEnv()
	if err != nil {
		logger.Fatal("service auth key", zap.Error(err))
	}

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
	sugar := logger.Sugar()
	svc := service.New(
		postgres.NewRepository(db),
		client.NewAccounts(cfg.AccountURL, svcauth.NewSigner(serviceName, svcKey)),
		client.NewFraud(cfg.FraudURL),
		client.NewCustomers(cfg.CustomerURL),
		func(msg string, kv ...any) { sugar.Warnw(msg, kv...) },
	)

	relay := postgres.NewOutboxRelay(db, eventbus.NewPublisher(redisClient), logger)
	relayCtx, cancelRelay := context.WithCancel(context.Background())
	go relay.Run(relayCtx)

	actionsPerMin := int64(30)
	if v, err := strconv.ParseInt(os.Getenv("RATE_LIMIT_ADMIN_ACTIONS_PER_MIN"), 10, 64); err == nil && v > 0 {
		actionsPerMin = v
	}

	router := transporthttp.NewRouter(transporthttp.RouterDeps{
		Handler: transporthttp.NewHandler(svc, client.NewPrometheus(cfg.PrometheusURL)), Verifier: verifier, DB: db,
		Logger: logger, FrontendOrigin: cfg.FrontendBaseURL, Limiter: ratelimit.New(redisClient), ActionsPerMin: actionsPerMin,
	})
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second}
	go func() {
		logger.Info("admin-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	cancelRelay()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
