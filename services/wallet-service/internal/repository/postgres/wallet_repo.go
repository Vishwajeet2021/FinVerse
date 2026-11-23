package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/shared/telemetry"
	"github.com/finverse/wallet-service/internal/domain"
	"github.com/finverse/wallet-service/internal/service"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WalletRepository struct {
	db *gorm.DB
}

func NewWalletRepository(db *gorm.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) CreateWalletWithProjection(ctx context.Context, wallet *domain.Wallet, eventType string, eventPayload any) error {
	payloadJSON, err := json.Marshal(eventPayload)
	if err != nil {
		return fmt.Errorf("marshaling outbox payload: %w", err)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(toWalletModel(wallet)).Error; err != nil {
			if mapped := mapUniqueViolation(err); mapped != nil {
				return mapped
			}
			return fmt.Errorf("inserting wallet: %w", err)
		}
		if err := tx.Create(&balanceProjectionModel{WalletID: wallet.ID, UpdatedAt: time.Now()}).Error; err != nil {
			return fmt.Errorf("inserting balance projection: %w", err)
		}
		outbox := &outboxModel{ID: uuid.NewString(), EventType: eventType, Payload: payloadJSON, CreatedAt: time.Now(), TraceParent: telemetry.TraceParent(ctx)}
		if err := tx.Create(outbox).Error; err != nil {
			return fmt.Errorf("inserting outbox event: %w", err)
		}
		return nil
	})
}

func (r *WalletRepository) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	var m walletModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("querying wallet: %w", err)
	}
	return fromWalletModel(&m), nil
}

func (r *WalletRepository) GetByCustomerID(ctx context.Context, customerID string) (*domain.Wallet, error) {
	var m walletModel
	if err := r.db.WithContext(ctx).First(&m, "customer_id = ?", customerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("querying wallet by customer: %w", err)
	}
	return fromWalletModel(&m), nil
}

func (r *WalletRepository) ExistsByID(ctx context.Context, id string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&walletModel{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, fmt.Errorf("checking wallet existence: %w", err)
	}
	return count > 0, nil
}

func (r *WalletRepository) GetBalance(ctx context.Context, walletID string) (*domain.BalanceProjection, error) {
	var m balanceProjectionModel
	if err := r.db.WithContext(ctx).First(&m, "wallet_id = ?", walletID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("querying balance projection: %w", err)
	}
	return fromBalanceModel(&m), nil
}

// ApplyTransactionPostedIdempotent: the processed_events insert and every
// delta in the same DB transaction, so a redelivered event (Redis Streams
// consumer groups are at-least-once) is a guaranteed no-op on replay
// (docs/05-system-architecture.md §7).
func (r *WalletRepository) ApplyTransactionPostedIdempotent(ctx context.Context, eventID, transactionID string, deltas []service.WalletDelta) (bool, error) {
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&processedEventModel{EventID: eventID, ProcessedAt: time.Now()})
		if result.Error != nil {
			return fmt.Errorf("recording processed event: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return nil // already processed
		}
		applied = true

		for _, d := range deltas {
			err := tx.Model(&balanceProjectionModel{}).
				Where("wallet_id = ?", d.WalletID).
				Updates(map[string]any{
					"balance_minor":       gorm.Expr("balance_minor + ?", d.DeltaMinor),
					"last_transaction_id": transactionID,
					"updated_at":          time.Now(),
				}).Error
			if err != nil {
				return fmt.Errorf("applying balance delta for wallet %s: %w", d.WalletID, err)
			}
		}
		return nil
	})
	return applied, err
}

func (r *WalletRepository) InsertOutboxEvent(ctx context.Context, eventType string, payload any) error {
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
