package config

import "os"

type Config struct {
	HTTPPort    string
	GRPCPort    string
	DatabaseURL string
	RedisAddr   string
	LogLevel    string
	Environment string
}

func Load() Config {
	return Config{
		HTTPPort:    getEnv("PORT", "8091"),
		GRPCPort:    getEnv("GRPC_PORT", "9091"),
		DatabaseURL: getEnv("TRANSACTION_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/transaction_db?sslmode=disable"),
		RedisAddr:   getEnv("REDIS_ADDR", "localhost:6379"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		Environment: getEnv("ENVIRONMENT", "development"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
