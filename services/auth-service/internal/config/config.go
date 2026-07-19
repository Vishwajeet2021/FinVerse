// Package config loads Auth Service's runtime configuration from the
// environment. In staging/prod these values are sourced from Secrets
// Manager/Parameter Store and injected as env vars by the ECS task
// definition (docs/14-aws-infrastructure.md §8); locally they come from
// docker/.env (docs/18-development-workflow.md §7).
package config

import "os"

type Config struct {
	Port              string
	DatabaseURL       string
	JWTPrivateKeyPath string
	JWTKeyID          string
	JWTIssuer         string
	JWTAudience       string
	FrontendBaseURL   string
	LogLevel          string
	Environment       string
}

func Load() Config {
	return Config{
		Port:              getEnv("PORT", "8081"),
		DatabaseURL:       getEnv("AUTH_DATABASE_URL", "postgres://finverse:finverse@localhost:5432/auth_db?sslmode=disable"),
		JWTPrivateKeyPath: getEnv("AUTH_JWT_PRIVATE_KEY_PATH", "./keys/auth-jwt-private.pem"),
		JWTKeyID:          getEnv("AUTH_JWT_KID", "auth-dev-key-1"),
		JWTIssuer:         getEnv("AUTH_JWT_ISSUER", "https://auth.finverse.local"),
		JWTAudience:       getEnv("AUTH_JWT_AUDIENCE", "finverse-api"),
		FrontendBaseURL:   getEnv("FRONTEND_BASE_URL", "http://localhost:5173"),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		Environment:       getEnv("ENVIRONMENT", "development"),
	}
}

// CookieSecure implements docs/10-authentication-authorization.md §2's
// "Secure" cookie flag: required once there's a real TLS edge, but must be
// off for local http development or browsers silently drop the cookie.
func (c Config) CookieSecure() bool {
	return c.Environment != "development"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
