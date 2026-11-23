package logging

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// TraceFields returns trace_id/span_id for the span active in ctx, or nil.
// Shared by gRPC and event-consumer logging so every log line that belongs to
// a request can be joined to its Jaeger trace.
func TraceFields(ctx context.Context) []zap.Field {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []zap.Field{zap.String("trace_id", sc.TraceID().String()), zap.String("span_id", sc.SpanID().String())}
}

// UnaryServerInterceptor logs one line per gRPC call, with the trace ids. The
// otelgrpc stats handler runs before interceptors, so the span already exists.
// It also stores a trace-aware logger in the context for FromContext.
func UnaryServerInterceptor(base *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		l := base.With(TraceFields(ctx)...)
		ctx = context.WithValue(ctx, loggerCtxKey, l)
		resp, err := handler(ctx, req)
		l.Info("grpc call completed",
			zap.String("method", info.FullMethod),
			zap.String("code", status.Code(err).String()),
			zap.Duration("duration", time.Since(start)),
		)
		return resp, err
	}
}
