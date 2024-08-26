package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type Tracer interface {
	Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span)
	WithinSpan(ctx context.Context, operationName string, f func(), opts ...trace.SpanStartOption)
}

type DefaultTracer struct {
	tracer trace.Tracer
}

func NewTracer(tracerName string, opts ...trace.TracerOption) (Tracer, error) {
	return &DefaultTracer{
		tracer: otel.Tracer(tracerName, opts...),
	}, nil
}

func (t *DefaultTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, spanName, opts...)
}

func (t *DefaultTracer) WithinSpan(ctx context.Context, operationName string, f func(), opts ...trace.SpanStartOption) {
	_, span := t.tracer.Start(ctx, operationName, opts...)
	defer span.End()

	f()
}
