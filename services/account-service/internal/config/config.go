package config

import "os"

type Config struct {
	Port                       string
	DatabaseURL                string
	RedisAddr                  string
	TransactionServiceGRPCAddr string
	CustomerServiceURL         string
	AuthJWKSURL                string
	AuthJWTIssuer              string
	AuthJWTAudience            string
	FrontendBaseURL            string
	LogLevel                   string
	Environment                string
}

func Load() Config {
	return Config{
		Port:                       getEnv("PORT", "8083"),
		DatabaseURL:                getEnv("ACCOUNT_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/account_db?sslmode=disable"),
		RedisAddr:                  getEnv("REDIS_ADDR", "localhost:6379"),
		TransactionServiceGRPCAddr: getEnv("TRANSACTION_SERVICE_GRPC_ADDR", "localhost:9091"),
		CustomerServiceURL:         getEnv("CUSTOMER_SERVICE_URL", "http://localhost:8082"),
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
