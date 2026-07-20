package postgres

import "github.com/finverse/account-service/internal/domain"

func toAccountModel(a *domain.Account) *accountModel {
	return &accountModel{
		ID:                  a.ID,
		CustomerID:          a.CustomerID,
		AccountNumber:       a.AccountNumber,
		ProductType:         string(a.ProductType),
		Status:              string(a.Status),
		OverdraftLimitMinor: a.OverdraftLimitMinor,
		CreatedAt:           a.CreatedAt,
	}
}

func fromAccountModel(m *accountModel) *domain.Account {
	return &domain.Account{
		ID:                  m.ID,
		CustomerID:          m.CustomerID,
		AccountNumber:       m.AccountNumber,
		ProductType:         domain.ProductType(m.ProductType),
		Status:              domain.AccountStatus(m.Status),
		OverdraftLimitMinor: m.OverdraftLimitMinor,
		CreatedAt:           m.CreatedAt,
	}
}

func toHolderModel(h *domain.AccountHolder) *accountHolderModel {
	return &accountHolderModel{AccountID: h.AccountID, CustomerID: h.CustomerID, Role: string(h.Role)}
}

func fromBalanceModel(m *balanceProjectionModel) *domain.BalanceProjection {
	return &domain.BalanceProjection{
		AccountID:             m.AccountID,
		LedgerBalanceMinor:    m.LedgerBalanceMinor,
		AvailableBalanceMinor: m.AvailableBalanceMinor,
		LastTransactionID:     m.LastTransactionID,
		UpdatedAt:             m.UpdatedAt,
	}
}
