// Package service is Audit Service's use-case layer: record every event from
// the catalog exactly once, immutably, and serve admin/compliance queries.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/shared/eventbus"
	"github.com/google/uuid"
)

// Streams is every event type this service records. Audit consumes the whole
// catalog (docs/07-microservice-specifications.md section 12); a new event type
// is one more entry here.
var Streams = []string{
	"PaymentInitiated", "PaymentCompleted", "PaymentFailed",
	"TransactionPosted",
	"AccountOpened", "AccountFrozen", "AccountUnfrozen", "AccountClosed",
	"WalletProvisioned", "WalletToppedUp", "WalletTransferred",
	"FraudCaseOpened", "FraudCaseResolved",
	"AdminActionRequested", "AdminActionResolved",
}

type Record struct {
	ID            int64           `json:"id"`
	EventID       string          `json:"eventId"`
	EventType     string          `json:"eventType"`
	SourceService string          `json:"sourceService"`
	OccurredAt    time.Time       `json:"occurredAt"`
	TraceID       string          `json:"traceId,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	RecordedAt    time.Time       `json:"recordedAt"`
}

type Query struct {
	EventType string
	BeforeID  int64 // keyset pagination: records with id < BeforeID
	Limit     int
}

type Repository interface {
	// Append inserts the record; a record for the same EventID already
	// existing is not an error (idempotent redelivery).
	Append(ctx context.Context, r Record) error
	List(ctx context.Context, q Query) ([]Record, error)
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

// Handle is the eventbus.Handler. An envelope that can never be recorded
// (no valid event ID) is Permanent so it reaches the DLQ instead of retrying;
// a database failure is retryable.
func (s *Service) Handle(ctx context.Context, env eventbus.Envelope) error {
	if _, err := uuid.Parse(env.EventID); err != nil {
		return eventbus.Permanent(fmt.Errorf("envelope has no valid eventId: %q", env.EventID))
	}
	if !json.Valid(env.Data) {
		return eventbus.Permanent(errors.New("envelope data is not valid JSON"))
	}
	occurred := env.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	return s.repo.Append(ctx, Record{
		EventID: env.EventID, EventType: env.EventType, SourceService: env.SourceService,
		OccurredAt: occurred, TraceID: env.TraceID, Payload: env.Data,
	})
}

func (s *Service) List(ctx context.Context, q Query) ([]Record, error) {
	if q.Limit < 1 || q.Limit > 200 {
		q.Limit = 50
	}
	return s.repo.List(ctx, q)
}
