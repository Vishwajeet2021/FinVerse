// Package config loads Wallet Service's runtime configuration from the
// environment. In staging/prod these values are sourced from Secrets
// Manager/Parameter Store and injected as env vars by the ECS task
// definition (docs/14-aws-infrastructure.md §8); locally they come from
// docker/.env (docs/18-development-workflow.md §7).
package config

import "os"

type Config struct {
	Port                       string
	DatabaseURL                string
	RedisAddr                  string
	TransactionServiceGRPCAddr string
	AuthJWKSURL                string
	AuthJWTIssuer              string
	AuthJWTAudience            string
	FrontendBaseURL            string
	LogLevel                   string
	Environment                string
}

func Load() Config {
	return Config{
		Port:                       getEnv("PORT", "8084"),
		DatabaseURL:                getEnv("WALLET_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/wallet_db?sslmode=disable"),
		RedisAddr:                  getEnv("REDIS_ADDR", "localhost:6379"),
		TransactionServiceGRPCAddr: getEnv("TRANSACTION_SERVICE_GRPC_ADDR", "localhost:9091"),
		AuthJWKSURL:                getEnv("AUTH_JWKS_URL", "http://localhost:8081/.well-known/jwks.json"),
		AuthJWTIssuer:              getEnv("AUTH_JWT_ISSUER", "https://auth.finverse.local"),
		AuthJWTAudience:            getEnv("AUTH_JWT_AUDIENCE", "finverse-api"),
		FrontendBaseURL:            getEnv("FRONTEND_BASE_URL", "http://localhost:5173"),
		LogLevel:                   getEnv("LOG_LEVEL", "info"),
		Environment:                getEnv("ENVIRONMENT", "development"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
