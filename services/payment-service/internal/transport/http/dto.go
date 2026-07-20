package http

import "github.com/finverse/payment-service/internal/domain"

type initiatePaymentRequest struct {
	FromAccountID string `json:"fromAccountId" binding:"required"`
	ToAccountID   string `json:"toAccountId" binding:"required"`
	AmountMinor   int64  `json:"amountMinor" binding:"required"`
}

type paymentResponse struct {
	ID            string  `json:"id"`
	FromAccountID string  `json:"fromAccountId"`
	ToReference   string  `json:"toReference"`
	Rail          string  `json:"rail"`
	AmountMinor   int64   `json:"amountMinor"`
	Status        string  `json:"status"`
	TransactionID *string `json:"transactionId,omitempty"`
	FailureReason *string `json:"failureReason,omitempty"`
}

func toPaymentResponse(p *domain.Payment) paymentResponse {
	return paymentResponse{
		ID: p.ID, FromAccountID: p.FromAccountID, ToReference: p.ToReference,
		Rail: string(p.Rail), AmountMinor: p.AmountMinor, Status: string(p.Status),
		TransactionID: p.TransactionID, FailureReason: p.FailureReason,
	}
}
