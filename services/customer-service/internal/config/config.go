// Package config loads Customer Service's runtime configuration from the
// environment. In staging/prod these values are sourced from Secrets
// Manager/Parameter Store and injected as env vars by the ECS task
// definition (docs/14-aws-infrastructure.md §8); locally they come from
// docker/.env (docs/18-development-workflow.md §7).
package config

import "os"

type Config struct {
	Port                 string
	DatabaseURL          string
	AuthJWKSURL          string
	AuthJWTIssuer        string
	AuthJWTAudience      string
	PANEncryptionKeyB64  string
	DocumentsStoragePath string
	AutoApproveKYC       bool
	FrontendBaseURL      string
	LogLevel             string
	Environment          string
}

func Load() Config {
	return Config{
		Port:                 getEnv("PORT", "8082"),
		DatabaseURL:          getEnv("CUSTOMER_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/customer_db?sslmode=disable"),
		AuthJWKSURL:          getEnv("AUTH_JWKS_URL", "http://localhost:8081/.well-known/jwks.json"),
		AuthJWTIssuer:        getEnv("AUTH_JWT_ISSUER", "https://auth.finverse.local"),
		AuthJWTAudience:      getEnv("AUTH_JWT_AUDIENCE", "finverse-api"),
		PANEncryptionKeyB64:  getEnv("CUSTOMER_PAN_ENCRYPTION_KEY", ""),
		DocumentsStoragePath: getEnv("DOCUMENTS_STORAGE_PATH", "./data/documents"),
		AutoApproveKYC:       getEnvBool("AUTO_APPROVE_KYC", true),
		FrontendBaseURL:      getEnv("FRONTEND_BASE_URL", "http://localhost:5173"),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		Environment:          getEnv("ENVIRONMENT", "development"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvBool implements the AUTO_APPROVE_KYC default=true dev convenience
// (docs/19-implementation-roadmap.md Phase 1: "auto-approve in dev") — any
// value other than the literal "false" is treated as true, so the default
// stays on unless an operator deliberately opts out.
func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v != "false"
}
