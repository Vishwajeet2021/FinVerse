package telemetry

import (
	"context"
	"testing"
)

// Init must succeed with the SDK's default resource (its schema URL differs
// across OTel versions; a conflicting merge crashed every service at boot).
func TestInitWithoutExporter(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := Init(context.Background(), "test-service")
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInitWithExporter(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	shutdown, err := Init(context.Background(), "test-service")
	if err != nil {
		t.Fatal(err)
	}
	_ = shutdown(context.Background())
}
