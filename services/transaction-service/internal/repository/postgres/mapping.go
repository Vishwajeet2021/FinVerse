package postgres

import "github.com/finverse/transaction-service/internal/domain"

func toTransactionModel(t *domain.Transaction) *transactionModel {
	return &transactionModel{
		ID:                    t.ID,
		IdempotencyKey:        t.IdempotencyKey,
		Status:                string(t.Status),
		ReversesTransactionID: t.ReversesTransactionID,
		SourceService:         t.SourceService,
		CreatedAt:             t.CreatedAt,
	}
}

func toLegModels(legs []domain.Leg) []legModel {
	out := make([]legModel, len(legs))
	for i, l := range legs {
		out[i] = legModel{
			ID:              l.ID,
			TransactionID:   l.TransactionID,
			ParticipantID:   l.ParticipantID,
			ParticipantType: string(l.ParticipantType),
			AmountMinor:     l.AmountMinor,
			Direction:       string(l.Direction),
			Currency:        l.Currency,
		}
	}
	return out
}

func fromLegModel(m *legModel) domain.Leg {
	return domain.Leg{
		ID:              m.ID,
		TransactionID:   m.TransactionID,
		ParticipantID:   m.ParticipantID,
		ParticipantType: domain.ParticipantType(m.ParticipantType),
		AmountMinor:     m.AmountMinor,
		Direction:       domain.Direction(m.Direction),
		Currency:        m.Currency,
	}
}

func fromTransactionModel(m *transactionModel, legs []legModel) *domain.Transaction {
	domainLegs := make([]domain.Leg, len(legs))
	for i := range legs {
		domainLegs[i] = fromLegModel(&legs[i])
	}
	return &domain.Transaction{
		ID:                    m.ID,
		IdempotencyKey:        m.IdempotencyKey,
		Status:                domain.TransactionStatus(m.Status),
		ReversesTransactionID: m.ReversesTransactionID,
		SourceService:         m.SourceService,
		CreatedAt:             m.CreatedAt,
		Legs:                  domainLegs,
	}
}
