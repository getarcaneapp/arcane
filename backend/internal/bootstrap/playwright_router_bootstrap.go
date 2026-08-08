//go:build playwright

package bootstrap

import (
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/v2/api"
	"github.com/getarcaneapp/arcane/backend/v2/internal/playwright"
	"github.com/labstack/echo/v5"
)

func init() {
	registerPlaywrightRoutes = []func(apiGroup *echo.Group, deps api.HandlerDeps){
		func(apiGroup *echo.Group, deps api.HandlerDeps) {
			apiKeyService := deps.ApiKey.Service()
			userService := deps.User.Service()
			if apiKeyService == nil || userService == nil || deps.Federated == nil {
				slog.Warn("Playwright service not available, skipping playwright routes")
				return
			}

			playwrightService := playwright.NewPlaywrightService(apiKeyService, userService)
			playwright.SetupRoutes(apiGroup, playwrightService, deps.Federated)
			slog.Info("Playwright routes registered for E2E testing")
		},
	}
}
