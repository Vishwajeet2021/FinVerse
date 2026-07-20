package postgres

import (
	"errors"

	"github.com/finverse/account-service/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

const pgUniqueViolation = "23505"

// mapUniqueViolation maps a unique-constraint violation on
// accounts.account_number to domain.ErrAccountNumberCollision, per
// internal/service/account_number.go's documented retry-on-collision design.
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return domain.ErrAccountNumberCollision
	}
	return nil
}
