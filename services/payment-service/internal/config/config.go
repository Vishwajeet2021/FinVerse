package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port                       string
	DatabaseURL                string
	RedisAddr                  string
	TransactionServiceGRPCAddr string
	AccountServiceURL          string
	FraudServiceGRPCAddr       string
	FraudFailOpen              bool
	RailFailureRate            float64
	NeftBatchSeconds           int
	AuthJWKSURL                string
	AuthJWTIssuer              string
	AuthJWTAudience            string
	FrontendBaseURL            string
	LogLevel                   string
	Environment                string
}

func Load() Config {
	return Config{
		Port:                       getEnv("PORT", "8085"),
		DatabaseURL:                getEnv("PAYMENT_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/payment_db?sslmode=disable"),
		RedisAddr:                  getEnv("REDIS_ADDR", "localhost:6379"),
		TransactionServiceGRPCAddr: getEnv("TRANSACTION_SERVICE_GRPC_ADDR", "localhost:9091"),
		AccountServiceURL:          getEnv("ACCOUNT_SERVICE_URL", "http://localhost:8083"),
		FraudServiceGRPCAddr:       getEnv("FRAUD_SERVICE_GRPC_ADDR", "localhost:9092"),
		FraudFailOpen:              getEnv("FRAUD_FAIL_OPEN", "false") == "true",
		RailFailureRate:            getEnvFloat("RAIL_FAILURE_RATE", 0),
		NeftBatchSeconds:           int(getEnvFloat("NEFT_BATCH_SECONDS", 30)),
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

func getEnvFloat(key string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return fallback
}
