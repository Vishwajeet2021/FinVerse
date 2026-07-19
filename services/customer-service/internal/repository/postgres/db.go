// Package postgres implements Customer Service's repository interfaces
// (internal/service/interfaces.go) against Postgres via GORM, per
// docs/08-database-design.md §6. Its models carry gorm tags — the domain
// package (internal/domain) deliberately does not, per
// docs/12-coding-standards.md §1.1.
package postgres

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewDB opens a connection pool to the Customer Service database. Migrations
// are applied separately (internal/migrate), never via GORM AutoMigrate —
// schema changes go through versioned SQL files per
// docs/08-database-design.md §15.
func NewDB(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), // request logging is handled by shared/go/logging, not GORM's own logger
	})
}
