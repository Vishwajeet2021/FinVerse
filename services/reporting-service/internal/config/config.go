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
	LedgerGRPCAddr  string
	AccountURL      string
	FraudURL        string
	ReportDir       string
	LogLevel        string
	Environment     string
}

func Load() Config {
	return Config{
		Port:            getEnv("PORT", "8090"),
		DatabaseURL:     getEnv("REPORTING_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/reporting_db?sslmode=disable"),
		RedisAddr:       getEnv("REDIS_ADDR", "localhost:6379"),
		AuthJWKSURL:     getEnv("AUTH_JWKS_URL", "http://localhost:8081/.well-known/jwks.json"),
		AuthJWTIssuer:   getEnv("AUTH_JWT_ISSUER", "https://auth.finverse.local"),
		AuthJWTAudience: getEnv("AUTH_JWT_AUDIENCE", "finverse-api"),
		FrontendBaseURL: getEnv("FRONTEND_BASE_URL", "http://localhost:5173"),
		LedgerGRPCAddr:  getEnv("TRANSACTION_SERVICE_GRPC_ADDR", "localhost:9091"),
		AccountURL:      getEnv("ACCOUNT_SERVICE_URL", "http://localhost:8083"),
		FraudURL:        getEnv("FRAUD_SERVICE_URL", "http://localhost:8088"),
		ReportDir:       getEnv("REPORT_DIR", "/var/lib/finverse/reports"),
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
