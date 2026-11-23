package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Handler processes one event. Returning an error leaves the message
// unacknowledged so it's retried with exponential backoff (per-service
// handlers must be idempotent — dedupe by Envelope.EventID — per
// docs/09-api-standards.md §4.1, since a retry can and will redeliver).
// Wrap an error with Permanent to skip retries and dead-letter immediately.
type Handler func(ctx context.Context, env Envelope) error

// Stats are cumulative per-Consumer counters, exposed so a service can
// publish them on its metrics/admin endpoints (NFR-OBS-2).
type Stats struct {
	Processed     int64 `json:"processed"`     // handler succeeded and the message was acked
	Retried       int64 `json:"retried"`       // handler failed and the message was left for redelivery
	DeadLettered  int64 `json:"deadLettered"`  // moved to the DLQ
	SkippedTarget int64 `json:"skippedTarget"` // replay addressed to another group
}

// Consumer implements the SQS-like semantics of a per-service queue
// subscribed to an EventBridge rule (docs/05-system-architecture.md §7),
// backed by one Redis Stream consumer group per (stream, service):
//   - Backoff ≈ SQS visibility timeout, but growing: the Nth redelivery waits
//     base·2^(N-1) (capped at max), so a struggling downstream (an email
//     provider having an outage) isn't hammered at a fixed rate.
//   - MaxDeliveries ≈ SQS maxReceiveCount — beyond this, the message moves to
//     the per-group DLQ (DLQName) instead of being retried again, carrying its
//     full envelope so an operator can inspect and Replay it.
type Consumer struct {
	client        *redis.Client
	stream        string
	group         string
	consumerName  string
	baseBackoff   time.Duration
	maxBackoff    time.Duration
	maxDeliveries int64
	logger        *zap.Logger

	processed, retried, deadLettered, skipped atomic.Int64
}

type ConsumerOption func(*Consumer)

// WithMinIdleTime sets the base redelivery delay (kept for compatibility
// with earlier callers; equivalent to WithBackoff(d, current max)).
func WithMinIdleTime(d time.Duration) ConsumerOption { return func(c *Consumer) { c.baseBackoff = d } }
func WithMaxDeliveries(n int64) ConsumerOption       { return func(c *Consumer) { c.maxDeliveries = n } }

// WithBackoff sets the redelivery delay schedule: base for the first retry,
// doubling each time up to max.
func WithBackoff(base, max time.Duration) ConsumerOption {
	return func(c *Consumer) { c.baseBackoff, c.maxBackoff = base, max }
}

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
		baseBackoff:   5 * time.Second,
		maxBackoff:    5 * time.Minute,
		maxDeliveries: 5,
		logger:        logger,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Stats returns a snapshot of this consumer's counters.
func (c *Consumer) Stats() Stats {
	return Stats{
		Processed:     c.processed.Load(),
		Retried:       c.retried.Load(),
		DeadLettered:  c.deadLettered.Load(),
		SkippedTarget: c.skipped.Load(),
	}
}

// DLQ returns the operator handle for this consumer's dead-letter stream.
func (c *Consumer) DLQ() *DLQ { return NewDLQ(c.client, c.stream, c.group) }

// backoffFor is the minimum idle time before the message's (attempt+1)th
// delivery, where attempt is the number of deliveries so far (>= 1).
func (c *Consumer) backoffFor(attempt int64) time.Duration {
	d := c.baseBackoff
	for i := int64(1); i < attempt && d < c.maxBackoff; i++ {
		d *= 2
	}
	if d > c.maxBackoff {
		d = c.maxBackoff
	}
	return d
}

