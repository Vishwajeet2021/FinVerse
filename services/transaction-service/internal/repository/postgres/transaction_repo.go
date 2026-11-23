package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/finverse/shared/telemetry"
	"github.com/finverse/transaction-service/internal/domain"
	"github.com/finverse/transaction-service/internal/service"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TransactionRepository struct {
	db *gorm.DB
}

func NewTransactionRepository(db *gorm.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

// CreateWithOutbox implements the atomic ledger-write + outbox-event pattern
// (docs/05-system-architecture.md §7). If the legs don't balance, the
// database's deferred constraint trigger (db/migrations/0001_init.up.sql)
// fires at COMMIT and rolls back the entire transaction — transaction row,
// legs, and outbox row all disappear together, never a partial write.
func (r *TransactionRepository) CreateWithOutbox(ctx context.Context, txn *domain.Transaction, eventType string, eventPayload any) error {
	payloadJSON, err := json.Marshal(eventPayload)
	if err != nil {
		return fmt.Errorf("marshaling outbox payload: %w", err)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(toTransactionModel(txn)).Error; err != nil {
			if mapped := mapUniqueViolation(err); mapped != nil {
				return mapped
			}
			return fmt.Errorf("inserting transaction: %w", err)
		}

		legModels := toLegModels(txn.Legs)
		if err := tx.Create(&legModels).Error; err != nil {
			return fmt.Errorf("inserting legs: %w", err) // includes the balance-invariant trigger failure
		}

		outbox := &outboxModel{ID: uuid.NewString(), EventType: eventType, Payload: payloadJSON, CreatedAt: time.Now(), TraceParent: telemetry.TraceParent(ctx)}
		if err := tx.Create(outbox).Error; err != nil {
			return fmt.Errorf("inserting outbox event: %w", err)
		}

		return nil
	})
}

func (r *TransactionRepository) InsertOutboxEvent(ctx context.Context, eventType string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling outbox payload: %w", err)
	}
	outbox := &outboxModel{ID: uuid.NewString(), EventType: eventType, Payload: payloadJSON, CreatedAt: time.Now(), TraceParent: telemetry.TraceParent(ctx)}
	if err := r.db.WithContext(ctx).Create(outbox).Error; err != nil {
		return fmt.Errorf("inserting standalone outbox event: %w", err)
	}
	return nil
}

func (r *TransactionRepository) GetByIdempotencyKey(ctx context.Context, sourceService, idempotencyKey string) (*domain.Transaction, error) {
	var m transactionModel
	err := r.db.WithContext(ctx).First(&m, "source_service = ? AND idempotency_key = ?", sourceService, idempotencyKey).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTransactionNotFound
		}
		return nil, fmt.Errorf("querying transaction by idempotency key: %w", err)
	}
	return r.loadWithLegs(ctx, &m)
}

func (r *TransactionRepository) GetByID(ctx context.Context, id string) (*domain.Transaction, error) {
	var m transactionModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTransactionNotFound
		}
		return nil, fmt.Errorf("querying transaction by id: %w", err)
	}
	return r.loadWithLegs(ctx, &m)
}

func (r *TransactionRepository) loadWithLegs(ctx context.Context, m *transactionModel) (*domain.Transaction, error) {
	var legs []legModel
	if err := r.db.WithContext(ctx).Where("transaction_id = ?", m.ID).Find(&legs).Error; err != nil {
		return nil, fmt.Errorf("loading legs: %w", err)
	}
	return fromTransactionModel(m, legs), nil
}

func (r *TransactionRepository) IsReversed(ctx context.Context, transactionID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&transactionModel{}).Where("reverses_transaction_id = ?", transactionID).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("checking reversal status: %w", err)
	}
	return count > 0, nil
}

func (r *TransactionRepository) MarkReversed(ctx context.Context, transactionID string) error {
	res := r.db.WithContext(ctx).Model(&transactionModel{}).Where("id = ?", transactionID).Update("status", string(domain.TransactionStatusReversed))
	if res.Error != nil {
		return fmt.Errorf("marking transaction reversed: %w", res.Error)
	}
	return nil
}

