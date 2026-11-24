package config

import "os"

type Config struct {
	Port            string
	DatabaseURL     string
	RedisAddr       string
	AuthJWKSURL     string
	AuthJWTIssuer   string
	AuthJWTAudience string
	FrontendBaseURL string
	AccountURL      string
	FraudURL        string
	CustomerURL     string
	PrometheusURL   string
	LogLevel        string
	Environment     string
}

func Load() Config {
	return Config{
		Port:            getEnv("PORT", "8089"),
		DatabaseURL:     getEnv("ADMIN_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/admin_db?sslmode=disable"),
		RedisAddr:       getEnv("REDIS_ADDR", "localhost:6379"),
		AuthJWKSURL:     getEnv("AUTH_JWKS_URL", "http://localhost:8081/.well-known/jwks.json"),
		AuthJWTIssuer:   getEnv("AUTH_JWT_ISSUER", "https://auth.finverse.local"),
		AuthJWTAudience: getEnv("AUTH_JWT_AUDIENCE", "finverse-api"),
		FrontendBaseURL: getEnv("FRONTEND_BASE_URL", "http://localhost:5173"),
		AccountURL:      getEnv("ACCOUNT_SERVICE_URL", "http://localhost:8083"),
		FraudURL:        getEnv("FRAUD_SERVICE_URL", "http://localhost:8088"),
		CustomerURL:     getEnv("CUSTOMER_SERVICE_URL", "http://localhost:8082"),
		PrometheusURL:   getEnv("PROMETHEUS_URL", "http://localhost:9090"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		Environment:     getEnv("ENVIRONMENT", "development"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
