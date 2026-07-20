package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

const pgUniqueViolation = "23505"

// mapUniqueViolation catches the rare race where two concurrent requests
// with the same (source_service, idempotency_key) both pass the
// service-layer GetByIdempotencyKey check before either commits — the
// UNIQUE constraint is the actual guarantee against a double-post; this
// just turns the resulting Postgres error into a clearer message. The
// caller is expected to retry (re-fetch by idempotency key) on this error;
// a resilient inline retry loop is a documented hardening gap for Phase 3,
// not implemented here.
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return fmt.Errorf("idempotency key already in use by a concurrent request, retry by re-fetching: %w", err)
	}
	return nil
}
