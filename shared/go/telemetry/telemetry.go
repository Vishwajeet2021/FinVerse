// Package telemetry wires OpenTelemetry tracing for every FinVerse service
// (docs/16-observability.md §3, NFR-OBS-1). One call in main() gives a
// service W3C trace-context propagation, OTLP/HTTP export to Jaeger, and the
// gin/gRPC/eventbus instrumentation hooks that make a single trace span the
// whole request → ledger → event fan-out path.
package telemetry

import (
	"context"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/stats"
)

// Init installs the global tracer provider and W3C propagator. It is a no-op
// export-wise when OTEL_EXPORTER_OTLP_ENDPOINT is unset (spans are still
// created and propagated, so trace IDs in logs/events stay consistent, but
// nothing leaves the process) — local `go run` and unit tests need no collector.
// The returned func flushes buffered spans and must be deferred by main().
func Init(ctx context.Context, serviceName string) (shutdown func(context.Context) error, err error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("deployment.environment.name", os.Getenv("ENVIRONMENT")),
	))
	if err != nil {
		return nil, err
	}

	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exp, err := otlptracehttp.New(ctx) // endpoint/insecure come from the standard OTEL_* env vars
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(time.Second)))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// GinMiddleware creates a server span per request and extracts an incoming
// traceparent header, so a call from another service continues its trace.
func GinMiddleware(serviceName string) gin.HandlerFunc {
	return withMetrics(serviceName, otelgin.Middleware(serviceName))
}

// GRPCServerStats / GRPCClientStats are passed to grpc.NewServer /
// grpc.NewClient so ledger postings show up as child spans of the payment.
func GRPCServerStats() stats.Handler { return otelgrpc.NewServerHandler() }
func GRPCClientStats() stats.Handler { return otelgrpc.NewClientHandler() }

// TraceParent returns the W3C traceparent of the span in ctx, or nil when
// there is none. Services store it on each outbox row so the relay — which
// runs on a timer, long after the request span ended — can publish the event
// as a child of the request that caused it.
func TraceParent(ctx context.Context) *string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if tp := carrier["traceparent"]; tp != "" {
		return &tp
	}
	return nil
}

// Str dereferences an optional string ("" for nil).
func Str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
