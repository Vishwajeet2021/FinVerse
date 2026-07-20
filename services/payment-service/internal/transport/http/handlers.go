// Package http implements Payment Service's REST contract per
// docs/09-api-standards.md and docs/07-microservice-specifications.md §7.
package http

import (
	"github.com/finverse/payment-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	payments *service.PaymentService
}

func NewHandler(payments *service.PaymentService) *Handler {
	return &Handler{payments: payments}
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

	payment, err := h.payments.InitiatePayment(c.Request.Context(), req.FromAccountID, req.ToAccountID, req.AmountMinor, key)
	if payment == nil {
		writeDomainError(c, err)
		return
	}
	// A payment record exists either way (COMPLETED or FAILED) — the
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
	apiresponse.OK(c, toPaymentResponse(payment))
}
