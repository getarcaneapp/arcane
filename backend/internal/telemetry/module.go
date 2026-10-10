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

	"github.com/labstack/echo/v5"
	"golang.org/x/time/rate"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
)

// RegisterRoutes adds an unauthenticated relay route per enabled signal; disabled
// signals return 404. Routes share a per-IP limit, a global limit, and a cap on concurrent requests.
func RegisterRoutes(g *echo.Group, service *Service) {
	if service == nil {
		return
	}
	perIP := middleware.PerIPRateLimit(120, 30)
	global := rate.NewLimiter(rate.Every(time.Minute/600), 60)
	inFlight := make(chan struct{}, 32)
	for signal := range service.destinations {
		g.POST("/telemetry/"+signal, func(c *echo.Context) error {
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
		}, perIP)
	}
}