func (c *Consumer) ensureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, c.stream, c.group, "0").Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		// BUSYGROUP means the group already exists — not an error, every
		// consumer instance hits this on startup.
		if strings.HasPrefix(err.Error(), "BUSYGROUP") {
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
	go c.reportDLQDepth(ctx)

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

// reclaimStalePending implements the redelivery decision for messages that
// were delivered but never acked: dead-letter what's exhausted its attempts,
// retry what has waited out its backoff, leave the rest alone.
func (c *Consumer) reclaimStalePending(ctx context.Context, handler Handler) error {
	pending, err := c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: c.stream,
		Group:  c.group,
		Idle:   c.baseBackoff,
		Start:  "-",
		End:    "+",
		Count:  50,
	}).Result()
	if err != nil {
		return fmt.Errorf("querying pending messages: %w", err)
	}

	for _, p := range pending {
		if p.RetryCount >= c.maxDeliveries {
			if err := c.deadLetter(ctx, p.ID, fmt.Sprintf("exceeded %d delivery attempts", c.maxDeliveries), p.RetryCount); err != nil {
				c.logger.Error("dead-lettering message", zap.String("id", p.ID), zap.Error(err))
			}
			continue
		}
		if p.Idle < c.backoffFor(p.RetryCount) {
			continue // still inside its backoff window
		}

		claimed, err := c.client.XClaim(ctx, &redis.XClaimArgs{
			Stream:   c.stream,
			Group:    c.group,
			Consumer: c.consumerName,
			MinIdle:  c.backoffFor(p.RetryCount),
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
		if tg, ok := m.Values["target_group"].(string); ok && tg != "" && tg != c.group {
			// A DLQ replay addressed to a different service — not ours.
			c.skipped.Add(1)
			c.count("skipped")
			_ = c.client.XAck(ctx, c.stream, c.group, m.ID).Err()
			continue
		}

		raw, ok := m.Values["envelope"].(string)
		if !ok {
			c.logger.Error("message missing envelope field, dead-lettering", zap.String("id", m.ID))
			_ = c.deadLetter(ctx, m.ID, "missing envelope field", 1)
			continue
		}

		var env Envelope
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			c.logger.Error("unmarshaling envelope, dead-lettering", zap.String("id", m.ID), zap.Error(err))
			_ = c.deadLetter(ctx, m.ID, "unmarshal failure: "+err.Error(), 1)
			continue
		}

		hctx, span := c.startSpan(ctx, env)
		err := handler(hctx, env)
		if err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.Bool("messaging.failed", true))
		}
		span.End()

		if err != nil {
			if IsPermanent(err) {
				c.logger.Error("handler failed permanently, dead-lettering",
					zap.String("event_id", env.EventID), zap.String("event_type", env.EventType), zap.Error(err))
				_ = c.deadLetter(ctx, m.ID, "permanent failure: "+err.Error(), 1)
				continue
			}
			c.retried.Add(1)
			c.count("retried")
			c.logger.Warn("handler failed, leaving unacked for retry",
				zap.String("event_id", env.EventID), zap.String("event_type", env.EventType), zap.Error(err))
			continue // left un-acked; reclaimStalePending retries after backoff or dead-letters it
		}

		if err := c.client.XAck(ctx, c.stream, c.group, m.ID).Err(); err != nil {
			c.logger.Error("acking message", zap.String("id", m.ID), zap.Error(err))
			continue
		}
		c.processed.Add(1)
		c.count("processed")
	}
}

// startSpan continues the publisher's trace: the consumer span is a child of
// the span recorded in the envelope, so Jaeger shows request → ledger post →
// event → every consumer as one trace.
func (c *Consumer) startSpan(ctx context.Context, env Envelope) (context.Context, trace.Span) {
	if env.TraceParent != "" {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": env.TraceParent})
	}
	return otel.Tracer(tracerName).Start(ctx, env.EventType+" process",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "redis-streams"),
			attribute.String("messaging.destination.name", c.stream),
			attribute.String("messaging.consumer.group.name", c.group),
			attribute.String("messaging.operation.type", "process"),
			attribute.String("event.id", env.EventID)))
}

// deadLetter moves a message to this group's DLQ, copying its original
// envelope (when still readable) so it can be inspected and replayed, then
// acks it out of the pending list.
func (c *Consumer) deadLetter(ctx context.Context, id, reason string, attempts int64) error {
	values := map[string]any{
		"original_id":      id,
		"reason":           reason,
		"attempts":         attempts,
		"dead_lettered_at": time.Now().UTC().Format(time.RFC3339),
	}
	if msgs, err := c.client.XRangeN(ctx, c.stream, id, id, 1).Result(); err == nil && len(msgs) == 1 {
		if raw, ok := msgs[0].Values["envelope"].(string); ok {
			values["envelope"] = raw
		}
	}
	if err := c.client.XAdd(ctx, &redis.XAddArgs{Stream: DLQName(c.stream, c.group), Values: values}).Err(); err != nil {
		return fmt.Errorf("adding to DLQ: %w", err)
	}
	c.deadLettered.Add(1)
	c.count("dead_lettered")
	return c.client.XAck(ctx, c.stream, c.group, id).Err()
}
