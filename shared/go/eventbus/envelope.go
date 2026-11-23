// Package eventbus is the local-development stand-in for EventBridge + SQS,
// per docs/18-development-workflow.md §7's Phase 2 decision: Redis Streams,
// behind this package's interface, so swapping in real EventBridge/SQS for
// deployed environments (docs/14-aws-infrastructure.md) only means a new
// implementation of Publisher/Consumer, never a change to a service's
// business logic.
package eventbus

import (
	"encoding/json"
	"time"
)

// Envelope is the domain event shape from docs/09-api-standards.md §4.1 —
// every publisher and consumer in every service uses exactly this shape, so
// the 15 services' events feel like one system.
type Envelope struct {
	EventID       string    `json:"eventId"`
	EventType     string    `json:"eventType"`
	Version       int       `json:"version"`
	OccurredAt    time.Time `json:"occurredAt"`
	SourceService string    `json:"sourceService"`
	TraceID       string    `json:"traceId,omitempty"`
	// TraceParent is the W3C traceparent of the publishing span, so a
	// consumer's span is a child of the request that caused the event
	// (NFR-OBS-1, trace propagation across the event fan-out).
	TraceParent string          `json:"traceParent,omitempty"`
	Data        json.RawMessage `json:"data"`
}
