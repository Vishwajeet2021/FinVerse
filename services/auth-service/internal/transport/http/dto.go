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

// loginResponse is either a session (accessToken) or a pending second step (mfaRequired /
// mfaEnrollmentRequired plus a challengeToken). The challenge token proves only that the password
// step passed: it opens no API and expires in minutes.
type loginResponse struct {
	AccessToken           string `json:"accessToken,omitempty"`
	TokenType             string `json:"tokenType,omitempty"`
	MFARequired           bool   `json:"mfaRequired,omitempty"`
	MFAEnrollmentRequired bool   `json:"mfaEnrollmentRequired,omitempty"`
	ChallengeToken        string `json:"challengeToken,omitempty"`
}

type mfaVerifyRequest struct {
	ChallengeToken string `json:"challengeToken" binding:"required,max=256"`
	// Six digits from the authenticator app, or a recovery code (XXXXX-XXXXX).
	Code string `json:"code" binding:"required,min=6,max=16"`
}

type mfaChallengeRequest struct {
	ChallengeToken string `json:"challengeToken" binding:"required,max=256"`
}

type mfaEnrollConfirmRequest struct {
	ChallengeToken string `json:"challengeToken" binding:"required,max=256"`
	Code           string `json:"code" binding:"required,min=6,max=16"`
}

type mfaSetupBeginRequest struct {
	Password string `json:"password" binding:"required,max=128"`
}

type mfaSetupConfirmRequest struct {
	Code string `json:"code" binding:"required,min=6,max=16"`
}

type enrollmentResponse struct {
	Secret     string `json:"secret"`
	OtpauthURI string `json:"otpauthUri"`
}

// recoveryCodesResponse is the only time the recovery codes are ever shown.
type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

// enrollmentSessionResponse completes a sign-in that began with a required enrolment.
type enrollmentSessionResponse struct {
	AccessToken   string   `json:"accessToken"`
	TokenType     string   `json:"tokenType"`
	RecoveryCodes []string `json:"recoveryCodes"`
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
