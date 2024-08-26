package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// RegisterTraceProvider must be called before creation of a tracer
func RegisterTraceProvider(ctx context.Context, serviceName string) error {
	traceProvider, err := newTraceProvider(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("failed to create trace provider: %w", err)
	}

	otel.SetTracerProvider(traceProvider)
	return nil
}

func newTraceProvider(ctx context.Context, serviceName string) (*tracesdk.TracerProvider, error) {
	resource, err := resource.New(
		ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String(serviceName)),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Connection parameters for the exporter are extracted from environment variables
	// e.g. `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`
	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	traceProvider := tracesdk.NewTracerProvider(
		tracesdk.WithResource(resource),
		tracesdk.WithBatcher(traceExporter),
	)

	return traceProvider, nil
}
