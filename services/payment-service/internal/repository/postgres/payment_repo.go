package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/payment-service/internal/domain"
	"github.com/finverse/shared/telemetry"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PaymentRepository struct {
	db *gorm.DB
}

func NewPaymentRepository(db *gorm.DB) *PaymentRepository {
	return &PaymentRepository{db: db}
}

func (r *PaymentRepository) GetByIdempotencyKey(ctx context.Context, fromAccountID, idempotencyKey string) (*domain.Payment, error) {
	var m paymentModel
	err := r.db.WithContext(ctx).First(&m, "from_account_id = ? AND idempotency_key = ?", fromAccountID, idempotencyKey).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrPaymentNotFound
		}
		return nil, fmt.Errorf("querying payment by idempotency key: %w", err)
	}
	return fromPaymentModel(&m), nil
}

func (r *PaymentRepository) GetByID(ctx context.Context, paymentID string) (*domain.Payment, error) {
	var m paymentModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", paymentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrPaymentNotFound
		}
		return nil, fmt.Errorf("querying payment: %w", err)
	}
	return fromPaymentModel(&m), nil
}

func (r *PaymentRepository) Create(ctx context.Context, payment *domain.Payment) error {
	if err := r.db.WithContext(ctx).Create(toPaymentModel(payment)).Error; err != nil {
		if mapped := mapUniqueViolation(err); mapped != nil {
			return mapped
		}
		return fmt.Errorf("inserting payment: %w", err)
	}
	paymentsTotal.WithLabelValues("INITIATED").Inc()
	return nil
}

func (r *PaymentRepository) InsertOutboxEvent(ctx context.Context, eventType string, payload any) error {
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

func (r *PaymentRepository) MarkCompletedWithOutbox(ctx context.Context, paymentID, transactionID, eventType string, eventPayload any) error {
	payloadJSON, err := json.Marshal(eventPayload)
	if err != nil {
		return fmt.Errorf("marshaling outbox payload: %w", err)
	}

	return countOnSuccess("COMPLETED", r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&paymentModel{}).Where("id = ?", paymentID).Updates(map[string]any{
			"status":         string(domain.StatusCompleted),
			"transaction_id": transactionID,
		}).Error
		if err != nil {
			return fmt.Errorf("marking payment completed: %w", err)
		}
		outbox := &outboxModel{ID: uuid.NewString(), EventType: eventType, Payload: payloadJSON, CreatedAt: time.Now(), TraceParent: telemetry.TraceParent(ctx)}
		if err := tx.Create(outbox).Error; err != nil {
			return fmt.Errorf("inserting outbox event: %w", err)
		}
		return nil
	}))
}

func (r *PaymentRepository) MarkFailedWithOutbox(ctx context.Context, paymentID, reason, eventType string, eventPayload any) error {
	payloadJSON, err := json.Marshal(eventPayload)
	if err != nil {
		return fmt.Errorf("marshaling outbox payload: %w", err)
	}

	return countOnSuccess("FAILED", r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&paymentModel{}).Where("id = ?", paymentID).Updates(map[string]any{
			"status":         string(domain.StatusFailed),
			"failure_reason": reason,
		}).Error
		if err != nil {
			return fmt.Errorf("marking payment failed: %w", err)
		}
		outbox := &outboxModel{ID: uuid.NewString(), EventType: eventType, Payload: payloadJSON, CreatedAt: time.Now(), TraceParent: telemetry.TraceParent(ctx)}
		if err := tx.Create(outbox).Error; err != nil {
			return fmt.Errorf("inserting outbox event: %w", err)
		}
		return nil
	}))
}
