package postgres

import (
	"context"
	"time"

	"github.com/finverse/audit-service/internal/service"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type recordModel struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement"`
	EventID       string    `gorm:"column:event_id"`
	EventType     string    `gorm:"column:event_type"`
	SourceService string    `gorm:"column:source_service"`
	OccurredAt    time.Time `gorm:"column:occurred_at"`
	TraceID       *string   `gorm:"column:trace_id"`
	Payload       []byte    `gorm:"column:payload;type:jsonb"`
	RecordedAt    time.Time `gorm:"column:recorded_at;->"` // DB default; never written by the app
}

func (recordModel) TableName() string { return "audit_records" }

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Append(ctx context.Context, rec service.Record) error {
	m := recordModel{EventID: rec.EventID, EventType: rec.EventType, SourceService: rec.SourceService,
		OccurredAt: rec.OccurredAt, Payload: rec.Payload}
	if rec.TraceID != "" {
		m.TraceID = &rec.TraceID
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, DoNothing: true}).Create(&m).Error
}

func (r *Repository) List(ctx context.Context, q service.Query) ([]service.Record, error) {
	tx := r.db.WithContext(ctx).Order("id DESC").Limit(q.Limit)
	if q.EventType != "" {
		tx = tx.Where("event_type = ?", q.EventType)
	}
	if q.BeforeID > 0 {
		tx = tx.Where("id < ?", q.BeforeID)
	}
	var ms []recordModel
	if err := tx.Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]service.Record, 0, len(ms))
	for _, m := range ms {
		rec := service.Record{ID: m.ID, EventID: m.EventID, EventType: m.EventType, SourceService: m.SourceService,
			OccurredAt: m.OccurredAt, Payload: m.Payload, RecordedAt: m.RecordedAt}
		if m.TraceID != nil {
			rec.TraceID = *m.TraceID
		}
		out = append(out, rec)
	}
	return out, nil
}
