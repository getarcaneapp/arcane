package middleware

import (
	"context"
	"net/http"
	"strings"

	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

var authenticatedRequests = func() metric.Int64Counter {
	counter, err := otel.Meter(tracing.InstrumentationName).Int64Counter("arcane.auth.requests",
		metric.WithDescription("Authenticated API requests by credential type"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		otel.Handle(err)
	}
	return counter
}()

// RecordAuthenticatedRequest tags the request span with the acting user and
// counts the request by credential type.
func RecordAuthenticatedRequest(ctx context.Context, header http.Header, user *usertypes.Actor) {
	method := "session"
	switch {
	case header.Get(HeaderApiKey) != "":
		method = "api_key"
	case strings.HasPrefix(header.Get("Authorization"), "Bearer "):
		method = "bearer"
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("enduser.id", user.ID),
		attribute.String("user.name", user.Username),
		attribute.String("arcane.auth.method", method),
	)
	authenticatedRequests.Add(ctx, 1, metric.WithAttributes(attribute.String("arcane.auth.method", method)))
}

// SpanEnvironment tags the request span with the routed environment's ID, and with its name once the request
// succeeds, so unauthenticated or unknown-environment requests never trigger a lookup.
func SpanEnvironment(environmentName func(ctx context.Context, id string) string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			ctx := c.Request().Context()
			span := trace.SpanFromContext(ctx)
			id := c.Param("id")
			if id == "" || !strings.HasPrefix(c.Path(), "/api/environments/:id") || !span.IsRecording() {
				return next(c)
			}
			span.SetAttributes(attribute.String("arcane.environment.id", id))
			err := next(c)
			if resp, unwrapErr := echo.UnwrapResponse(c.Response()); err == nil && unwrapErr == nil && resp.Status < http.StatusBadRequest {
				span.SetAttributes(attribute.String("arcane.environment.name", environmentName(ctx, id)))
			}
			return err
		}
	}
}
