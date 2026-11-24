package http

import (
	"github.com/finverse/auth-service/internal/domain"
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
	case domain.ErrEmailAlreadyRegistered:
		apiresponse.Conflict(c, "EMAIL_ALREADY_REGISTERED", "An account with this email already exists.")
	case domain.ErrPhoneAlreadyRegistered:
		apiresponse.Conflict(c, "PHONE_ALREADY_REGISTERED", "An account with this phone number already exists.")
	case domain.ErrWeakPassword:
		apiresponse.BadRequest(c, "WEAK_PASSWORD", "Password must be at least 12 characters long.")
	case domain.ErrInvalidCredentials:
		apiresponse.Unauthorized(c, "INVALID_CREDENTIALS", "Incorrect email or password.")
	case domain.ErrAccountLocked:
		apiresponse.Unauthorized(c, "ACCOUNT_LOCKED", "This account is temporarily locked due to repeated failed logins. Please try again later.")
	case domain.ErrAccountDisabled:
		apiresponse.Forbidden(c, "ACCOUNT_DISABLED", "This account has been disabled.")
	case domain.ErrEmailNotVerified:
		apiresponse.Forbidden(c, "EMAIL_NOT_VERIFIED", "Please verify your email address first.")
	case domain.ErrTokenInvalidOrExpired:
		apiresponse.Unauthorized(c, "TOKEN_INVALID_OR_EXPIRED", "This link or token is invalid or has expired.")
	case domain.ErrTokenAlreadyUsed:
		apiresponse.Conflict(c, "TOKEN_ALREADY_USED", "This link or token has already been used.")
	case domain.ErrRefreshTokenReused:
		apiresponse.Unauthorized(c, "SESSION_REVOKED", "Your session was revoked for security reasons. Please log in again.")
	case domain.ErrMFACodeInvalid:
		apiresponse.Unauthorized(c, "MFA_CODE_INVALID", "That code is not right. Check your authenticator app and try again.")
	case domain.ErrMFAChallengeInvalid:
		apiresponse.Unauthorized(c, "MFA_CHALLENGE_INVALID", "This sign-in step has expired. Please log in again.")
	case domain.ErrMFARequired:
		apiresponse.Unauthorized(c, "MFA_REQUIRED", "Two-step verification is required for this account. Please log in again.")
	case domain.ErrMFAAlreadyEnabled:
		apiresponse.Conflict(c, "MFA_ALREADY_ENABLED", "Two-step verification is already set up for this account.")
	case domain.ErrMFANotStarted:
		apiresponse.Conflict(c, "MFA_NOT_STARTED", "Start two-step verification setup before confirming a code.")
	case domain.ErrUserNotFound:
		apiresponse.NotFound(c, "USER_NOT_FOUND", "User not found.")
	default:
		apiresponse.Internal(c, "INTERNAL_ERROR", "Something went wrong on our end. Please try again.")
	}
}
