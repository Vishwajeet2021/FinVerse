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

// ResolveAccountNumber is an internal, service-to-service endpoint (no JWT
// middleware — internal network trust, the same posture as Customer
// Service's GetKYCTierByUserID) that lets Payment Service turn the account
// number a payer actually knows into the internal account ID the ledger's
// participant_id column requires. Returns 404 if no account has that number.
func (h *Handler) ResolveAccountNumber(c *gin.Context) {
	account, err := h.accounts.ResolveAccountNumber(c.Request.Context(), c.Param("accountNumber"))
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"id": account.ID, "status": string(account.Status)})
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

// AccountStatus lets Payment Service ask whether an account may be debited, without a user
// token (scheduled and standing payments run with none). It reveals only the status.
func (h *Handler) AccountStatus(c *gin.Context) {
	a, err := h.accounts.GetAccount(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"id": a.ID, "status": string(a.Status)})
}

// AdminFreeze and AdminUnfreeze are internal, service-token protected (Admin Service only).
// The human operator's identity and the audit record live in Admin Service; here the
// account just records that an administrator, not the customer, holds the freeze.
func (h *Handler) AdminFreeze(c *gin.Context) {
	var req freezeRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.accounts.FreezeAs(c.Request.Context(), c.Param("id"), req.Reason, domain.ActorAdmin); err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"frozen": true})
}

func (h *Handler) AdminUnfreeze(c *gin.Context) {
	if err := h.accounts.UnfreezeAs(c.Request.Context(), c.Param("id"), domain.ActorAdmin); err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"frozen": false})
}

// AdminGetAccount lets Admin Service look an account up by id without being a holder.
func (h *Handler) AdminGetAccount(c *gin.Context) {
	a, err := h.accounts.GetAccount(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{
		"id": a.ID, "customerId": a.CustomerID, "accountNumber": a.AccountNumber,
		"productType": string(a.ProductType), "status": string(a.Status), "frozenBy": string(a.FrozenBy),
	})
}

// Close implements FR-ACC-5's terminal transition. Unlike Freeze/Unfreeze,
// there's no request body -- there's no "reason" or reversible state to
// capture, only a one-way door gated by domain.ErrAccountHasBalance.
func (h *Handler) Close(c *gin.Context) {
	accountID := c.Param("id")
	if !h.requireOwnership(c, accountID) {
		return
	}
	if err := h.accounts.CloseAccount(c.Request.Context(), accountID); err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"closed": true})
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
