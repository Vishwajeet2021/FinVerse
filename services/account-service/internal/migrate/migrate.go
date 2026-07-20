// Package migrate applies Account Service's embedded SQL migrations at
// startup, per docs/08-database-design.md §15.
package migrate

import (
	"errors"
	"fmt"

	"github.com/finverse/account-service/db/migrations"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

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
