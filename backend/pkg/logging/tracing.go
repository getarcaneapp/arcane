package logging

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// InstrumentationName identifies spans, metrics, and logs emitted by Arcane code.
	InstrumentationName = "getarcane.dev/arcane/v3"
)

// End records err on span, marks the span failed, and ends it. Context
// cancellation is treated as a normal outcome.
func EndSpan(span trace.Span, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
