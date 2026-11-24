package http

import "github.com/finverse/auth-service/internal/domain"

type registerRequest struct {
	Email    string `json:"email" binding:"required,email,max=254"`
	Phone    string `json:"phone" binding:"omitempty,e164"`
	Password string `json:"password" binding:"required,max=128"`
}

type userResponse struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Phone         string `json:"phone,omitempty"`
	Role          string `json:"role"`
	EmailVerified bool   `json:"emailVerified"`
	Status        string `json:"status"`
}

func toUserResponse(u *domain.User) userResponse {
	return userResponse{
		ID:            u.ID,
		Email:         u.Email,
		Phone:         u.Phone,
		Role:          string(u.Role),
		EmailVerified: u.EmailVerified,
		Status:        string(u.Status),
	}
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,max=128"`
}

// tokenResponse deliberately omits the refresh token — it travels only as
// the HttpOnly cookie set by cookie.go, per
// docs/10-authentication-authorization.md §2. Putting it in both places
// would defeat the point of HttpOnly (any XSS that can read the JSON
// response could then read the refresh token too).
type tokenResponse struct {
	AccessToken string `json:"accessToken"`
	TokenType   string `json:"tokenType"`
}

type verifyEmailRequest struct {
	Token string `json:"token" binding:"required,max=256"`
}

type passwordResetRequestRequest struct {
	Email string `json:"email" binding:"required,email,max=254"`
}

type passwordResetConfirmRequest struct {
	Token       string `json:"token" binding:"required,max=256"`
	NewPassword string `json:"newPassword" binding:"required,max=128"`
}
