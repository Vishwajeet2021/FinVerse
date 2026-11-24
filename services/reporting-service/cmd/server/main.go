// Reporting Service entrypoint. Wiring only — per docs/12-coding-standards.md section 1.1.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/finverse/reporting-service/internal/client"
	"github.com/finverse/reporting-service/internal/config"
	"github.com/finverse/reporting-service/internal/migrate"
	"github.com/finverse/reporting-service/internal/repository/postgres"
	"github.com/finverse/reporting-service/internal/service"
	"github.com/finverse/reporting-service/internal/storage"
	transporthttp "github.com/finverse/reporting-service/internal/transport/http"
	"github.com/finverse/shared/jwtauth"
	sharedlogging "github.com/finverse/shared/logging"
	"github.com/finverse/shared/ratelimit"
	"github.com/finverse/shared/svcauth"
	"github.com/finverse/shared/telemetry"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const serviceName = "reporting-service"

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

	ledger, err := client.NewLedger(cfg.LedgerGRPCAddr, svcauth.NewSigner(serviceName, svcKey))
	if err != nil {
		logger.Fatal("ledger client", zap.Error(err))
	}
	defer ledger.Close()
	store, err := storage.NewLocal(cfg.ReportDir)
	if err != nil {
		logger.Fatal("report storage", zap.Error(err))
	}

	sugar := logger.Sugar()
	svc := service.New(postgres.NewRepository(db), store, ledger, client.NewAccounts(cfg.AccountURL), client.NewFraud(cfg.FraudURL),
		func(msg string, kv ...any) { sugar.Warnw(msg, kv...) })

	workerCtx, cancelWorker := context.WithCancel(context.Background())
	go runWorker(workerCtx, svc, logger)

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	perMin := int64(10)
	if v, err := strconv.ParseInt(os.Getenv("RATE_LIMIT_REPORTS_PER_MIN"), 10, 64); err == nil && v > 0 {
		perMin = v
	}
	router := transporthttp.NewRouter(transporthttp.RouterDeps{
		Handler: transporthttp.NewHandler(svc), Verifier: verifier, DB: db, Logger: logger,
		FrontendOrigin: cfg.FrontendBaseURL, Limiter: ratelimit.New(redisClient), RequestsPerMin: perMin,
	})
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		logger.Info("reporting-service listening", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	cancelWorker()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// runWorker builds queued reports one at a time and sweeps expired files. It drains the queue
// back to back, and polls once a second when idle.
func runWorker(ctx context.Context, svc *service.Service, logger *zap.Logger) {
	idle := time.NewTicker(time.Second)
	defer idle.Stop()
	sweep := time.NewTicker(time.Minute)
	defer sweep.Stop()
	for {
		for {
			did, err := svc.ProcessNext(ctx)
			if err != nil {
				logger.Error("report worker pass failed", zap.Error(err))
				break
			}
			if !did || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-sweep.C:
			if n, err := svc.Sweep(ctx); err != nil {
				logger.Error("report sweep failed", zap.Error(err))
			} else if n > 0 {
				logger.Info("expired reports removed", zap.Int("count", n))
			}
		case <-idle.C:
		}
	}
}
