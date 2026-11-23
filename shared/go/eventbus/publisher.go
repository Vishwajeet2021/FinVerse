package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/finverse/shared/eventbus"

// Publisher publishes domain events to a Redis Stream — the local analogue
// of publishing to an EventBridge bus. One stream per event type, matching
// EventBridge's per-eventType rule model (docs/09-api-standards.md §4.3);
// callers pass the eventType as the stream name via Publish.
type Publisher struct {
	client *redis.Client
}

func NewPublisher(client *redis.Client) *Publisher {
	return &Publisher{client: client}
}

// Publish wraps data in the standard Envelope and appends it to the stream
// named after eventType. This is called from a service's outbox relay
// (docs/05-system-architecture.md §7), never directly from a request
// handler — the relay is what makes publish-after-commit atomic with the
// triggering database write.
func (p *Publisher) Publish(ctx context.Context, sourceService, eventType, traceID string, data any) error {
	return p.PublishWithTrace(ctx, sourceService, eventType, traceID, "", data)
}

// PublishWithTrace is Publish plus an explicit upstream W3C traceparent
// (typically persisted in the outbox row when the originating request ran),
// used when ctx itself carries no span because the relay runs on a timer.
func (p *Publisher) PublishWithTrace(ctx context.Context, sourceService, eventType, traceID, traceParent string, data any) error {
	if traceParent != "" {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": traceParent})
	}
	ctx, span := otel.Tracer(tracerName).Start(ctx, eventType+" publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "redis-streams"),
			attribute.String("messaging.destination.name", eventType),
			attribute.String("messaging.operation.type", "publish")))
	defer span.End()

	payload, err := json.Marshal(data)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("marshaling event data: %w", err)
	}

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if traceID == "" && span.SpanContext().HasTraceID() {
		traceID = span.SpanContext().TraceID().String()
	}

	env := Envelope{
		EventID:       uuid.NewString(),
		EventType:     eventType,
		Version:       1,
		OccurredAt:    time.Now(),
		SourceService: sourceService,
		TraceID:       traceID,
		TraceParent:   carrier["traceparent"],
		Data:          payload,
	}
	span.SetAttributes(attribute.String("event.id", env.EventID))

	envBytes, err := json.Marshal(env)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("marshaling envelope: %w", err)
	}

	err = p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: eventType,
		Values: map[string]any{"envelope": envBytes},
	}).Err()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("publishing %s to stream: %w", eventType, err)
	}
	return nil
}
