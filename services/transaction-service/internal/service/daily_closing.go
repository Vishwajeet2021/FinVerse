package service

import (
	"context"
	"fmt"
	"time"
)

type dailyClosingPayload struct {
	AsOf             string `json:"asOf"`
	ParticipantCount int    `json:"participantCount"`
}

// RunDailyClosing implements FR-TXN-5: snapshot every ledger participant's
// balance as of now. Deliberately callable on demand (via an internal HTTP
// endpoint, see internal/transport/http) rather than self-scheduled inside
// the service — a real cron/EventBridge Scheduler trigger belongs to
// docs/14-aws-infrastructure.md §7 (Phase 4); Phase 2's job is to get the
// closing logic itself correct and testable first.
func (s *LedgerService) RunDailyClosing(ctx context.Context) error {
	asOf := time.Now()

	participants, err := s.repo.DistinctParticipants(ctx)
	if err != nil {
		return fmt.Errorf("listing participants: %w", err)
	}

	for _, p := range participants {
		balance, err := s.repo.BalanceAsOf(ctx, p.ParticipantID, p.Currency, asOf)
		if err != nil {
			return fmt.Errorf("computing balance for participant %s: %w", p.ParticipantID, err)
		}
		if err := s.repo.InsertClosingSnapshot(ctx, p.ParticipantID, balance, p.Currency, asOf); err != nil {
			return fmt.Errorf("inserting snapshot for participant %s: %w", p.ParticipantID, err)
		}
	}

	if err := s.repo.InsertOutboxEvent(ctx, EventDailyClosingDone, dailyClosingPayload{
		AsOf:             asOf.Format(time.RFC3339),
		ParticipantCount: len(participants),
	}); err != nil {
		return fmt.Errorf("queuing DailyClosingCompleted event: %w", err)
	}

	return nil
}
