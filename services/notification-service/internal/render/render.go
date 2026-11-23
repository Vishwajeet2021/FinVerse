// Package render turns a domain event into the message a customer sees.
// It owns the notification wording and nothing else: the Notification
// Service "has no opinion on why something happened"
// (docs/07-microservice-specifications.md §11).
package render

import (
	"encoding/json"
	"fmt"

	"github.com/finverse/shared/eventbus"
)

// Streams are the event types this service consumes (one Redis Stream each).
var Streams = []string{"PaymentCompleted", "PaymentFailed", "AccountOpened", "AccountFrozen"}

type Message struct {
	RecipientRef string
	Subject      string
	Body         string
}

// Render returns a Permanent error for anything retrying can't fix — an
// unknown event type or an unparseable/incomplete payload — so poison
// messages reach the DLQ on the first attempt rather than after N retries.
func Render(env eventbus.Envelope) (Message, error) {
	switch env.EventType {
	case "PaymentCompleted":
		var p struct {
			PaymentID     string `json:"paymentId"`
			TransactionID string `json:"transactionId"`
		}
		if err := json.Unmarshal(env.Data, &p); err != nil || p.PaymentID == "" {
			return Message{}, bad(env, err, "paymentId")
		}
		return Message{RecipientRef: "payment:" + p.PaymentID, Subject: "Your payment was completed",
			Body: fmt.Sprintf("Payment %s has been completed (transaction %s).", p.PaymentID, p.TransactionID)}, nil
	case "PaymentFailed":
		var p struct {
			PaymentID string `json:"paymentId"`
			Reason    string `json:"reason"`
		}
		if err := json.Unmarshal(env.Data, &p); err != nil || p.PaymentID == "" {
			return Message{}, bad(env, err, "paymentId")
		}
		return Message{RecipientRef: "payment:" + p.PaymentID, Subject: "Your payment could not be completed",
			Body: fmt.Sprintf("Payment %s failed: %s.", p.PaymentID, p.Reason)}, nil
	case "AccountOpened":
		var p struct {
			AccountID     string `json:"accountId"`
			CustomerID    string `json:"customerId"`
			AccountNumber string `json:"accountNumber"`
		}
		if err := json.Unmarshal(env.Data, &p); err != nil || p.CustomerID == "" {
			return Message{}, bad(env, err, "customerId")
		}
		return Message{RecipientRef: "customer:" + p.CustomerID, Subject: "Welcome — your account is open",
			Body: fmt.Sprintf("Your new account %s is ready to use.", p.AccountNumber)}, nil
	case "AccountFrozen":
		var p struct {
			AccountID string `json:"accountId"`
			Reason    string `json:"reason"`
		}
		if err := json.Unmarshal(env.Data, &p); err != nil || p.AccountID == "" {
			return Message{}, bad(env, err, "accountId")
		}
		return Message{RecipientRef: "account:" + p.AccountID, Subject: "Your account has been frozen",
			Body: fmt.Sprintf("Account %s was frozen. Reason: %s.", p.AccountID, p.Reason)}, nil
	default:
		return Message{}, eventbus.Permanent(fmt.Errorf("no template for event type %q", env.EventType))
	}
}

func bad(env eventbus.Envelope, err error, field string) error {
	if err != nil {
		return eventbus.Permanent(fmt.Errorf("%s payload unparseable: %w", env.EventType, err))
	}
	return eventbus.Permanent(fmt.Errorf("%s payload missing %s", env.EventType, field))
}
