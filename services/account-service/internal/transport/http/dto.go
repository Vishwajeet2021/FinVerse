package http

import "github.com/finverse/account-service/internal/service"

type openAccountRequest struct {
	ProductType string `json:"productType" binding:"required,max=16"`
}

type accountResponse struct {
	ID                  string `json:"id"`
	AccountNumber       string `json:"accountNumber"`
	ProductType         string `json:"productType"`
	Status              string `json:"status"`
	OverdraftLimitMinor int64  `json:"overdraftLimitMinor"`
}

type balanceResponse struct {
	AccountID             string `json:"accountId"`
	LedgerBalanceMinor    int64  `json:"ledgerBalanceMinor"`
	AvailableBalanceMinor int64  `json:"availableBalanceMinor"`
}

type freezeRequest struct {
	Reason string `json:"reason"`
}

type statementEntryResponse struct {
	TransactionID string `json:"transactionId"`
	LegID         string `json:"legId"`
	AmountMinor   int64  `json:"amountMinor"`
	Direction     string `json:"direction"`
	Currency      string `json:"currency"`
}

func toStatementResponse(entries []service.StatementEntry) []statementEntryResponse {
	out := make([]statementEntryResponse, len(entries))
	for i, e := range entries {
		out[i] = statementEntryResponse{
			TransactionID: e.TransactionID,
			LegID:         e.LegID,
			AmountMinor:   e.AmountMinor,
			Direction:     e.Direction,
			Currency:      e.Currency,
		}
	}
	return out
}
