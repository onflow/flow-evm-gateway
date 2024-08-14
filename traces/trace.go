package traces

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

// RegisterTraceProvider must be called before creation of a tracer
func RegisterTraceProvider(ctx context.Context, logger zerolog.Logger, port uint) error {
	traceProvider, err := newTraceProvider(ctx, logger, port)
	if err != nil {
		logger.Error().Err(err).Msg("failed to create trace provider")
		return err
	}

	otel.SetTracerProvider(traceProvider)
	return nil
}

func newTraceProvider(ctx context.Context, logger zerolog.Logger, port uint) (*tracesdk.TracerProvider, error) {
	endpoint := fmt.Sprintf("localhost:%d", port)
	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithInsecure(), otlptracehttp.WithEndpoint(endpoint))
	if err != nil {
		logger.Error().Err(err).Msg("failed to create trace exporter")
		return nil, err
	}

	traceProvider := tracesdk.NewTracerProvider(tracesdk.WithBatcher(traceExporter, tracesdk.WithBatchTimeout(time.Second)))

	return traceProvider, nil
}
