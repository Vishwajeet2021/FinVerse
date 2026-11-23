// Package sender is the delivery-channel boundary. Phase 3 ships a logging
// "email" sender (docs/19-implementation-roadmap.md Phase 3: SMS/push can be
// stubbed loggers), plus an outage switch so the retry/DLQ path can be
// demonstrated and load-tested without a real provider.
package sender

import (
	"context"
	"errors"
	"sync/atomic"

	"go.uber.org/zap"
)

// ErrProviderDown is what a failing provider looks like to the service.
var ErrProviderDown = errors.New("email provider unavailable")

type Message struct {
	To      string
	Subject string
	Body    string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender "delivers" by logging, and can be flipped into a simulated
// provider outage. The outage flag is how the roadmap's exit criterion
// ("killing the Notification Service doesn't affect payment success", and the
// DLQ demo) is exercised on demand.
type LogSender struct {
	logger  *zap.Logger
	failing atomic.Bool
	sent    atomic.Int64
}

func NewLogSender(logger *zap.Logger) *LogSender { return &LogSender{logger: logger} }

func (s *LogSender) SetFailing(v bool) { s.failing.Store(v) }
func (s *LogSender) Failing() bool     { return s.failing.Load() }
func (s *LogSender) Sent() int64       { return s.sent.Load() }

func (s *LogSender) Send(_ context.Context, m Message) error {
	if s.failing.Load() {
		return ErrProviderDown
	}
	s.sent.Add(1)
	s.logger.Info("notification delivered", zap.String("to", m.To), zap.String("subject", m.Subject))
	return nil
}
