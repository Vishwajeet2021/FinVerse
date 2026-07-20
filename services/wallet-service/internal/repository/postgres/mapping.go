package postgres

import "github.com/finverse/wallet-service/internal/domain"

func toWalletModel(w *domain.Wallet) *walletModel {
	return &walletModel{
		ID:               w.ID,
		CustomerID:       w.CustomerID,
		FundingAccountID: w.FundingAccountID,
		Status:           string(w.Status),
		CreatedAt:        w.CreatedAt,
	}
}

func fromWalletModel(m *walletModel) *domain.Wallet {
	return &domain.Wallet{
		ID:               m.ID,
		CustomerID:       m.CustomerID,
		FundingAccountID: m.FundingAccountID,
		Status:           domain.WalletStatus(m.Status),
		CreatedAt:        m.CreatedAt,
	}
}

func fromBalanceModel(m *balanceProjectionModel) *domain.BalanceProjection {
	return &domain.BalanceProjection{
		WalletID:          m.WalletID,
		BalanceMinor:      m.BalanceMinor,
		LastTransactionID: m.LastTransactionID,
		UpdatedAt:         m.UpdatedAt,
	}
}
