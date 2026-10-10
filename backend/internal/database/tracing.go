package database

import (
	"database/sql"
	"errors"
	"strings"

	"go.getarcane.app/kit/pkg"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

const statementSpanKey = "arcane:otel_span"

var statementTracer = otel.Tracer(tracing.InstrumentationName)

// startStatementSpan opens a client span only inside an existing trace, so
// background queries do not create root traces.
func startStatementSpan(tx *gorm.DB) {
	ctx := tx.Statement.Context
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return
	}
	_, span := statementTracer.Start(ctx, "db", trace.WithSpanKind(trace.SpanKindClient))
	tx.InstanceSet(statementSpanKey, span)
}

// endStatementSpan names and ends the span with the placeholder SQL; parameter values are never recorded.
func endStatementSpan(tx *gorm.DB) {
	value, _ := tx.InstanceGet(statementSpanKey)
	span, ok := value.(trace.Span)
	if !ok {
		return
	}

	query := tx.Statement.SQL.String()
	operation, _, _ := strings.Cut(strings.TrimSpace(query), " ")
	operation = strings.ToLower(operation)
	if name := strings.TrimSpace(operation + " " + tx.Statement.Table); name != "" {
		span.SetName(name)
	}
	span.SetAttributes(
		attribute.String("db.system.name", kit.Ternary(tx.Name() == dbProviderPostgres, "postgresql", "sqlite")),
		attribute.String("db.query.text", query),
		attribute.String("db.operation.name", operation),
		attribute.String("db.collection.name", tx.Statement.Table),
	)
	err := tx.Error
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	tracing.End(span, err)
}
