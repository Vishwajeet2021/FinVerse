// Package http implements Payment Service's REST contract per
// docs/09-api-standards.md and docs/07-microservice-specifications.md §7.
package http

import (
	"context"
	"errors"
	"strings"

	"github.com/finverse/payment-service/internal/domain"
	"github.com/finverse/payment-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/jwtauth"
	"github.com/gin-gonic/gin"
)

// OwnershipVerifier confirms the authenticated caller holds an account. The
// service layer deliberately never sees the caller's token; ownership is an
// edge concern checked before any payment, schedule or instruction is created.
type OwnershipVerifier interface {
	VerifyOwnership(ctx context.Context, userID, bearer, accountID string) error
}

type Handler struct {
	payments  *service.PaymentService
	ownership OwnershipVerifier
}

func NewHandler(payments *service.PaymentService, ownership OwnershipVerifier) *Handler {
	return &Handler{payments: payments, ownership: ownership}
}

// callerID is the authenticated user (JWT subject).
func callerID(c *gin.Context) string {
	if claims := jwtauth.ClaimsFromContext(c); claims != nil {
		return claims.Subject
	}
	return ""
}

func bearerToken(c *gin.Context) string {
	return strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
}

// requireHolder aborts with 403 unless the caller holds accountID.
func (h *Handler) requireHolder(c *gin.Context, accountID string) bool {
	if err := h.ownership.VerifyOwnership(c.Request.Context(), callerID(c), bearerToken(c), accountID); err != nil {
		writeDomainError(c, err)
		return false
	}
	return true
}

func parseRail(s string) domain.Rail {
	if s == "" {
		return domain.RailInternal
	}
	return domain.Rail(strings.ToUpper(s))
}

// idempotencyKey reads the required Idempotency-Key header
// (docs/09-api-standards.md §2.6). Unlike Wallet Service's topup/transfer,
// this endpoint REQUIRES the header rather than generating one server-side —
// a payment is the one operation in this system where "the client didn't
// send an idempotency key" is itself a client error worth surfacing, not
// something to paper over.
func (h *Handler) InitiatePayment(c *gin.Context) {
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		apiresponse.BadRequest(c, "MISSING_IDEMPOTENCY_KEY", "An Idempotency-Key header is required for payments.")
		return
	}

	var req initiatePaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}
	if !h.requireHolder(c, req.FromAccountID) {
		return
	}

	payment, err := h.payments.InitiatePaymentOnRail(c.Request.Context(), parseRail(req.Rail), req.FromAccountID, req.ToAccountNumber, req.AmountMinor, key)
	if payment == nil {
		writeDomainError(c, err)
		return
	}
	// A payment record exists either way (COMPLETED, PROCESSING or FAILED) — the
	// resource itself was created successfully, so 201 either way; the
	// client reads the outcome from the `status` field, per
	// docs/09-api-standards.md's payment status lifecycle, not from the
	// HTTP status code alone.
	apiresponse.Created(c, toPaymentResponse(payment))
}

func (h *Handler) GetPayment(c *gin.Context) {
	payment, err := h.payments.GetPayment(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDomainError(c, err)
		return
	}
	// Only a holder of the paying account may read the payment. A non-holder gets the same
	// 404 as a missing payment so payment ids cannot be probed (docs/22 D-S3).
	if err := h.ownership.VerifyOwnership(c.Request.Context(), callerID(c), bearerToken(c), payment.FromAccountID); err != nil {
		if errors.Is(err, domain.ErrNotAccountHolder) {
			writeDomainError(c, domain.ErrPaymentNotFound)
			return
		}
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, toPaymentResponse(payment))
}

func (h *Handler) bindSchedule(c *gin.Context) (scheduleRequest, service.ScheduleRequest, bool) {
	var req scheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return req, service.ScheduleRequest{}, false
	}
	if !h.requireHolder(c, req.FromAccountID) {
		return req, service.ScheduleRequest{}, false
	}
	return req, service.ScheduleRequest{
		CreatedBy: callerID(c), FromAccountID: req.FromAccountID, ToAccountNumber: req.ToAccountNumber,
		AmountMinor: req.AmountMinor, Rail: parseRail(req.Rail),
	}, true
}

func (h *Handler) CreateScheduled(c *gin.Context) {
	req, sreq, ok := h.bindSchedule(c)
	if !ok {
		return
	}
	sp, err := h.payments.ScheduleOnce(c.Request.Context(), sreq, req.RunAt)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.Created(c, toScheduledResponse(*sp))
}

func (h *Handler) ListScheduled(c *gin.Context) {
	items, err := h.payments.ListScheduled(c.Request.Context(), callerID(c))
	if err != nil {
		writeDomainError(c, err)
		return
	}
	out := make([]scheduledResponse, 0, len(items))
	for _, s := range items {
		out = append(out, toScheduledResponse(s))
	}
	apiresponse.OK(c, out)
}

func (h *Handler) CancelScheduled(c *gin.Context) {
	if err := h.payments.CancelScheduled(c.Request.Context(), callerID(c), c.Param("id")); err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.NoContent(c)
}

func (h *Handler) CreateStanding(c *gin.Context) {
	req, sreq, ok := h.bindSchedule(c)
	if !ok {
		return
	}
	si, err := h.payments.CreateStanding(c.Request.Context(), sreq, domain.Frequency(strings.ToUpper(req.Frequency)), req.FirstRunAt)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.Created(c, toStandingResponse(*si))
}

func (h *Handler) ListStanding(c *gin.Context) {
	items, err := h.payments.ListStanding(c.Request.Context(), callerID(c))
	if err != nil {
		writeDomainError(c, err)
		return
	}
	out := make([]standingResponse, 0, len(items))
	for _, s := range items {
		out = append(out, toStandingResponse(s))
	}
	apiresponse.OK(c, out)
}

func (h *Handler) PauseStanding(c *gin.Context) {
	h.standingAction(c, h.payments.PauseStanding)
}

func (h *Handler) ResumeStanding(c *gin.Context) {
	h.standingAction(c, h.payments.ResumeStanding)
}

func (h *Handler) CancelStanding(c *gin.Context) {
	h.standingAction(c, h.payments.CancelStanding)
}

func (h *Handler) standingAction(c *gin.Context, fn func(context.Context, string, string) error) {
	if err := fn(c.Request.Context(), callerID(c), c.Param("id")); err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.NoContent(c)
}
