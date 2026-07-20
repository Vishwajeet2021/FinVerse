// Package http implements Account Service's REST contract per
// docs/09-api-standards.md and docs/07-microservice-specifications.md §4.
package http

import (
	"context"
	"time"

	"github.com/finverse/account-service/internal/domain"
	"github.com/finverse/account-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/jwtauth"
	"github.com/gin-gonic/gin"
)

// KYCTierLookup resolves a user's live KYC tier — implemented by
// internal/client/customer against Customer Service. See that package's
// comment for why this replaced trusting the JWT's kyc_tier claim directly.
type KYCTierLookup interface {
	GetKYCTier(ctx context.Context, userID string) (int, error)
}

type Handler struct {
	accounts *service.AccountService
	kycTier  KYCTierLookup
}

func NewHandler(accounts *service.AccountService, kycTier KYCTierLookup) *Handler {
	return &Handler{accounts: accounts, kycTier: kycTier}
}

// customerIDFromClaims treats the JWT subject as the customer identity
// directly — a documented Phase 2 simplification (see
// internal/service/account_service.go's package comment and
// services/customer-service's 1:1 user_id<->customer_id link).
func customerIDFromClaims(c *gin.Context) string {
	return jwtauth.ClaimsFromContext(c).Subject
}

// OpenAccount implements FR-ACC-1, including the Tier 1+ gate (BR-03). The
// tier check calls Customer Service directly rather than trusting the JWT's
// kyc_tier claim — Auth Service always issues that claim as 0
// (services/auth-service/internal/service/login.go's documented Phase 1/2
// stand-in), so trusting it would make Tier-1+ unreachable for every user.
func (h *Handler) OpenAccount(c *gin.Context) {
	tier, err := h.kycTier.GetKYCTier(c.Request.Context(), customerIDFromClaims(c))
	if err != nil {
		apiresponse.Internal(c, "INTERNAL_ERROR", "Couldn't verify your account verification status right now. Please try again.")
		return
	}
	if tier < 1 {
		writeDomainError(c, domain.ErrInsufficientKYCTier)
		return
	}

	var req openAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	account, err := h.accounts.OpenAccount(c.Request.Context(), customerIDFromClaims(c), req.ProductType)
	if err != nil {
		writeDomainError(c, err)
		return
	}

	apiresponse.Created(c, accountResponse{
		ID: account.ID, AccountNumber: account.AccountNumber,
		ProductType: string(account.ProductType), Status: string(account.Status),
		OverdraftLimitMinor: account.OverdraftLimitMinor,
	})
}

// ListMyAccounts backs the frontend dashboard's account list — there was no
// way to discover a customer's accounts by ID alone, so this iterates
// ListMyAccounts rather than requiring a client to already know an account ID.
func (h *Handler) ListMyAccounts(c *gin.Context) {
	accounts, err := h.accounts.ListMyAccounts(c.Request.Context(), customerIDFromClaims(c))
	if err != nil {
		writeDomainError(c, err)
		return
	}
	out := make([]accountResponse, len(accounts))
	for i, a := range accounts {
		out[i] = accountResponse{
			ID: a.ID, AccountNumber: a.AccountNumber,
			ProductType: string(a.ProductType), Status: string(a.Status),
			OverdraftLimitMinor: a.OverdraftLimitMinor,
		}
	}
	apiresponse.OK(c, out)
}

// requireOwnership implements docs/10-authentication-authorization.md §3.3:
// a scope check alone is not enough — every handler for a specific resource
// must verify the caller actually owns it.
func (h *Handler) requireOwnership(c *gin.Context, accountID string) bool {
	isHolder, err := h.accounts.IsAccountHolder(c.Request.Context(), accountID, customerIDFromClaims(c))
	if err != nil {
		writeDomainError(c, err)
		return false
	}
	if !isHolder {
		writeDomainError(c, domain.ErrNotAccountHolder)
		return false
	}
	return true
}

func (h *Handler) GetAccount(c *gin.Context) {
	accountID := c.Param("id")
	if !h.requireOwnership(c, accountID) {
		return
	}
	account, err := h.accounts.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, accountResponse{
		ID: account.ID, AccountNumber: account.AccountNumber,
		ProductType: string(account.ProductType), Status: string(account.Status),
		OverdraftLimitMinor: account.OverdraftLimitMinor,
	})
}

func (h *Handler) GetBalance(c *gin.Context) {
	accountID := c.Param("id")
	if !h.requireOwnership(c, accountID) {
		return
	}
	balance, err := h.accounts.GetBalance(c.Request.Context(), accountID)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, balanceResponse{
		AccountID: balance.AccountID, LedgerBalanceMinor: balance.LedgerBalanceMinor,
		AvailableBalanceMinor: balance.AvailableBalanceMinor,
	})
}

func (h *Handler) Freeze(c *gin.Context) {
	accountID := c.Param("id")
	if !h.requireOwnership(c, accountID) {
		return
	}
	var req freezeRequest
	_ = c.ShouldBindJSON(&req) // reason is optional
	if err := h.accounts.Freeze(c.Request.Context(), accountID, req.Reason); err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"frozen": true})
}

func (h *Handler) Unfreeze(c *gin.Context) {
	accountID := c.Param("id")
	if !h.requireOwnership(c, accountID) {
		return
	}
	if err := h.accounts.Unfreeze(c.Request.Context(), accountID); err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"frozen": false})
}

func (h *Handler) GetStatement(c *gin.Context) {
	accountID := c.Param("id")
	if !h.requireOwnership(c, accountID) {
		return
	}

	var from, to time.Time
	if v := c.Query("from"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apiresponse.BadRequest(c, "INVALID_REQUEST", "from must be an RFC3339 timestamp.")
			return
		}
		from = parsed
	}
	if v := c.Query("to"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apiresponse.BadRequest(c, "INVALID_REQUEST", "to must be an RFC3339 timestamp.")
			return
		}
		to = parsed
	}

	entries, err := h.accounts.GetStatement(c.Request.Context(), accountID, from, to)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, toStatementResponse(entries))
}
