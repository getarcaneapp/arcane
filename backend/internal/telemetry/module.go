// Package telemetry relays browser OpenTelemetry data to the collector configured
// through the standard OTEL_* exporter, endpoint, protocol, and header variables.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"
	"golang.org/x/time/rate"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
)

func RegisterRoutes(api huma.API, service *Service) {
	global := rate.NewLimiter(rate.Every(time.Minute/600), 60)
	inFlight := make(chan struct{}, 32)
	huma.Register(api, huma.Operation{
		OperationID: "relay-browser-telemetry",
		Method:      http.MethodPost,
		Path:        "/telemetry/{signal}",
		Summary:     "Relay browser telemetry",
		Tags:        []string{"Telemetry"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessPublic},
		Middlewares: huma.Middlewares{middleware.EchoMiddleware(middleware.PerIPRateLimit(120, 30))},
	}, middleware.EchoHandler(func(c *echo.Context) error {
		signal := c.Param("signal")
		if _, relayed := kit.FromPtr(service).destinations[signal]; !relayed {
			return c.NoContent(http.StatusNotFound)
		}
		req := c.Request()
		if !global.Allow() {
			c.Response().Header().Set("Retry-After", "60")
			return c.NoContent(http.StatusTooManyRequests)
		}
		select {
		case inFlight <- struct{}{}:
			defer func() { <-inFlight }()
		default:
			return c.NoContent(http.StatusTooManyRequests)
		}
		// Slow uploads must not hold a relay slot indefinitely.
		if err := http.NewResponseController(c.Response()).SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			slog.DebugContext(req.Context(), "Failed to set relay read deadline", "error", err)
		}
		if mediaType, _, _ := mime.ParseMediaType(req.Header.Get("Content-Type")); mediaType != "application/json" {
			return c.NoContent(http.StatusUnsupportedMediaType)
		}
		status, err := service.Forward(context.WithoutCancel(req.Context()), signal, req.Body)
		switch {
		case errors.Is(err, ErrPayloadTooLarge):
			return c.NoContent(http.StatusRequestEntityTooLarge)
		case errors.Is(err, ErrInvalidPayload):
			return c.NoContent(http.StatusBadRequest)
		case errors.Is(err, os.ErrDeadlineExceeded):
			return c.NoContent(http.StatusRequestTimeout)
		case err != nil:
			slog.DebugContext(req.Context(), "Failed to relay browser telemetry", "signal", signal, "error", err)
			return c.NoContent(http.StatusBadGateway)
		}
		return c.NoContent(status)
	}))
}
