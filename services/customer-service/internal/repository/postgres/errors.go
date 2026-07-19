package postgres

import (
	"errors"

	"github.com/finverse/customer-service/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

const pgUniqueViolation = "23505"

// mapUniqueViolation translates a Postgres unique-constraint violation into
// the specific domain error the caller should see, based on which
// constraint fired. Any other error is returned unchanged for the caller to
// wrap with fmt.Errorf per docs/12-coding-standards.md §1.3.
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		switch pgErr.ConstraintName {
		case "customers_user_id_key":
			return domain.ErrProfileAlreadyExists
		}
	}
	return err
}
