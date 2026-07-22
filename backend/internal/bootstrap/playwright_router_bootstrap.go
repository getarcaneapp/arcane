//go:build playwright

package bootstrap

import (
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/v2/api"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/labstack/echo/v4"
)

func init() {
	registerPlaywrightRoutes = []func(apiGroup *echo.Group, deps api.HandlerDeps){
		func(apiGroup *echo.Group, deps api.HandlerDeps) {
			playwrightService := services.NewPlaywrightService(deps.ApiKey, deps.User, deps.Federated)
			if playwrightService == nil {
				slog.Warn("Playwright service not available, skipping playwright routes")
				return
			}

			api.SetupPlaywrightRoutes(apiGroup, playwrightService)
			slog.Info("Playwright routes registered for E2E testing")
		},
	}
}
