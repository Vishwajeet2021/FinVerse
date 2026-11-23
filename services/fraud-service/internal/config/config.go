package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port            string
	GRPCPort        string
	DatabaseURL     string
	RedisAddr       string
	AuthJWKSURL     string
	AuthJWTIssuer   string
	AuthJWTAudience string
	FrontendBaseURL string
	LogLevel        string
	Environment     string

	// Rule thresholds, in minor units (paise). Defaults are product-realistic;
	// the local compose file loosens velocity/daily for load testing.
	SingleTxnLimitMinor  int64
	ReviewThresholdMinor int64
	DailyLimitMinor      int64
	VelocityMaxPerMin    int64

	RTGSSingleTxnLimitMinor int64
	RTGSDailyLimitMinor     int64
}

func Load() Config {
	return Config{
		Port:            getEnv("PORT", "8088"),
		GRPCPort:        getEnv("GRPC_PORT", "9092"),
		DatabaseURL:     getEnv("FRAUD_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/fraud_db?sslmode=disable"),
		RedisAddr:       getEnv("REDIS_ADDR", "localhost:6379"),
		AuthJWKSURL:     getEnv("AUTH_JWKS_URL", "http://localhost:8081/.well-known/jwks.json"),
		AuthJWTIssuer:   getEnv("AUTH_JWT_ISSUER", "https://auth.finverse.local"),
		AuthJWTAudience: getEnv("AUTH_JWT_AUDIENCE", "finverse-api"),
		FrontendBaseURL: getEnv("FRONTEND_BASE_URL", "http://localhost:5173"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		Environment:     getEnv("ENVIRONMENT", "development"),

		SingleTxnLimitMinor:  getInt("FRAUD_SINGLE_TXN_LIMIT_MINOR", 5_000_000), // ₹50,000
		ReviewThresholdMinor: getInt("FRAUD_REVIEW_THRESHOLD_MINOR", 2_500_000), // ₹25,000
		DailyLimitMinor:      getInt("FRAUD_DAILY_LIMIT_MINOR", 10_000_000),     // ₹1,00,000
		VelocityMaxPerMin:    getInt("FRAUD_VELOCITY_MAX_PER_MIN", 10),

		RTGSSingleTxnLimitMinor: getInt("FRAUD_RTGS_SINGLE_TXN_LIMIT_MINOR", 100_000_000), // ₹10,00,000
		RTGSDailyLimitMinor:     getInt("FRAUD_RTGS_DAILY_LIMIT_MINOR", 1_000_000_000),    // ₹1,00,00,000
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}
