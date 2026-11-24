package http

import (
	"errors"

	"github.com/finverse/payment-service/internal/domain"
	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
)

func writeDomainError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrPaymentNotFound):
		apiresponse.NotFound(c, "PAYMENT_NOT_FOUND", "Payment not found.")
	case errors.Is(err, domain.ErrInvalidAmount):
		apiresponse.BadRequest(c, "INVALID_AMOUNT", "Amount must be a positive number.")
	case errors.Is(err, domain.ErrSameAccount):
		apiresponse.BadRequest(c, "SAME_ACCOUNT", "You can't transfer to the same account.")
	case errors.Is(err, domain.ErrRecipientAccountNotFound):
		apiresponse.NotFound(c, "RECIPIENT_ACCOUNT_NOT_FOUND", "No account exists with that account number.")
	case errors.Is(err, domain.ErrSourceAccountFrozen):
		apiresponse.UnprocessableEntity(c, "ACCOUNT_FROZEN", "This account is frozen and cannot make payments. Please contact support.")
	case errors.Is(err, domain.ErrSourceAccountClosed):
		apiresponse.UnprocessableEntity(c, "ACCOUNT_CLOSED", "This account is closed.")
	case errors.Is(err, domain.ErrRailLimit):
		apiresponse.BadRequest(c, "RAIL_LIMIT", err.Error())
	case errors.Is(err, domain.ErrUnknownRail):
		apiresponse.BadRequest(c, "UNKNOWN_RAIL", "Rail must be one of INTERNAL, UPI, IMPS, NEFT, RTGS.")
	case errors.Is(err, domain.ErrScheduleInPast):
		apiresponse.BadRequest(c, "SCHEDULE_IN_PAST", "The date must be in the future.")
	case errors.Is(err, domain.ErrInvalidFrequency):
		apiresponse.BadRequest(c, "INVALID_FREQUENCY", "Frequency must be DAILY, WEEKLY or MONTHLY.")
	case errors.Is(err, domain.ErrNotAccountHolder):
		apiresponse.Forbidden(c, "NOT_ACCOUNT_HOLDER", "You do not hold that account.")
	case errors.Is(err, domain.ErrScheduleNotFound):
		apiresponse.NotFound(c, "SCHEDULE_NOT_FOUND", "No such schedule.")
	case errors.Is(err, domain.ErrScheduleNotPending):
		apiresponse.Conflict(c, "SCHEDULE_NOT_ACTIVE", "That schedule is no longer active.")
	default:
		apiresponse.Internal(c, "INTERNAL_ERROR", "Something went wrong on our end. Please try again.")
	}
}
