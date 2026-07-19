// Package logging provides the structured JSON logger every FinVerse service
// uses, per docs/12-coding-standards.md §1.5 and docs/16-observability.md §5.
package logging

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New builds a production-shaped, JSON-structured zap logger tagged with the
// owning service name. Every log line carries "service" so a shared Loki/CloudWatch
// sink can be filtered per service without relying on the log stream's origin alone.
func New(serviceName string, level zapcore.Level) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(level)
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}
	return logger.With(zap.String("service", serviceName)), nil
}
