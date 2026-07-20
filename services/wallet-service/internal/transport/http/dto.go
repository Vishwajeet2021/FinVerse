package http

import "github.com/finverse/wallet-service/internal/domain"

type provisionWalletRequest struct {
	FundingAccountID string `json:"fundingAccountId" binding:"required"`
}

type walletResponse struct {
	ID               string `json:"id"`
	FundingAccountID string `json:"fundingAccountId"`
	Status           string `json:"status"`
}

func toWalletResponse(w *domain.Wallet) walletResponse {
	return walletResponse{ID: w.ID, FundingAccountID: w.FundingAccountID, Status: string(w.Status)}
}

type balanceResponse struct {
	WalletID     string `json:"walletId"`
	BalanceMinor int64  `json:"balanceMinor"`
}

type topUpRequest struct {
	AmountMinor int64 `json:"amountMinor" binding:"required"`
}

type transferRequest struct {
	ToWalletID  string `json:"toWalletId" binding:"required"`
	AmountMinor int64  `json:"amountMinor" binding:"required"`
}
