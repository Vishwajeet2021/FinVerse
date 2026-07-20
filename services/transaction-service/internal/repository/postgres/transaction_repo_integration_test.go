//go:build integration

// Integration tests against a real Postgres (testcontainers), per
// docs/17-testing-strategy.md §3: mocking the database would prove nothing
// about the one invariant that matters most here — that the database
// itself, not just application code, refuses to let an unbalanced set of
// legs commit. Run with: go test -tags=integration ./...
package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/finverse/transaction-service/db/migrations"
	"github.com/finverse/transaction-service/internal/domain"
	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func setupTestDB(t *testing.T) *TransactionRepository {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("transaction_test"),
		tcpostgres.WithUsername("finverse"),
		tcpostgres.WithPassword("finverse"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting connection string: %v", err)
	}

	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatalf("loading migrations: %v", err)
	}
	mdb, err := (&migratepostgres.Postgres{}).Open(dsn)
	if err != nil {
		t.Fatalf("opening migration connection: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "postgres", mdb)
	if err != nil {
		t.Fatalf("creating migrator: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("applying migrations: %v", err)
	}

	db, err := NewDB(dsn)
	if err != nil {
		t.Fatalf("connecting gorm: %v", err)
	}
	return NewTransactionRepository(db)
}

func TestIntegration_BalancedLegsCommitSuccessfully(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	repo := setupTestDB(t)
	ctx := context.Background()

	txn := &domain.Transaction{
		ID:             uuid.NewString(),
		IdempotencyKey: "test-key-1",
		Status:         domain.TransactionStatusPosted,
		SourceService:  "integration-test",
		CreatedAt:      time.Now(),
		Legs: []domain.Leg{
			{ID: uuid.NewString(), ParticipantID: uuid.NewString(), ParticipantType: domain.ParticipantAccount, AmountMinor: 500_00, Direction: domain.DirectionDebit, Currency: "INR"},
			{ID: uuid.NewString(), ParticipantID: uuid.NewString(), ParticipantType: domain.ParticipantAccount, AmountMinor: 500_00, Direction: domain.DirectionCredit, Currency: "INR"},
		},
	}
	for i := range txn.Legs {
		txn.Legs[i].TransactionID = txn.ID
	}

	if err := repo.CreateWithOutbox(ctx, txn, "TransactionPosted", map[string]string{"test": "true"}); err != nil {
		t.Fatalf("balanced transaction should commit, got error: %v", err)
	}

	loaded, err := repo.GetByID(ctx, txn.ID)
	if err != nil {
		t.Fatalf("loading committed transaction: %v", err)
	}
	if len(loaded.Legs) != 2 {
		t.Fatalf("expected 2 legs to have committed, got %d", len(loaded.Legs))
	}
}

// TestIntegration_DatabaseRejectsUnbalancedLegs is the single most important
// test in this codebase: it proves the double-entry invariant is enforced
// by Postgres itself, not merely by application code that a future bug
// could bypass. It calls CreateWithOutbox directly — skipping
// LedgerService.PostTransaction's own Validate() pre-check entirely — so a
// passing test can only mean the database-level constraint trigger did its job.
func TestIntegration_DatabaseRejectsUnbalancedLegs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	repo := setupTestDB(t)
	ctx := context.Background()

	txn := &domain.Transaction{
		ID:             uuid.NewString(),
		IdempotencyKey: "test-key-2",
		Status:         domain.TransactionStatusPosted,
		SourceService:  "integration-test",
		CreatedAt:      time.Now(),
		Legs: []domain.Leg{
			{ID: uuid.NewString(), ParticipantID: uuid.NewString(), ParticipantType: domain.ParticipantAccount, AmountMinor: 500_00, Direction: domain.DirectionDebit, Currency: "INR"},
			{ID: uuid.NewString(), ParticipantID: uuid.NewString(), ParticipantType: domain.ParticipantAccount, AmountMinor: 499_00, Direction: domain.DirectionCredit, Currency: "INR"}, // deliberately off by 1 rupee
		},
	}
	for i := range txn.Legs {
		txn.Legs[i].TransactionID = txn.ID
	}

	err := repo.CreateWithOutbox(ctx, txn, "TransactionPosted", map[string]string{"test": "true"})
	if err == nil {
		t.Fatal("expected the database to reject unbalanced legs, but the write succeeded")
	}

	// And the rejection must have rolled back the ENTIRE transaction —
	// no orphaned transaction row, no orphaned legs, no orphaned outbox
	// event. Partial commits here would be worse than an outright failure.
	if _, err := repo.GetByID(ctx, txn.ID); err != domain.ErrTransactionNotFound {
		t.Fatalf("expected the failed transaction to not exist at all, got err=%v", err)
	}
}
