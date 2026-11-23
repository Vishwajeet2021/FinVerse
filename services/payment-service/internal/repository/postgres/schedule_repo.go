package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/payment-service/internal/domain"
	"gorm.io/gorm"
)

type scheduledModel struct {
	ID            string     `gorm:"column:id;primaryKey"`
	CreatedBy     string     `gorm:"column:created_by"`
	FromAccountID string     `gorm:"column:from_account_id"`
	ToReference   string     `gorm:"column:to_reference"`
	Rail          string     `gorm:"column:rail"`
	AmountMinor   int64      `gorm:"column:amount_minor"`
	RunAt         time.Time  `gorm:"column:run_at"`
	Status        string     `gorm:"column:status"`
	PaymentID     *string    `gorm:"column:payment_id"`
	ClaimedUntil  *time.Time `gorm:"column:claimed_until"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
}

func (scheduledModel) TableName() string { return "scheduled_payments" }

type standingModel struct {
	ID            string     `gorm:"column:id;primaryKey"`
	CreatedBy     string     `gorm:"column:created_by"`
	FromAccountID string     `gorm:"column:from_account_id"`
	ToReference   string     `gorm:"column:to_reference"`
	Rail          string     `gorm:"column:rail"`
	AmountMinor   int64      `gorm:"column:amount_minor"`
	Frequency     string     `gorm:"column:frequency"`
	NextRunAt     time.Time  `gorm:"column:next_run_at"`
	AnchorDay     int        `gorm:"column:anchor_day"`
	Status        string     `gorm:"column:status"`
	LastPaymentID *string    `gorm:"column:last_payment_id"`
	ClaimedUntil  *time.Time `gorm:"column:claimed_until"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
}

func (standingModel) TableName() string { return "standing_instructions" }

func (m scheduledModel) toDomain() domain.ScheduledPayment {
	return domain.ScheduledPayment{ID: m.ID, CreatedBy: m.CreatedBy, FromAccountID: m.FromAccountID, ToReference: m.ToReference,
		Rail: domain.Rail(m.Rail), AmountMinor: m.AmountMinor, RunAt: m.RunAt, Status: m.Status, PaymentID: m.PaymentID, CreatedAt: m.CreatedAt}
}

func (m standingModel) toDomain() domain.StandingInstruction {
	return domain.StandingInstruction{ID: m.ID, CreatedBy: m.CreatedBy, FromAccountID: m.FromAccountID, ToReference: m.ToReference,
		Rail: domain.Rail(m.Rail), AmountMinor: m.AmountMinor, Frequency: domain.Frequency(m.Frequency), NextRunAt: m.NextRunAt,
		AnchorDay: m.AnchorDay, Status: m.Status, LastPaymentID: m.LastPaymentID, CreatedAt: m.CreatedAt}
}

type ScheduleRepository struct{ db *gorm.DB }

func NewScheduleRepository(db *gorm.DB) *ScheduleRepository { return &ScheduleRepository{db: db} }

// --- scheduled (one-time) payments ---------------------------------------

func (r *ScheduleRepository) CreateScheduled(ctx context.Context, sp *domain.ScheduledPayment) error {
	m := scheduledModel{ID: sp.ID, CreatedBy: sp.CreatedBy, FromAccountID: sp.FromAccountID, ToReference: sp.ToReference,
		Rail: string(sp.Rail), AmountMinor: sp.AmountMinor, RunAt: sp.RunAt, Status: "PENDING", CreatedAt: sp.CreatedAt}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *ScheduleRepository) ListScheduled(ctx context.Context, createdBy string, limit int) ([]domain.ScheduledPayment, error) {
	var ms []scheduledModel
	if err := r.db.WithContext(ctx).Where("created_by = ?", createdBy).Order("created_at DESC").Limit(limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ScheduledPayment, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.toDomain())
	}
	return out, nil
}

func (r *ScheduleRepository) CancelScheduled(ctx context.Context, createdBy, id string) error {
	var m scheduledModel
	if err := r.db.WithContext(ctx).Where("id = ? AND created_by = ?", id, createdBy).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrScheduleNotFound
		}
		return err
	}
	// The status guard is in the UPDATE itself so a cancel racing the executor
	// resolves in the database: exactly one of them wins.
	res := r.db.WithContext(ctx).Model(&scheduledModel{}).Where("id = ? AND status = 'PENDING'", id).Update("status", "CANCELLED")
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrScheduleNotPending
	}
	return nil
}

