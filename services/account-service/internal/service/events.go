package service

// participantTypeAccount mirrors Transaction Service's
// domain.ParticipantAccount value ("ACCOUNT") — kept as a local string
// constant rather than importing Transaction Service's domain package (there
// is no such shared dependency; the two services communicate only via the
// event payload's participantType string field, per
// docs/09-api-standards.md §4.1's tolerant-reader convention).
const participantTypeAccount = "ACCOUNT"

// Event type names this service publishes — the authoritative catalog entry
// is docs/06-domain-driven-design.md §5; these constants are what
// shared/go/eventbus.Publisher.Publish uses as the Redis Stream name today.
const (
	EventAccountOpened   = "AccountOpened"
	EventAccountFrozen   = "AccountFrozen"
	EventAccountUnfrozen = "AccountUnfrozen"
)

// EventTransactionPosted is the event this service *consumes* — published by
// Transaction Service (docs/06-domain-driven-design.md §5). Account Service
// is a Conformist here (docs/06 §3): it takes this payload shape exactly as
// Transaction Service defines it, with no independent opinion of its own.
const EventTransactionPosted = "TransactionPosted"

type accountOpenedPayload struct {
	AccountID     string `json:"accountId"`
	CustomerID    string `json:"customerId"`
	AccountNumber string `json:"accountNumber"`
	ProductType   string `json:"productType"`
}

// accountFrozenPayload/accountUnfrozenPayload carry the reason/actor FR-ACC-5
// requires be recorded. They're carried on the event only, not persisted as
// a column on accounts — the given schema (docs/08-database-design.md §5)
// has no freeze-reason column, and Audit/Notification (the documented
// consumers of these events, docs/06-domain-driven-design.md §5) are exactly
// where that detail is meant to land.
type accountFrozenPayload struct {
	AccountID string `json:"accountId"`
	Reason    string `json:"reason"`
	Actor     string `json:"actor"`
}

type accountUnfrozenPayload struct {
	AccountID string `json:"accountId"`
	Actor     string `json:"actor"`
}

// legPayload mirrors Transaction Service's internal/service/events.go
// legPayload field-for-field — this is the Conformist relationship called
// out in docs/06-domain-driven-design.md §3: Account Service has no
// independent opinion about what a leg looks like.
type legPayload struct {
	ParticipantID   string `json:"participantId"`
	ParticipantType string `json:"participantType"`
	AmountMinor     int64  `json:"amountMinor"`
	Direction       string `json:"direction"`
	Currency        string `json:"currency"`
}

// TransactionPostedPayload is the Envelope.Data shape for the
// TransactionPosted event, exported so cmd/server/main.go can unmarshal
// eventbus.Envelope.Data into it before calling ConsumeTransactionPosted.
type TransactionPostedPayload struct {
	TransactionID string       `json:"transactionId"`
	SourceService string       `json:"sourceService"`
	Legs          []legPayload `json:"legs"`
}

// toLegUpdates filters the payload down to ACCOUNT-type legs and resolves
// each one's ledger direction to a signed balance delta: a credit leg adds
// money to the account (delta > 0), a debit leg removes it (delta < 0) —
// standard double-entry semantics for a liability-side participant like a
// bank account. Legs for other participant types (e.g. WALLET) are simply
// not this service's concern and are dropped here, per FR-ACC-3's scope.
func toLegUpdates(legs []legPayload) []LegUpdate {
	updates := make([]LegUpdate, 0, len(legs))
	for _, l := range legs {
		if l.ParticipantType != participantTypeAccount {
			continue
		}
		delta := l.AmountMinor
		if l.Direction == "D" {
			delta = -delta
		}
		updates = append(updates, LegUpdate{AccountID: l.ParticipantID, DeltaMinor: delta})
	}
	return updates
}
