package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

const pgUniqueViolation = "23505"

// mapUniqueViolation catches a concurrent duplicate-request race on
// (from_account_id, idempotency_key) -- the UNIQUE constraint is the actual
// guarantee against a double-post; this just clarifies the resulting error.
// Same documented pattern as Transaction Service's own repository (see
// services/transaction-service/internal/repository/postgres/errors.go).
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return fmt.Errorf("idempotency key already in use by a concurrent request, retry by re-fetching: %w", err)
	}
	return nil
}
