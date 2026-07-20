package postgres

import (
	"errors"

	"github.com/finverse/wallet-service/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

const pgUniqueViolation = "23505"

// mapUniqueViolation maps a unique-constraint violation on
// wallets.customer_id to domain.ErrWalletAlreadyExists -- this is the actual
// non-bypassable guarantee against a concurrent double-provision race
// (internal/service/interfaces.go's comment on CreateWalletWithProjection).
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return domain.ErrWalletAlreadyExists
	}
	return nil
}
