package api

import (
	"errors"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/labstack/echo/v4"
)

// RegisterWebhookTrigger registers the public (unauthenticated) trigger endpoint.
// The token in the URL is the sole authentication mechanism.
// Rate-limited via PerIPRateLimitForPaths in router_bootstrap.go.
func RegisterWebhookTrigger(g *echo.Group, webhookService *services.WebhookService) {
	g.POST("/webhooks/trigger/:token", func(c echo.Context) error {
		if webhookService == nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "error": "service not available"})
		}

		token := c.Param("token")
		result, err := webhookService.TriggerByToken(c.Request().Context(), token)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, services.ErrWebhookNotFound) || errors.Is(err, services.ErrWebhookInvalid) {
				status = http.StatusNotFound
			} else if errors.Is(err, services.ErrWebhookDisabled) {
				status = http.StatusForbidden
			}
			msg := err.Error()
			if status == http.StatusInternalServerError {
				msg = "internal server error"
			}
			return c.JSON(status, map[string]any{"success": false, "error": msg})
		}

		return c.JSON(http.StatusOK, map[string]any{"success": true, "data": result})
	})
}
