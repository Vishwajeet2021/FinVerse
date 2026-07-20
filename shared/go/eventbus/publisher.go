package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

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
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling event data: %w", err)
	}

	env := Envelope{
		EventID:       uuid.NewString(),
		EventType:     eventType,
		Version:       1,
		OccurredAt:    time.Now(),
		SourceService: sourceService,
		TraceID:       traceID,
		Data:          payload,
	}

	envBytes, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshaling envelope: %w", err)
	}

	err = p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: eventType,
		Values: map[string]any{"envelope": envBytes},
	}).Err()
	if err != nil {
		return fmt.Errorf("publishing %s to stream: %w", eventType, err)
	}
	return nil
}