func (r *ScheduleRepository) ClaimDueScheduled(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]domain.ScheduledPayment, error) {
	var ms []scheduledModel
	err := r.db.WithContext(ctx).Raw(`
		UPDATE scheduled_payments SET claimed_until = ?
		 WHERE id IN (
		       SELECT id FROM scheduled_payments
		        WHERE status = 'PENDING' AND run_at <= ?
		          AND (claimed_until IS NULL OR claimed_until <= ?)
		        ORDER BY run_at FOR UPDATE SKIP LOCKED LIMIT ?)
		RETURNING *`, now.Add(lease), now, now, limit).Scan(&ms).Error
	if err != nil {
		return nil, fmt.Errorf("claiming due scheduled payments: %w", err)
	}
	out := make([]domain.ScheduledPayment, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.toDomain())
	}
	return out, nil
}

func (r *ScheduleRepository) CompleteScheduled(ctx context.Context, id, paymentID string) error {
	return r.db.WithContext(ctx).Model(&scheduledModel{}).Where("id = ? AND status = 'PENDING'", id).
		Updates(map[string]any{"status": "EXECUTED", "payment_id": paymentID, "claimed_until": nil}).Error
}

func (r *ScheduleRepository) ReleaseScheduled(ctx context.Context, id string, retryAt time.Time) error {
	return r.db.WithContext(ctx).Model(&scheduledModel{}).Where("id = ?", id).Update("claimed_until", retryAt).Error
}

// --- standing instructions -------------------------------------------------

func (r *ScheduleRepository) CreateStanding(ctx context.Context, si *domain.StandingInstruction) error {
	m := standingModel{ID: si.ID, CreatedBy: si.CreatedBy, FromAccountID: si.FromAccountID, ToReference: si.ToReference,
		Rail: string(si.Rail), AmountMinor: si.AmountMinor, Frequency: string(si.Frequency), NextRunAt: si.NextRunAt,
		AnchorDay: si.AnchorDay, Status: "ACTIVE", CreatedAt: si.CreatedAt}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *ScheduleRepository) ListStanding(ctx context.Context, createdBy string, limit int) ([]domain.StandingInstruction, error) {
	var ms []standingModel
	if err := r.db.WithContext(ctx).Where("created_by = ?", createdBy).Order("created_at DESC").Limit(limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.StandingInstruction, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.toDomain())
	}
	return out, nil
}

func (r *ScheduleRepository) GetStanding(ctx context.Context, createdBy, id string) (*domain.StandingInstruction, error) {
	var m standingModel
	if err := r.db.WithContext(ctx).Where("id = ? AND created_by = ?", id, createdBy).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrScheduleNotFound
		}
		return nil, err
	}
	d := m.toDomain()
	return &d, nil
}

func (r *ScheduleRepository) SetStandingStatus(ctx context.Context, createdBy, id, status string, nextRunAt *time.Time) error {
	updates := map[string]any{"status": status, "claimed_until": nil}
	if nextRunAt != nil {
		updates["next_run_at"] = *nextRunAt
	}
	// CANCELLED is terminal: the guard lives in the UPDATE so it cannot be raced.
	res := r.db.WithContext(ctx).Model(&standingModel{}).
		Where("id = ? AND created_by = ? AND status <> 'CANCELLED'", id, createdBy).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		if _, err := r.GetStanding(ctx, createdBy, id); err != nil {
			return err
		}
		return domain.ErrScheduleNotPending
	}
	return nil
}

func (r *ScheduleRepository) ClaimDueStanding(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]domain.StandingInstruction, error) {
	var ms []standingModel
	err := r.db.WithContext(ctx).Raw(`
		UPDATE standing_instructions SET claimed_until = ?
		 WHERE id IN (
		       SELECT id FROM standing_instructions
		        WHERE status = 'ACTIVE' AND next_run_at <= ?
		          AND (claimed_until IS NULL OR claimed_until <= ?)
		        ORDER BY next_run_at FOR UPDATE SKIP LOCKED LIMIT ?)
		RETURNING *`, now.Add(lease), now, now, limit).Scan(&ms).Error
	if err != nil {
		return nil, fmt.Errorf("claiming due standing instructions: %w", err)
	}
	out := make([]domain.StandingInstruction, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.toDomain())
	}
	return out, nil
}

func (r *ScheduleRepository) AdvanceStanding(ctx context.Context, id string, nextRunAt time.Time, lastPaymentID string) error {
	return r.db.WithContext(ctx).Model(&standingModel{}).Where("id = ?", id).
		Updates(map[string]any{"next_run_at": nextRunAt, "last_payment_id": lastPaymentID, "claimed_until": nil}).Error
}

func (r *ScheduleRepository) ReleaseStanding(ctx context.Context, id string, retryAt time.Time) error {
	return r.db.WithContext(ctx).Model(&standingModel{}).Where("id = ?", id).Update("claimed_until", retryAt).Error
}
