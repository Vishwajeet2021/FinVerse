// Package http implements Auth Service's REST contract per
// docs/09-api-standards.md. Handlers validate input, call internal/service,
// and map results to the standard envelope — no business logic lives here,
// per docs/12-coding-standards.md §1.1.
package http

import (
	"github.com/finverse/auth-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/jwtauth"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	auth   *service.AuthService
	cookie CookieConfig
}

func NewHandler(auth *service.AuthService, cookie CookieConfig) *Handler {
	return &Handler{auth: auth, cookie: cookie}
}

// Register implements FR-AUTH-1.
func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	user, err := h.auth.Register(c.Request.Context(), req.Email, req.Phone, req.Password)
	if err != nil {
		if user == nil {
			writeDomainError(c, err)
			return
		}
		// The account was created but the verification email failed to send
		// (internal/service/auth_service.go documents this as non-fatal to
		// registration) — still return 201 with the user, since the primary
		// operation succeeded.
	}

	apiresponse.Created(c, toUserResponse(user))
}

// VerifyEmail implements FR-AUTH-2.
func (h *Handler) VerifyEmail(c *gin.Context) {
	var req verifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	if err := h.auth.VerifyEmail(c.Request.Context(), req.Token); err != nil {
		writeDomainError(c, err)
		return
	}

	apiresponse.OK(c, gin.H{"verified": true})
}

// Login implements FR-AUTH-3.
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	pair, err := h.auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		writeDomainError(c, err)
		return
	}

	setRefreshCookie(c, h.cookie, pair.RefreshToken, pair.RefreshTokenExpiresAt)
	apiresponse.OK(c, tokenResponse{AccessToken: pair.AccessToken, TokenType: "Bearer"})
}

// Refresh implements FR-AUTH-6. The refresh token comes from the HttpOnly
// cookie set by Login/Refresh — never from the request body — per
// docs/10-authentication-authorization.md §2.
func (h *Handler) Refresh(c *gin.Context) {
	rawRefreshToken, err := readRefreshCookie(c)
	if err != nil {
		apiresponse.Unauthorized(c, "MISSING_REFRESH_TOKEN", "Your session has expired. Please log in again.")
		return
	}

	pair, err := h.auth.Refresh(c.Request.Context(), rawRefreshToken)
	if err != nil {
		clearRefreshCookie(c, h.cookie)
		writeDomainError(c, err)
		return
	}

	setRefreshCookie(c, h.cookie, pair.RefreshToken, pair.RefreshTokenExpiresAt)
	apiresponse.OK(c, tokenResponse{AccessToken: pair.AccessToken, TokenType: "Bearer"})
}

// Logout implements FR-AUTH-7 (single session).
func (h *Handler) Logout(c *gin.Context) {
	rawRefreshToken, err := readRefreshCookie(c)
	if err != nil {
		// No cookie present — already logged out from this browser's point of view.
		apiresponse.NoContent(c)
		return
	}

	if err := h.auth.Logout(c.Request.Context(), rawRefreshToken); err != nil {
		writeDomainError(c, err)
		return
	}
	clearRefreshCookie(c, h.cookie)

	apiresponse.NoContent(c)
}

// LogoutAll implements FR-AUTH-7 (revoke every session) — requires a valid
// access token, since it acts on "the current user", not a token in the body.
func (h *Handler) LogoutAll(c *gin.Context) {
	claims := jwtauth.ClaimsFromContext(c)
	if err := h.auth.LogoutAll(c.Request.Context(), claims.Subject); err != nil {
		writeDomainError(c, err)
		return
	}
	clearRefreshCookie(c, h.cookie)
	apiresponse.NoContent(c)
}

// RequestPasswordReset implements FR-AUTH-8.
func (h *Handler) RequestPasswordReset(c *gin.Context) {
	var req passwordResetRequestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	if err := h.auth.RequestPasswordReset(c.Request.Context(), req.Email); err != nil {
		writeDomainError(c, err)
		return
	}

	// Always 200 regardless of whether the email existed — see
	// internal/service/password_reset.go on enumeration defense.
	apiresponse.OK(c, gin.H{"message": "If that email is registered, a reset link has been sent."})
}

// ConfirmPasswordReset implements FR-AUTH-8.
func (h *Handler) ConfirmPasswordReset(c *gin.Context) {
	var req passwordResetConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	if err := h.auth.ResetPassword(c.Request.Context(), req.Token, req.NewPassword); err != nil {
		writeDomainError(c, err)
		return
	}

	apiresponse.OK(c, gin.H{"reset": true})
}
