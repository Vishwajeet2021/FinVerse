// Package service implements Transaction Service's business logic — the
// core domain of the whole platform (docs/06-domain-driven-design.md §1).
// It depends only on domain types and TransactionRepository, never on gRPC
// or GORM directly, per docs/12-coding-standards.md §1.1.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/transaction-service/internal/domain"
	"github.com/google/uuid"
)

type LedgerService struct {
	repo TransactionRepository
}

func NewLedgerService(repo TransactionRepository) *LedgerService {
	return &LedgerService{repo: repo}
}

// PostTransaction implements FR-TXN-1 to FR-TXN-3: validate the double-entry
// invariant in application code (fast, clear error), post atomically with
// its outbox event, and treat a repeated idempotency key as "already done"
// rather than double-posting.
func (s *LedgerService) PostTransaction(ctx context.Context, sourceService, idempotencyKey string, legs []domain.Leg) (*domain.Transaction, error) {
	if existing, err := s.repo.GetByIdempotencyKey(ctx, sourceService, idempotencyKey); err == nil && existing != nil {
		return existing, nil // FR-TXN-3: duplicate request returns the original result, no double-post
	} else if err != nil && err != domain.ErrTransactionNotFound {
		return nil, fmt.Errorf("checking idempotency key: %w", err)
	}

	txnID := uuid.NewString()
	for i := range legs {
		legs[i].ID = uuid.NewString()
		legs[i].TransactionID = txnID
	}

	txn := &domain.Transaction{
		ID:             txnID,
		IdempotencyKey: idempotencyKey,
		Status:         domain.TransactionStatusPosted,
		SourceService:  sourceService,
		CreatedAt:      time.Now(),
		Legs:           legs,
	}

	if err := txn.Validate(); err != nil {
		if errors.Is(err, domain.ErrUnbalancedLegs) {
			balanceCheckFailures.Inc() // must always be zero; alerts page on any increment (NFR-DATA-3)
		}
		return nil, err
	}

	payload := transactionPostedPayload{
		TransactionID: txn.ID,
		SourceService: txn.SourceService,
		Legs:          toLegPayloads(txn.Legs),
	}
	if err := s.repo.CreateWithOutbox(ctx, txn, EventTransactionPosted, payload); err != nil {
		return nil, fmt.Errorf("posting transaction: %w", err)
	}
	transactionsPosted.Inc()

	return txn, nil
}

// ReverseTransaction implements FR-TXN-4: a new, equal-and-opposite
// transaction referencing the original — never a delete, never an in-place edit.
func (s *LedgerService) ReverseTransaction(ctx context.Context, sourceService, originalTransactionID, idempotencyKey string) (*domain.Transaction, error) {
	if existing, err := s.repo.GetByIdempotencyKey(ctx, sourceService, idempotencyKey); err == nil && existing != nil {
		return existing, nil
	} else if err != nil && err != domain.ErrTransactionNotFound {
		return nil, fmt.Errorf("checking idempotency key: %w", err)
	}

	original, err := s.repo.GetByID(ctx, originalTransactionID)
	if err != nil {
		return nil, err
	}

	alreadyReversed, err := s.repo.IsReversed(ctx, originalTransactionID)
	if err != nil {
		return nil, fmt.Errorf("checking reversal status: %w", err)
	}
	if alreadyReversed {
		return nil, domain.ErrAlreadyReversed
	}

	reversalID := uuid.NewString()
	reversalLegs := make([]domain.Leg, len(original.Legs))
	for i, l := range original.Legs {
		inverse := domain.DirectionCredit
		if l.Direction == domain.DirectionCredit {
			inverse = domain.DirectionDebit
		}
		reversalLegs[i] = domain.Leg{
			ID:              uuid.NewString(),
			TransactionID:   reversalID,
			ParticipantID:   l.ParticipantID,
			ParticipantType: l.ParticipantType,
			AmountMinor:     l.AmountMinor,
			Direction:       inverse,
			Currency:        l.Currency,
		}
	}

	reversal := &domain.Transaction{
		ID:                    reversalID,
		IdempotencyKey:        idempotencyKey,
		Status:                domain.TransactionStatusPosted,
		ReversesTransactionID: &originalTransactionID,
		SourceService:         sourceService,
		CreatedAt:             time.Now(),
		Legs:                  reversalLegs,
	}
	if err := reversal.Validate(); err != nil {
		return nil, fmt.Errorf("reversal legs failed validation (this should be impossible if the original was valid): %w", err)
	}

	payload := transactionReversedPayload{
		TransactionID:         reversal.ID,
		ReversesTransactionID: originalTransactionID,
		Legs:                  toLegPayloads(reversal.Legs),
	}
	if err := s.repo.CreateWithOutbox(ctx, reversal, EventTransactionReversed, payload); err != nil {
		return nil, fmt.Errorf("posting reversal: %w", err)
	}

	if err := s.repo.MarkReversed(ctx, originalTransactionID); err != nil {
		return nil, fmt.Errorf("marking original transaction reversed: %w", err)
	}

	return reversal, nil
}

// GetAccountHistory implements FR-TXN-6.
func (s *LedgerService) GetAccountHistory(ctx context.Context, participantID string, from, to *time.Time, limit int, cursor string) ([]domain.Leg, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.GetHistory(ctx, participantID, from, to, limit, cursor)
}