// historyRow is deliberately flat, not an embedded legModel — GORM's Scan
// into a struct embedding another GORM model didn't reliably bind columns
// via the embedded type's `gorm:"column:..."` tags in practice (every field
// came back zero-valued against a real query), so this spells out each
// column explicitly instead of relying on that promotion working.
type historyRow struct {
	ID              string    `gorm:"column:id"`
	TransactionID   string    `gorm:"column:transaction_id"`
	ParticipantID   string    `gorm:"column:participant_id"`
	ParticipantType string    `gorm:"column:participant_type"`
	AmountMinor     int64     `gorm:"column:amount_minor"`
	Direction       string    `gorm:"column:direction"`
	Currency        string    `gorm:"column:currency"`
	TxnCreatedAt    time.Time `gorm:"column:txn_created_at"`
}

func (h *historyRow) toLeg() domain.Leg {
	return domain.Leg{
		ID:              h.ID,
		TransactionID:   h.TransactionID,
		ParticipantID:   h.ParticipantID,
		ParticipantType: domain.ParticipantType(h.ParticipantType),
		AmountMinor:     h.AmountMinor,
		Direction:       domain.Direction(h.Direction),
		Currency:        h.Currency,
	}
}

// GetHistory implements FR-TXN-6 with keyset pagination on
// (transactions.created_at, legs.id) — cheaper and more correct under
// concurrent inserts than an OFFSET-based page, and the cursor is opaque to
// the caller per docs/09-api-standards.md §2.7.
func (r *TransactionRepository) GetHistory(ctx context.Context, participantID string, from, to *time.Time, limit int, cursor string) ([]domain.Leg, string, error) {
	q := r.db.WithContext(ctx).
		Table("legs").
		Select("legs.*, transactions.created_at as txn_created_at").
		Joins("JOIN transactions ON transactions.id = legs.transaction_id").
		Where("legs.participant_id = ?", participantID)

	if from != nil {
		q = q.Where("transactions.created_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("transactions.created_at <= ?", *to)
	}
	if cursor != "" {
		cursorTime, cursorID, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		q = q.Where("(transactions.created_at, legs.id) < (?, ?)", cursorTime, cursorID)
	}

	var rows []historyRow
	if err := q.Order("transactions.created_at DESC, legs.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, "", fmt.Errorf("querying history: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	legs := make([]domain.Leg, len(rows))
	for i := range rows {
		legs[i] = rows[i].toLeg()
	}

	nextCursor := ""
	if hasMore {
		last := rows[len(rows)-1]
		nextCursor = encodeCursor(last.TxnCreatedAt, last.ID)
	}
	return legs, nextCursor, nil
}

func encodeCursor(t time.Time, id string) string {
	raw := fmt.Sprintf("%d|%s", t.UnixNano(), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("malformed cursor")
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", err
	}
	return time.Unix(0, nanos), parts[1], nil
}

func (r *TransactionRepository) DistinctParticipants(ctx context.Context) ([]service.ParticipantRef, error) {
	var rows []struct {
		ParticipantID string
		Currency      string
	}
	if err := r.db.WithContext(ctx).Table("legs").Distinct("participant_id", "currency").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing distinct participants: %w", err)
	}
	out := make([]service.ParticipantRef, len(rows))
	for i, row := range rows {
		out[i] = service.ParticipantRef{ParticipantID: row.ParticipantID, Currency: row.Currency}
	}
	return out, nil
}

func (r *TransactionRepository) BalanceAsOf(ctx context.Context, participantID, currency string, asOf time.Time) (int64, error) {
	var result struct{ Balance int64 }
	err := r.db.WithContext(ctx).
		Table("legs").
		Select("COALESCE(SUM(CASE WHEN direction = 'D' THEN amount_minor ELSE -amount_minor END), 0) as balance").
		Joins("JOIN transactions ON transactions.id = legs.transaction_id").
		Where("legs.participant_id = ? AND legs.currency = ? AND transactions.created_at <= ?", participantID, currency, asOf).
		Scan(&result).Error
	if err != nil {
		return 0, fmt.Errorf("computing balance as of %s: %w", asOf, err)
	}
	return result.Balance, nil
}

func (r *TransactionRepository) InsertClosingSnapshot(ctx context.Context, participantID string, balanceMinor int64, currency string, asOf time.Time) error {
	snap := &closingSnapshotModel{
		ID:            uuid.NewString(),
		ParticipantID: participantID,
		BalanceMinor:  balanceMinor,
		Currency:      currency,
		AsOf:          asOf,
		CreatedAt:     time.Now(),
	}
	if err := r.db.WithContext(ctx).Create(snap).Error; err != nil {
		return fmt.Errorf("inserting closing snapshot: %w", err)
	}
	return nil
}
