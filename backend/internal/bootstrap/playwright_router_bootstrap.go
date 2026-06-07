//go:build playwright

package bootstrap

import (
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/api"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/labstack/echo/v4"
)

func init() {
	registerPlaywrightRoutes = []func(apiGroup *echo.Group, services *Services){
		func(apiGroup *echo.Group, svc *Services) {
			playwrightService := services.NewPlaywrightService(svc.ApiKey, svc.User)
			if playwrightService == nil {
				slog.Warn("Playwright service not available, skipping playwright routes")
				return
			}

			api.SetupPlaywrightRoutes(apiGroup, playwrightService)
			slog.Info("Playwright routes registered for E2E testing")
		},
	}
}
