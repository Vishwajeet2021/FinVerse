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
	LogLevel        string
	Environment     string
	// MaxDeliveries / backoff for the event consumers. Overridable so the
	// DLQ demo and load tests can shorten the retry schedule.
	MaxDeliveries int64
	BackoffBaseMs int64
	BackoffMaxMs  int64
}

func Load() Config {
	return Config{
		Port:            getEnv("PORT", "8086"),
		DatabaseURL:     getEnv("NOTIFICATION_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/notification_db?sslmode=disable"),
		RedisAddr:       getEnv("REDIS_ADDR", "localhost:6379"),
		AuthJWKSURL:     getEnv("AUTH_JWKS_URL", "http://localhost:8081/.well-known/jwks.json"),
		AuthJWTIssuer:   getEnv("AUTH_JWT_ISSUER", "https://auth.finverse.local"),
		AuthJWTAudience: getEnv("AUTH_JWT_AUDIENCE", "finverse-api"),
		FrontendBaseURL: getEnv("FRONTEND_BASE_URL", "http://localhost:5173"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		Environment:     getEnv("ENVIRONMENT", "development"),
		MaxDeliveries:   getInt("NOTIFY_MAX_DELIVERIES", 5),
		BackoffBaseMs:   getInt("NOTIFY_BACKOFF_BASE_MS", 2000),
		BackoffMaxMs:    getInt("NOTIFY_BACKOFF_MAX_MS", 60000),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int64
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return fallback
		}
		n = n*10 + int64(ch-'0')
	}
	return n
}
