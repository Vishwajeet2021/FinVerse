package postgres

import (
	"context"
	"time"

	"github.com/finverse/notification-service/internal/service"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type notificationModel struct {
	ID           string     `gorm:"column:id;primaryKey;default:gen_random_uuid()"`
	EventID      string     `gorm:"column:event_id"`
	EventType    string     `gorm:"column:event_type"`
	RecipientRef string     `gorm:"column:recipient_ref"`
	Channel      string     `gorm:"column:channel"`
	Subject      string     `gorm:"column:subject"`
	Body         string     `gorm:"column:body"`
	Status       string     `gorm:"column:status"`
	Attempts     int        `gorm:"column:attempts"`
	LastError    *string    `gorm:"column:last_error"`
	TraceID      *string    `gorm:"column:trace_id"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	SentAt       *time.Time `gorm:"column:sent_at"`
}

func (notificationModel) TableName() string { return "notifications" }

func (m notificationModel) toDomain() service.Notification {
	return service.Notification{
		ID: m.ID, EventID: m.EventID, EventType: m.EventType, RecipientRef: m.RecipientRef,
		Channel: m.Channel, Subject: m.Subject, Body: m.Body, Status: m.Status,
		Attempts: m.Attempts, LastError: m.LastError, CreatedAt: m.CreatedAt, SentAt: m.SentAt,
	}
}

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, n service.Notification, traceID string) (service.Notification, error) {
	m := notificationModel{
		EventID: n.EventID, EventType: n.EventType, RecipientRef: n.RecipientRef, Channel: n.Channel,
		Subject: n.Subject, Body: n.Body, Status: "PENDING",
	}
	if traceID != "" {
		m.TraceID = &traceID
	}
	// ON CONFLICT DO NOTHING + re-read keeps this idempotent under concurrent
	// redelivery of the same event without a read-then-write race.
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, DoNothing: true}).Create(&m).Error; err != nil {
		return service.Notification{}, err
	}
	var existing notificationModel
	if err := r.db.WithContext(ctx).Where("event_id = ?", n.EventID).First(&existing).Error; err != nil {
		return service.Notification{}, err
	}
	return existing.toDomain(), nil
}

func (r *Repository) RecordAttempt(ctx context.Context, id string, sendErr error) error {
	updates := map[string]any{"attempts": gorm.Expr("attempts + 1")}
	if sendErr != nil {
		updates["last_error"] = sendErr.Error()
	}
	return r.db.WithContext(ctx).Model(&notificationModel{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repository) MarkSent(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&notificationModel{}).Where("id = ?", id).
		Updates(map[string]any{"status": "SENT", "sent_at": time.Now(), "last_error": nil}).Error
}

func (r *Repository) ListByRecipient(ctx context.Context, recipientRef string, limit int) ([]service.Notification, error) {
	var ms []notificationModel
	if err := r.db.WithContext(ctx).Where("recipient_ref = ?", recipientRef).Order("created_at DESC").Limit(limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	return toDomainList(ms), nil
}

func (r *Repository) ListAll(ctx context.Context, limit int) ([]service.Notification, error) {
	var ms []notificationModel
	if err := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	return toDomainList(ms), nil
}

func toDomainList(ms []notificationModel) []service.Notification {
	out := make([]service.Notification, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.toDomain())
	}
	return out
}
