package postgres

import "github.com/finverse/payment-service/internal/domain"

func toPaymentModel(p *domain.Payment) *paymentModel {
	return &paymentModel{
		ID:             p.ID,
		IdempotencyKey: p.IdempotencyKey,
		FromAccountID:  p.FromAccountID,
		ToReference:    p.ToReference,
		Rail:           string(p.Rail),
		AmountMinor:    p.AmountMinor,
		Status:         string(p.Status),
		TransactionID:  p.TransactionID,
		FailureReason:  p.FailureReason,
		CreatedAt:      p.CreatedAt,
	}
}

func fromPaymentModel(m *paymentModel) *domain.Payment {
	return &domain.Payment{
		ID:             m.ID,
		IdempotencyKey: m.IdempotencyKey,
		FromAccountID:  m.FromAccountID,
		ToReference:    m.ToReference,
		Rail:           domain.Rail(m.Rail),
		AmountMinor:    m.AmountMinor,
		Status:         domain.Status(m.Status),
		TransactionID:  m.TransactionID,
		FailureReason:  m.FailureReason,
		CreatedAt:      m.CreatedAt,
	}
}
