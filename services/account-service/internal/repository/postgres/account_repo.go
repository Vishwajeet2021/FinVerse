package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/account-service/internal/domain"
	"github.com/finverse/account-service/internal/service"
	"github.com/finverse/shared/telemetry"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AccountRepository struct {
	db *gorm.DB
}

func NewAccountRepository(db *gorm.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

// OpenAccountWithOutbox implements the atomic account-open pattern: account
// row, OWNER holder row, zero-balance projection row, and the AccountOpened
// outbox event all commit together or not at all
// (docs/05-system-architecture.md §7).
func (r *AccountRepository) OpenAccountWithOutbox(ctx context.Context, account *domain.Account, holder *domain.AccountHolder, eventType string, eventPayload any) error {
	payloadJSON, err := json.Marshal(eventPayload)
	if err != nil {
		return fmt.Errorf("marshaling outbox payload: %w", err)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(toAccountModel(account)).Error; err != nil {
			if mapped := mapUniqueViolation(err); mapped != nil {
				return mapped
			}
			return fmt.Errorf("inserting account: %w", err)
		}
		if err := tx.Create(toHolderModel(holder)).Error; err != nil {
			return fmt.Errorf("inserting account holder: %w", err)
		}
		if err := tx.Create(&balanceProjectionModel{AccountID: account.ID, UpdatedAt: time.Now()}).Error; err != nil {
			return fmt.Errorf("inserting balance projection: %w", err)
		}
		outbox := &outboxModel{ID: uuid.NewString(), EventType: eventType, Payload: payloadJSON, CreatedAt: time.Now(), TraceParent: telemetry.TraceParent(ctx)}
		if err := tx.Create(outbox).Error; err != nil {
			return fmt.Errorf("inserting outbox event: %w", err)
		}
		return nil
	})
}

func (r *AccountRepository) GetByID(ctx context.Context, accountID string) (*domain.Account, error) {
	var m accountModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAccountNotFound
		}
		return nil, fmt.Errorf("querying account: %w", err)
	}
	return fromAccountModel(&m), nil
}

func (r *AccountRepository) GetByAccountNumber(ctx context.Context, accountNumber string) (*domain.Account, error) {
	var m accountModel
	if err := r.db.WithContext(ctx).First(&m, "account_number = ?", accountNumber).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAccountNotFound
		}
		return nil, fmt.Errorf("querying account by number: %w", err)
	}
	return fromAccountModel(&m), nil
}

func (r *AccountRepository) ListByCustomerID(ctx context.Context, customerID string) ([]domain.Account, error) {
	var models []accountModel
	if err := r.db.WithContext(ctx).Where("customer_id = ?", customerID).Order("created_at").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("listing accounts for customer: %w", err)
	}
	accounts := make([]domain.Account, len(models))
	for i := range models {
		accounts[i] = *fromAccountModel(&models[i])
	}
	return accounts, nil
}

func (r *AccountRepository) GetHolder(ctx context.Context, accountID, customerID string) (*domain.AccountHolder, error) {
	var m accountHolderModel
	err := r.db.WithContext(ctx).First(&m, "account_id = ? AND customer_id = ?", accountID, customerID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotAccountHolder
		}
		return nil, fmt.Errorf("querying account holder: %w", err)
	}
	return &domain.AccountHolder{AccountID: m.AccountID, CustomerID: m.CustomerID, Role: domain.HolderRole(m.Role)}, nil
}

func (r *AccountRepository) GetBalance(ctx context.Context, accountID string) (*domain.BalanceProjection, error) {
	var m balanceProjectionModel
	if err := r.db.WithContext(ctx).First(&m, "account_id = ?", accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAccountNotFound
		}
		return nil, fmt.Errorf("querying balance projection: %w", err)
	}
	return fromBalanceModel(&m), nil
}

func (r *AccountRepository) UpdateStatusWithOutbox(ctx context.Context, accountID string, status domain.AccountStatus, frozenBy domain.Actor, eventType string, eventPayload any) error {
	payloadJSON, err := json.Marshal(eventPayload)
	if err != nil {
		return fmt.Errorf("marshaling outbox payload: %w", err)
	}

	// frozen_by is meaningful only while FROZEN; any other transition clears it.
	var frozenByVal *string
	if status == domain.StatusFrozen && frozenBy != "" {
		v := string(frozenBy)
		frozenByVal = &v
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&accountModel{}).Where("id = ?", accountID).
			Updates(map[string]any{"status": string(status), "frozen_by": frozenByVal}).Error; err != nil {
			return fmt.Errorf("updating account status: %w", err)
		}
		outbox := &outboxModel{ID: uuid.NewString(), EventType: eventType, Payload: payloadJSON, CreatedAt: time.Now(), TraceParent: telemetry.TraceParent(ctx)}
		if err := tx.Create(outbox).Error; err != nil {
			return fmt.Errorf("inserting outbox event: %w", err)
		}
		return nil
	})
}

// ApplyTransactionPosted implements the idempotent projection update: the
// processed_events insert and every leg's balance delta happen in the same
// DB transaction, so a redelivered event (Redis Streams consumer groups are
// at-least-once) is a guaranteed no-op on replay
// (docs/05-system-architecture.md §7).
func (r *AccountRepository) ApplyTransactionPosted(ctx context.Context, eventID, transactionID string, legs []service.LegUpdate) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&processedEventModel{EventID: eventID, ProcessedAt: time.Now()})
		if result.Error != nil {
			return fmt.Errorf("recording processed event: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return nil // already processed — every leg update below is skipped
		}

		for _, leg := range legs {
			err := tx.Model(&balanceProjectionModel{}).
				Where("account_id = ?", leg.AccountID).
				Updates(map[string]any{
					"ledger_balance_minor":    gorm.Expr("ledger_balance_minor + ?", leg.DeltaMinor),
					"available_balance_minor": gorm.Expr("available_balance_minor + ?", leg.DeltaMinor),
					"last_transaction_id":     transactionID,
					"updated_at":              time.Now(),
				}).Error
			if err != nil {
				return fmt.Errorf("applying balance delta for account %s: %w", leg.AccountID, err)
			}
		}
		return nil
	})
}

func (r *AccountRepository) AccountNumberExists(ctx context.Context, accountNumber string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&accountModel{}).Where("account_number = ?", accountNumber).Count(&count).Error; err != nil {
		return false, fmt.Errorf("checking account number existence: %w", err)
	}
	return count > 0, nil
}
