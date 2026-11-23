package http

import (
	"time"

	"github.com/finverse/payment-service/internal/domain"
)

type initiatePaymentRequest struct {
	FromAccountID   string `json:"fromAccountId" binding:"required"`
	ToAccountNumber string `json:"toAccountNumber" binding:"required"`
	AmountMinor     int64  `json:"amountMinor" binding:"required"`
	Rail            string `json:"rail"` // optional; defaults to INTERNAL
}

type paymentResponse struct {
	ID            string     `json:"id"`
	FromAccountID string     `json:"fromAccountId"`
	ToReference   string     `json:"toReference"`
	Rail          string     `json:"rail"`
	AmountMinor   int64      `json:"amountMinor"`
	Status        string     `json:"status"`
	TransactionID *string    `json:"transactionId,omitempty"`
	FailureReason *string    `json:"failureReason,omitempty"`
	SettleAt      *time.Time `json:"settleAt,omitempty"` // when an async rail will clear the payment
}

func toPaymentResponse(p *domain.Payment) paymentResponse {
	return paymentResponse{
		ID: p.ID, FromAccountID: p.FromAccountID, ToReference: p.ToReference,
		Rail: string(p.Rail), AmountMinor: p.AmountMinor, Status: string(p.Status),
		TransactionID: p.TransactionID, FailureReason: p.FailureReason, SettleAt: p.SettleAt,
	}
}

type scheduleRequest struct {
	FromAccountID   string    `json:"fromAccountId" binding:"required"`
	ToAccountNumber string    `json:"toAccountNumber" binding:"required"`
	AmountMinor     int64     `json:"amountMinor" binding:"required"`
	Rail            string    `json:"rail"`
	RunAt           time.Time `json:"runAt"` // scheduled payments
	// Standing instructions:
	Frequency  string    `json:"frequency"`
	FirstRunAt time.Time `json:"firstRunAt"`
}

type scheduledResponse struct {
	ID            string    `json:"id"`
	FromAccountID string    `json:"fromAccountId"`
	ToReference   string    `json:"toReference"`
	Rail          string    `json:"rail"`
	AmountMinor   int64     `json:"amountMinor"`
	RunAt         time.Time `json:"runAt"`
	Status        string    `json:"status"`
	PaymentID     *string   `json:"paymentId,omitempty"`
}

func toScheduledResponse(s domain.ScheduledPayment) scheduledResponse {
	return scheduledResponse{ID: s.ID, FromAccountID: s.FromAccountID, ToReference: s.ToReference, Rail: string(s.Rail),
		AmountMinor: s.AmountMinor, RunAt: s.RunAt, Status: s.Status, PaymentID: s.PaymentID}
}

type standingResponse struct {
	ID            string    `json:"id"`
	FromAccountID string    `json:"fromAccountId"`
	ToReference   string    `json:"toReference"`
	Rail          string    `json:"rail"`
	AmountMinor   int64     `json:"amountMinor"`
	Frequency     string    `json:"frequency"`
	NextRunAt     time.Time `json:"nextRunAt"`
	Status        string    `json:"status"`
	LastPaymentID *string   `json:"lastPaymentId,omitempty"`
}

func toStandingResponse(s domain.StandingInstruction) standingResponse {
	return standingResponse{ID: s.ID, FromAccountID: s.FromAccountID, ToReference: s.ToReference, Rail: string(s.Rail),
		AmountMinor: s.AmountMinor, Frequency: string(s.Frequency), NextRunAt: s.NextRunAt, Status: s.Status, LastPaymentID: s.LastPaymentID}
}
