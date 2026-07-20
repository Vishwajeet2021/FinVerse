package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Handler processes one event. Returning an error leaves the message
// unacknowledged so it's retried (per-service handlers must be idempotent —
// dedupe by Envelope.EventID — per docs/09-api-standards.md §4.1, since a
// retry can and will redeliver).
type Handler func(ctx context.Context, env Envelope) error

// Consumer implements the SQS-like semantics of a per-service queue
// subscribed to an EventBridge rule (docs/05-system-architecture.md §7),
// backed by one Redis Stream consumer group per (stream, service):
//   - MinIdleTime ≈ SQS visibility timeout — a message claimed by a dead or
//     stuck consumer becomes eligible for another consumer to retry.
//   - MaxDeliveries ≈ SQS maxReceiveCount — beyond this, the message moves
//     to "<stream>:dlq" instead of being retried again.
type Consumer struct {
	client        *redis.Client
	stream        string
	group         string
	consumerName  string
	minIdleTime   time.Duration
	maxDeliveries int64
	logger        *zap.Logger
}

type ConsumerOption func(*Consumer)

func WithMinIdleTime(d time.Duration) ConsumerOption { return func(c *Consumer) { c.minIdleTime = d } }
func WithMaxDeliveries(n int64) ConsumerOption       { return func(c *Consumer) { c.maxDeliveries = n } }

// NewConsumer builds a Consumer. stream is the event type (matching what
// Publisher.Publish used as eventType); group should be unique per
// consuming service (e.g. "account-service") so each service gets its own
// delivery cursor and its own DLQ, per docs/05-system-architecture.md §7's
// bulkhead pattern — one slow/broken consumer never backs up another.
func NewConsumer(client *redis.Client, stream, group, consumerName string, logger *zap.Logger, opts ...ConsumerOption) *Consumer {
	c := &Consumer{
		client:        client,
		stream:        stream,
		group:         group,
		consumerName:  consumerName,
		minIdleTime:   30 * time.Second,
		maxDeliveries: 5,
		logger:        logger,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Consumer) dlqStream() string { return c.stream + ":dlq" }

func (c *Consumer) ensureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, c.stream, c.group, "0").Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		// BUSYGROUP means the group already exists — not an error, every
		// consumer instance hits this on startup.
		if e := err.Error(); len(e) >= 9 && e[:9] == "BUSYGROUP" {
			return nil
		}
		return fmt.Errorf("creating consumer group %s on %s: %w", c.group, c.stream, err)
	}
	return nil
}

// Run processes messages until ctx is cancelled. It is meant to run for the
// lifetime of the service in its own goroutine.
func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	if err := c.ensureGroup(ctx); err != nil {
		return err
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := c.reclaimStalePending(ctx, handler); err != nil {
			c.logger.Error("reclaiming stale pending messages", zap.String("stream", c.stream), zap.Error(err))
		}

		if err := c.readNew(ctx, handler); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			c.logger.Error("reading new messages", zap.String("stream", c.stream), zap.Error(err))
			time.Sleep(time.Second) // avoid a hot loop against a transiently unreachable Redis
		}
	}
}

// reclaimStalePending implements the visibility-timeout-expired /
// exceeded-redelivery-count decision: dead-letter what's exhausted its
// retries, reclaim-and-retry everything else.
func (c *Consumer) reclaimStalePending(ctx context.Context, handler Handler) error {
	pending, err := c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: c.stream,
		Group:  c.group,
		Idle:   c.minIdleTime,
		Start:  "-",
		End:    "+",
		Count:  50,
	}).Result()
	if err != nil {
		return fmt.Errorf("querying pending messages: %w", err)
	}

	for _, p := range pending {
		if p.RetryCount > c.maxDeliveries {
			if err := c.deadLetter(ctx, p.ID, fmt.Sprintf("exceeded %d delivery attempts", c.maxDeliveries)); err != nil {
				c.logger.Error("dead-lettering message", zap.String("id", p.ID), zap.Error(err))
			}
			continue
		}

		claimed, err := c.client.XClaim(ctx, &redis.XClaimArgs{
			Stream:   c.stream,
			Group:    c.group,
			Consumer: c.consumerName,
			MinIdle:  c.minIdleTime,
			Messages: []string{p.ID},
		}).Result()
		if err != nil {
			c.logger.Error("claiming message", zap.String("id", p.ID), zap.Error(err))
			continue
		}
		c.process(ctx, claimed, handler)
	}
	return nil
}

func (c *Consumer) readNew(ctx context.Context, handler Handler) error {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.group,
		Consumer: c.consumerName,
		Streams:  []string{c.stream, ">"},
		Count:    10,
		Block:    2 * time.Second,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) || err.Error() == "redis: nil" {
			return nil // block timeout, no new messages — not an error
		}
		return err
	}
	for _, s := range streams {
		c.process(ctx, s.Messages, handler)
	}
	return nil
}

func (c *Consumer) process(ctx context.Context, messages []redis.XMessage, handler Handler) {
	for _, m := range messages {
		raw, ok := m.Values["envelope"].(string)
		if !ok {
			c.logger.Error("message missing envelope field, dead-lettering", zap.String("id", m.ID))
			_ = c.deadLetter(ctx, m.ID, "missing envelope field")
			continue
		}

		var env Envelope
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			c.logger.Error("unmarshaling envelope, dead-lettering", zap.String("id", m.ID), zap.Error(err))
			_ = c.deadLetter(ctx, m.ID, "unmarshal failure: "+err.Error())
			continue
		}

		if err := handler(ctx, env); err != nil {
			c.logger.Warn("handler failed, leaving unacked for retry",
				zap.String("event_id", env.EventID), zap.String("event_type", env.EventType), zap.Error(err))
			continue // left un-acked; reclaimStalePending will retry or dead-letter it later
		}

		if err := c.client.XAck(ctx, c.stream, c.group, m.ID).Err(); err != nil {
			c.logger.Error("acking message", zap.String("id", m.ID), zap.Error(err))
		}
	}
}

func (c *Consumer) deadLetter(ctx context.Context, id, reason string) error {
	if err := c.client.XAdd(ctx, &redis.XAddArgs{
		Stream: c.dlqStream(),
		Values: map[string]any{"original_id": id, "reason": reason, "dead_lettered_at": time.Now().Format(time.RFC3339)},
	}).Err(); err != nil {
		return fmt.Errorf("adding to DLQ: %w", err)
	}
	return c.client.XAck(ctx, c.stream, c.group, id).Err()
}
