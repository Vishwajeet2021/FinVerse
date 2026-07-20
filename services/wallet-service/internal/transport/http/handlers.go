// Package http implements Wallet Service's REST contract per
// docs/09-api-standards.md and docs/07-microservice-specifications.md §5.
package http

import (
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/jwtauth"
	"github.com/finverse/wallet-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	wallets *service.WalletService
}

func NewHandler(wallets *service.WalletService) *Handler {
	return &Handler{wallets: wallets}
}

// customerIDFromClaims treats the JWT subject as the customer identity
// directly — the same documented Phase 2 simplification Account Service
// uses (see services/account-service/internal/transport/http/handlers.go).
func customerIDFromClaims(c *gin.Context) string {
	return jwtauth.ClaimsFromContext(c).Subject
}

// idempotencyKey reads the Idempotency-Key header (docs/09-api-standards.md §2.6);
// if the caller didn't send one, a server-generated one is used so the
// request still succeeds, but retries from that same caller without the
// header won't be deduplicated — clients are expected to send their own.
func idempotencyKey(c *gin.Context) string {
	if k := c.GetHeader("Idempotency-Key"); k != "" {
		return k
	}
	return uuid.NewString()
}

func (h *Handler) ProvisionWallet(c *gin.Context) {
	var req provisionWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	wallet, err := h.wallets.ProvisionWallet(c.Request.Context(), customerIDFromClaims(c), req.FundingAccountID)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.Created(c, toWalletResponse(wallet))
}

func (h *Handler) GetMyWallet(c *gin.Context) {
	wallet, err := h.wallets.GetByCustomerID(c.Request.Context(), customerIDFromClaims(c))
	if err != nil {
		writeDomainError(c, err)
		return
	}

	balance, err := h.wallets.GetBalance(c.Request.Context(), wallet.ID)
	if err != nil {
		writeDomainError(c, err)
		return
	}

	apiresponse.OK(c, gin.H{
		"wallet":  toWalletResponse(wallet),
		"balance": balanceResponse{WalletID: balance.WalletID, BalanceMinor: balance.BalanceMinor},
	})
}

func (h *Handler) TopUp(c *gin.Context) {
	wallet, err := h.wallets.GetByCustomerID(c.Request.Context(), customerIDFromClaims(c))
	if err != nil {
		writeDomainError(c, err)
		return
	}

	var req topUpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	if err := h.wallets.TopUp(c.Request.Context(), wallet.ID, req.AmountMinor, idempotencyKey(c)); err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"status": "completed"})
}

func (h *Handler) Transfer(c *gin.Context) {
	wallet, err := h.wallets.GetByCustomerID(c.Request.Context(), customerIDFromClaims(c))
	if err != nil {
		writeDomainError(c, err)
		return
	}

	var req transferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	if err := h.wallets.Transfer(c.Request.Context(), wallet.ID, req.ToWalletID, req.AmountMinor, idempotencyKey(c)); err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"status": "completed"})
}
