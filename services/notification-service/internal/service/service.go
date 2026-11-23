// Package service is Notification Service's use-case layer: turn one bus
// event into one delivered notification, exactly once in effect.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/finverse/notification-service/internal/render"
	"github.com/finverse/notification-service/internal/sender"
	"github.com/finverse/shared/eventbus"
)

type Notification struct {
	ID           string     `json:"id"`
	EventID      string     `json:"eventId"`
	EventType    string     `json:"eventType"`
	RecipientRef string     `json:"recipientRef"`
	Channel      string     `json:"channel"`
	Subject      string     `json:"subject"`
	Body         string     `json:"body"`
	Status       string     `json:"status"`
	Attempts     int        `json:"attempts"`
	LastError    *string    `json:"lastError,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	SentAt       *time.Time `json:"sentAt,omitempty"`
}

// Repository persists notifications. Create is idempotent on EventID.
type Repository interface {
	// Create inserts a PENDING notification for the event, or returns the
	// existing one for that event ID (so a redelivered event reuses its row).
	Create(ctx context.Context, n Notification, traceID string) (Notification, error)
	RecordAttempt(ctx context.Context, id string, sendErr error) error
	MarkSent(ctx context.Context, id string) error
	ListByRecipient(ctx context.Context, recipientRef string, limit int) ([]Notification, error)
	ListAll(ctx context.Context, limit int) ([]Notification, error)
}

type Service struct {
	repo   Repository
	sender sender.Sender
}

func New(repo Repository, s sender.Sender) *Service { return &Service{repo: repo, sender: s} }

// Handle is the eventbus.Handler. Contract:
//   - unrenderable event → Permanent error → straight to the DLQ;
//   - already SENT (a redelivery after a lost ack) → nil, no second email;
//   - provider failure → plain error → the bus retries with backoff, and
//     dead-letters after MaxDeliveries.
func (s *Service) Handle(ctx context.Context, env eventbus.Envelope) error {
	msg, err := render.Render(env)
	if err != nil {
		return err
	}

	n, err := s.repo.Create(ctx, Notification{
		EventID: env.EventID, EventType: env.EventType, RecipientRef: msg.RecipientRef,
		Channel: "EMAIL", Subject: msg.Subject, Body: msg.Body,
	}, env.TraceID)
	if err != nil {
		return fmt.Errorf("persisting notification: %w", err)
	}
	if n.Status == "SENT" {
		return nil
	}

	sendErr := s.sender.Send(ctx, sender.Message{To: msg.RecipientRef, Subject: msg.Subject, Body: msg.Body})
	if recErr := s.repo.RecordAttempt(ctx, n.ID, sendErr); recErr != nil && sendErr == nil {
		return fmt.Errorf("recording attempt: %w", recErr)
	}
	if sendErr != nil {
		return fmt.Errorf("delivering notification %s: %w", n.ID, sendErr)
	}
	return s.repo.MarkSent(ctx, n.ID)
}

func (s *Service) ListForRecipient(ctx context.Context, recipientRef string, limit int) ([]Notification, error) {
	return s.repo.ListByRecipient(ctx, recipientRef, limit)
}

func (s *Service) ListAll(ctx context.Context, limit int) ([]Notification, error) {
	return s.repo.ListAll(ctx, limit)
}
