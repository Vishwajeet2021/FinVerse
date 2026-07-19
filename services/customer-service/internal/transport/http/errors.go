package http

import (
	"github.com/finverse/customer-service/internal/domain"
	"github.com/finverse/shared/apiresponse"
	"github.com/gin-gonic/gin"
)

// writeDomainError maps a domain error to the HTTP status/code/message triple
// per docs/09-api-standards.md §2.4-2.5. Any error not in this table is
// treated as an unexpected infrastructure failure (500) — domain errors are
// the *only* errors allowed to reach a handler as a sentinel the handler
// branches on, per docs/12-coding-standards.md §1.3.
func writeDomainError(c *gin.Context, err error) {
	switch err {
	case domain.ErrCustomerNotFound:
		apiresponse.NotFound(c, "CUSTOMER_NOT_FOUND", "Customer profile not found.")
	case domain.ErrProfileAlreadyExists:
		apiresponse.Conflict(c, "PROFILE_ALREADY_EXISTS", "A customer profile already exists for this account.")
	case domain.ErrInvalidKYCTransition:
		apiresponse.UnprocessableEntity(c, "INVALID_KYC_TRANSITION", "This KYC status change is not allowed from the current state.")
	case domain.ErrBeneficiaryNotFound:
		apiresponse.NotFound(c, "BENEFICIARY_NOT_FOUND", "Beneficiary not found.")
	case domain.ErrDocumentNotFound:
		apiresponse.NotFound(c, "DOCUMENT_NOT_FOUND", "Document not found.")
	default:
		apiresponse.Internal(c, "INTERNAL_ERROR", "Something went wrong on our end. Please try again.")
	}
}
