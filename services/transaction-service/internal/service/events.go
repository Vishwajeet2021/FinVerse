package service

import "github.com/finverse/transaction-service/internal/domain"

// Event type names — the authoritative catalog entry is
// docs/06-domain-driven-design.md §5; these constants are what
// shared/go/eventbus.Publisher.Publish uses as the Redis Stream name today
// and the EventBridge rule name once Phase 4 lands.
const (
	EventTransactionPosted   = "TransactionPosted"
	EventTransactionReversed = "TransactionReversed"
	EventDailyClosingDone    = "DailyClosingCompleted"
)

type legPayload struct {
	ParticipantID   string `json:"participantId"`
	ParticipantType string `json:"participantType"`
	AmountMinor     int64  `json:"amountMinor"`
	Direction       string `json:"direction"`
	Currency        string `json:"currency"`
}

// transactionPostedPayload is TransactionPosted's Envelope.Data shape,
// consumed by Account/Wallet Service to update their balance projections
// (docs/06-domain-driven-design.md §5) and by Notification/Audit/Analytics.
type transactionPostedPayload struct {
	TransactionID string       `json:"transactionId"`
	SourceService string       `json:"sourceService"`
	Legs          []legPayload `json:"legs"`
}

type transactionReversedPayload struct {
	TransactionID         string       `json:"transactionId"`
	ReversesTransactionID string       `json:"reversesTransactionId"`
	Legs                  []legPayload `json:"legs"`
}

func toLegPayloads(legs []domain.Leg) []legPayload {
	out := make([]legPayload, len(legs))
	for i, l := range legs {
		out[i] = legPayload{
			ParticipantID:   l.ParticipantID,
			ParticipantType: string(l.ParticipantType),
			AmountMinor:     l.AmountMinor,
			Direction:       string(l.Direction),
			Currency:        l.Currency,
		}
	}
	return out
}
