package http

import (
	"github.com/finverse/payment-service/internal/domain"
	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
)

func writeDomainError(c *gin.Context, err error) {
	switch err {
	case domain.ErrPaymentNotFound:
		apiresponse.NotFound(c, "PAYMENT_NOT_FOUND", "Payment not found.")
	case domain.ErrInvalidAmount:
		apiresponse.BadRequest(c, "INVALID_AMOUNT", "Amount must be a positive number.")
	case domain.ErrSameAccount:
		apiresponse.BadRequest(c, "SAME_ACCOUNT", "You can't transfer to the same account.")
	default:
		apiresponse.Internal(c, "INTERNAL_ERROR", "Something went wrong on our end. Please try again.")
	}
}
