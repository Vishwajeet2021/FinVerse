package service

// Event type names this service publishes -- the authoritative catalog
// entry is docs/06-domain-driven-design.md §5.
const (
	EventPaymentInitiated = "PaymentInitiated"
	EventPaymentCompleted = "PaymentCompleted"
	EventPaymentFailed    = "PaymentFailed"
)

type paymentInitiatedPayload struct {
	PaymentID     string `json:"paymentId"`
	FromAccountID string `json:"fromAccountId"`
	ToReference   string `json:"toReference"`
	AmountMinor   int64  `json:"amountMinor"`
	Rail          string `json:"rail"`
}

type paymentCompletedPayload struct {
	PaymentID     string `json:"paymentId"`
	TransactionID string `json:"transactionId"`
}

type paymentFailedPayload struct {
	PaymentID string `json:"paymentId"`
	Reason    string `json:"reason"`
}
