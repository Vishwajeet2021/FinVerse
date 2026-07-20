package service

// Event type names this service publishes -- the authoritative catalog
// entry is docs/06-domain-driven-design.md §5 (FR-WAL-5 for the required
// two; WalletProvisioned is an extra this service also emits, see
// ProvisionWallet's comment). These are what
// shared/go/eventbus.Publisher.Publish uses as the Redis Stream name today
// and the EventBridge rule name once Phase 4 lands.
const (
	EventWalletProvisioned = "WalletProvisioned"
	EventWalletToppedUp    = "WalletToppedUp"
	EventWalletTransferred = "WalletTransferred"
)

// LegPayload and TransactionPostedPayload mirror Transaction Service's
// internal/service/events.go wire shape exactly -- this is what Wallet
// Service consumes off the TransactionPosted stream, not a type this
// service owns. Only the fields this service actually needs are declared,
// per the tolerant-reader pattern in docs/09-api-standards.md §4.1: an
// additive field Transaction Service adds later is silently ignored here
// rather than failing to unmarshal.
type LegPayload struct {
	ParticipantID   string `json:"participantId"`
	ParticipantType string `json:"participantType"`
	AmountMinor     int64  `json:"amountMinor"`
	Direction       string `json:"direction"`
	Currency        string `json:"currency"`
}

type TransactionPostedPayload struct {
	TransactionID string       `json:"transactionId"`
	SourceService string       `json:"sourceService"`
	Legs          []LegPayload `json:"legs"`
}

type walletProvisionedPayload struct {
	WalletID         string `json:"walletId"`
	CustomerID       string `json:"customerId"`
	FundingAccountID string `json:"fundingAccountId"`
}

type walletToppedUpPayload struct {
	WalletID         string `json:"walletId"`
	FundingAccountID string `json:"fundingAccountId"`
	AmountMinor      int64  `json:"amountMinor"`
	Currency         string `json:"currency"`
	TransactionID    string `json:"transactionId"`
}

type walletTransferredPayload struct {
	FromWalletID  string `json:"fromWalletId"`
	ToWalletID    string `json:"toWalletId"`
	AmountMinor   int64  `json:"amountMinor"`
	Currency      string `json:"currency"`
	TransactionID string `json:"transactionId"`
}
