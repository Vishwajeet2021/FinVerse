package service

import (
	"context"

	"go.uber.org/zap"
)

// ConsoleMailer satisfies the Mailer interface by logging the email instead
// of sending it — a deliberate, documented Phase 1 stand-in (see interfaces.go)
// for local development, before Notification Service exists. Swap this for
// an SES-backed implementation behind the same interface when that becomes
// the right call; nothing else in AuthService needs to change.
type ConsoleMailer struct {
	logger  *zap.Logger
	baseURL string // e.g. http://localhost:5173 — used to build the link a real email would contain
}

func NewConsoleMailer(logger *zap.Logger, frontendBaseURL string) *ConsoleMailer {
	return &ConsoleMailer{logger: logger, baseURL: frontendBaseURL}
}

func (m *ConsoleMailer) SendVerificationEmail(_ context.Context, toEmail, token string) error {
	m.logger.Info("dev mailer: verification email",
		zap.String("to", toEmail),
		zap.String("link", m.baseURL+"/verify-email?token="+token),
	)
	return nil
}

func (m *ConsoleMailer) SendPasswordResetEmail(_ context.Context, toEmail, token string) error {
	m.logger.Info("dev mailer: password reset email",
		zap.String("to", toEmail),
		zap.String("link", m.baseURL+"/reset-password?token="+token),
	)
	return nil
}
