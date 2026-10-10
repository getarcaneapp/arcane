//go:build playwright

package bootstrap

import (
	"context"
	"log/slog"

	"github.com/labstack/echo/v5"

	"github.com/getarcaneapp/arcane/backend/v2/api"
	"github.com/getarcaneapp/arcane/backend/v2/internal/playwright"
)

func init() {
	ctx := context.Background() //nolint:forbidigo // Package init has no request context.
	registerPlaywrightRoutes = []func(apiGroup *echo.Group, deps api.HandlerDeps){
		func(apiGroup *echo.Group, deps api.HandlerDeps) {
			apiKeyService := deps.ApiKey.Service()
			userService := deps.User.Service()
			if apiKeyService == nil || userService == nil || deps.Federated == nil || deps.GitRepository.Service() == nil || deps.GitOpsSync.Service() == nil {
				slog.WarnContext(ctx, "Playwright service not available, skipping playwright routes")
				return
			}

			playwrightService := playwright.NewPlaywrightService(apiKeyService, userService, deps.GitRepository.Service(), deps.GitOpsSync.Service(), deps.Project.Service())
			playwright.SetupRoutes(apiGroup, playwrightService, deps.Federated)
			slog.InfoContext(ctx, "Playwright routes registered for E2E testing")
		},
	}
}
