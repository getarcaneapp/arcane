package api

import (
	"net/http"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/webhook"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/labstack/echo/v5"
)

// RegisterWebhookTrigger registers the public (unauthenticated) trigger endpoint.
// The token in the URL is the sole authentication mechanism.
// Rate-limited via PerIPRateLimitForPaths in router_bootstrap.go.
func RegisterWebhookTrigger(g *echo.Group, webhookService *webhook.WebhookService, appCtx handlerutil.ActivityAppContext) {
	g.POST("/webhooks/trigger/:token", func(c *echo.Context) error {
		if webhookService == nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "error": "service not available"})
		}

		token := c.Param("token")
		err := webhookService.TriggerByToken(utils.ActivityRuntimeContext(c.Request().Context(), appCtx.Context()), token)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, webhook.ErrWebhookNotFound) || errors.Is(err, webhook.ErrWebhookInvalid) {
				status = http.StatusNotFound
			} else if errors.Is(err, webhook.ErrWebhookDisabled) {
				status = http.StatusForbidden
			}
			msg := err.Error()
			if status == http.StatusInternalServerError {
				msg = "internal server error"
			}
			return c.JSON(status, map[string]any{"success": false, "error": msg})
		}

		// The action runs in the background; its outcome lands in the event log.
		return c.JSON(http.StatusAccepted, map[string]any{"success": true, "data": map[string]string{"status": "accepted"}})
	})
}
