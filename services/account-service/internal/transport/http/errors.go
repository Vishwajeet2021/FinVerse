package http

import (
	"github.com/finverse/account-service/internal/domain"
	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
)

func writeDomainError(c *gin.Context, err error) {
	switch err {
	case domain.ErrAccountNotFound:
		apiresponse.NotFound(c, "ACCOUNT_NOT_FOUND", "Account not found.")
	case domain.ErrAccountFrozen:
		apiresponse.UnprocessableEntity(c, "ACCOUNT_FROZEN", "This account is currently frozen and cannot accept debits.")
	case domain.ErrAccountClosed:
		apiresponse.UnprocessableEntity(c, "ACCOUNT_CLOSED", "This account is closed.")
	case domain.ErrAccountAlreadyFrozen:
		apiresponse.Conflict(c, "ACCOUNT_ALREADY_FROZEN", "This account is already frozen.")
	case domain.ErrAccountNotFrozen:
		apiresponse.Conflict(c, "ACCOUNT_NOT_FROZEN", "This account is not frozen.")
	case domain.ErrInvalidProductType:
		apiresponse.BadRequest(c, "INVALID_PRODUCT_TYPE", "Product type must be one of SAVINGS, CURRENT, JOINT, VIRTUAL.")
	case domain.ErrNotAccountHolder:
		apiresponse.Forbidden(c, "NOT_ACCOUNT_HOLDER", "You don't have access to this account.")
	case domain.ErrInsufficientKYCTier:
		apiresponse.Forbidden(c, "INSUFFICIENT_KYC_TIER", "Your account verification tier does not permit this action.")
	default:
		apiresponse.Internal(c, "INTERNAL_ERROR", "Something went wrong on our end. Please try again.")
	}
}
