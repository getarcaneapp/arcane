// Package tracing holds shared helpers for Arcane's own OpenTelemetry spans.
package tracing

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentationName identifies spans, metrics, and logs emitted by Arcane code.
const InstrumentationName = "github.com/getarcaneapp/arcane/backend"

// End records err on span, marks the span failed, and ends it. Context
// cancellation is treated as a normal outcome.
func End(span trace.Span, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
