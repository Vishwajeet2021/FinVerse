// Package migrate applies Auth Service's embedded SQL migrations at startup.
// Migrations are forward-only in shared environments per
// docs/08-database-design.md §15 and docs/15-cicd-pipeline.md §7 — this
// runner never rolls back automatically, only Up().
package migrate

import (
	"errors"
	"fmt"

	"github.com/finverse/auth-service/db/migrations"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Up applies every pending migration. databaseURL must be a plain
// postgres:// DSN (the golang-migrate postgres driver parses it itself,
// separately from the GORM connection used for queries).
func Up(databaseURL string) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("loading embedded migrations: %w", err)
	}

	db, err := (&postgres.Postgres{}).Open(databaseURL)
	if err != nil {
		return fmt.Errorf("opening migration connection: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", db)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}
