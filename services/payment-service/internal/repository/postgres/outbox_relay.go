package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/finverse/shared/eventbus"
	"github.com/finverse/shared/telemetry"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// OutboxRelay mirrors Transaction Service's
// internal/repository/postgres/outbox_relay.go exactly.
type OutboxRelay struct {
	db        *gorm.DB
	publisher *eventbus.Publisher
	logger    *zap.Logger
	interval  time.Duration
}

func NewOutboxRelay(db *gorm.DB, publisher *eventbus.Publisher, logger *zap.Logger) *OutboxRelay {
	return &OutboxRelay{db: db, publisher: publisher, logger: logger, interval: 500 * time.Millisecond}
}

func (r *OutboxRelay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.relayOnce(ctx); err != nil {
				r.logger.Error("outbox relay pass failed", zap.Error(err))
			}
		}
	}
}

func (r *OutboxRelay) relayOnce(ctx context.Context) error {
	var rows []outboxModel
	if err := r.db.WithContext(ctx).Where("published_at IS NULL").Order("created_at").Limit(50).Find(&rows).Error; err != nil {
		return err
	}

	for _, row := range rows {
		var data any
		if err := json.Unmarshal(row.Payload, &data); err != nil {
			r.logger.Error("outbox row has unparseable payload, skipping", zap.String("id", row.ID), zap.Error(err))
			continue
		}

		if err := r.publisher.PublishWithTrace(ctx, "payment-service", row.EventType, "", telemetry.Str(row.TraceParent), data); err != nil {
			r.logger.Error("publishing outbox event", zap.String("id", row.ID), zap.String("event_type", row.EventType), zap.Error(err))
			continue
		}

		now := time.Now()
		if err := r.db.WithContext(ctx).Model(&outboxModel{}).Where("id = ?", row.ID).Update("published_at", now).Error; err != nil {
			r.logger.Error("marking outbox row published", zap.String("id", row.ID), zap.Error(err))
		}
	}
	return nil
}
