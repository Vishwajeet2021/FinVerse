package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/admin-service/internal/service"
	"github.com/finverse/shared/eventbus"
	"github.com/finverse/shared/telemetry"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type actionModel struct {
	ID            string     `gorm:"column:id;primaryKey"`
	ActorID       string     `gorm:"column:actor_id"`
	ActionType    string     `gorm:"column:action_type"`
	TargetType    string     `gorm:"column:target_type"`
	TargetID      string     `gorm:"column:target_id"`
	Reason        string     `gorm:"column:reason"`
	Params        []byte     `gorm:"column:params;type:jsonb"`
	Status        string     `gorm:"column:status"`
	FailureReason *string    `gorm:"column:failure_reason"`
	TraceID       *string    `gorm:"column:trace_id"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	ResolvedAt    *time.Time `gorm:"column:resolved_at"`
}

func (actionModel) TableName() string { return "admin_actions" }

type outboxModel struct {
	ID          string     `gorm:"column:id;primaryKey"`
	EventType   string     `gorm:"column:event_type"`
	Payload     []byte     `gorm:"column:payload;type:jsonb"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
	TraceParent *string    `gorm:"column:traceparent"`
}

func (outboxModel) TableName() string { return "outbox" }

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func enqueue(ctx context.Context, tx *gorm.DB, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", eventType, err)
	}
	var tp *string
	if v := telemetry.TraceParent(ctx); v != nil {
		tp = v
	}
	return tx.Create(&outboxModel{ID: uuid.NewString(), EventType: eventType, Payload: raw, CreatedAt: time.Now(), TraceParent: tp}).Error
}

func (r *Repository) Begin(ctx context.Context, a service.Action) error {
	m := actionModel{
		ID: a.ID, ActorID: a.ActorID, ActionType: string(a.Type), TargetType: a.TargetType, TargetID: a.TargetID,
		Reason: a.Reason, Params: a.Params, Status: string(service.StatusRequested), CreatedAt: a.CreatedAt,
	}
	if a.TraceID != "" {
		m.TraceID = &a.TraceID
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		return enqueue(ctx, tx, service.EventRequested, map[string]any{
			"actionId": a.ID, "actorId": a.ActorID, "actionType": a.Type,
			"targetType": a.TargetType, "targetId": a.TargetID, "reason": a.Reason,
			"params": json.RawMessage(a.Params), "requestedAt": a.CreatedAt,
		})
	})
}

var errAlreadyFinal = errors.New("admin action is not in REQUESTED state")

func (r *Repository) Finish(ctx context.Context, id string, status service.Status, failureReason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		updates := map[string]any{"status": string(status), "resolved_at": now}
		if failureReason != "" {
			updates["failure_reason"] = failureReason
		}
		res := tx.Model(&actionModel{}).Where("id = ? AND status = 'REQUESTED'", id).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errAlreadyFinal
		}
		var m actionModel
		if err := tx.First(&m, "id = ?", id).Error; err != nil {
			return err
		}
		return enqueue(ctx, tx, service.EventResolved, map[string]any{
			"actionId": id, "actorId": m.ActorID, "actionType": m.ActionType,
			"targetType": m.TargetType, "targetId": m.TargetID,
			"status": string(status), "failureReason": failureReason, "resolvedAt": now,
		})
	})
}

func (r *Repository) List(ctx context.Context, limit int, before *time.Time) ([]service.Action, error) {
	q := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit)
	if before != nil {
		q = q.Where("created_at < ?", *before)
	}
	var ms []actionModel
	if err := q.Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]service.Action, 0, len(ms))
	for _, m := range ms {
		a := service.Action{
			ID: m.ID, ActorID: m.ActorID, Type: service.ActionType(m.ActionType), TargetType: m.TargetType,
			TargetID: m.TargetID, Reason: m.Reason, Params: m.Params, Status: service.Status(m.Status),
			CreatedAt: m.CreatedAt, ResolvedAt: m.ResolvedAt,
		}
		if m.FailureReason != nil {
			a.FailureReason = *m.FailureReason
		}
		if m.TraceID != nil {
			a.TraceID = *m.TraceID
		}
		out = append(out, a)
	}
	return out, nil
}

// OutboxRelay publishes committed outbox rows to the event bus.
type OutboxRelay struct {
	db        *gorm.DB
	publisher *eventbus.Publisher
	logger    *zap.Logger
}

func NewOutboxRelay(db *gorm.DB, p *eventbus.Publisher, l *zap.Logger) *OutboxRelay {
	return &OutboxRelay{db: db, publisher: p, logger: l}
}

func (o *OutboxRelay) Run(ctx context.Context) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			o.once(ctx)
		}
	}
}

func (o *OutboxRelay) once(ctx context.Context) {
	var rows []outboxModel
	if err := o.db.WithContext(ctx).Where("published_at IS NULL").Order("created_at").Limit(50).Find(&rows).Error; err != nil {
		o.logger.Error("outbox read failed", zap.Error(err))
		return
	}
	for _, row := range rows {
		var data any
		if err := json.Unmarshal(row.Payload, &data); err != nil {
			o.logger.Error("unparseable outbox payload", zap.String("id", row.ID), zap.Error(err))
			continue
		}
		if err := o.publisher.PublishWithTrace(ctx, "admin-service", row.EventType, "", telemetry.Str(row.TraceParent), data); err != nil {
			o.logger.Error("publishing outbox event", zap.String("id", row.ID), zap.Error(err))
			continue
		}
		if err := o.db.WithContext(ctx).Model(&outboxModel{}).Where("id = ?", row.ID).Update("published_at", time.Now()).Error; err != nil {
			o.logger.Error("marking outbox row published", zap.String("id", row.ID), zap.Error(err))
		}
	}
}
