//go:build playwright

package bootstrap

import (
	"context"
	"log/slog"

	"github.com/labstack/echo/v5"

	"getarcane.dev/arcane/v3/api"
	"getarcane.dev/arcane/v3/internal/playwright"
)

func init() {
	ctx := context.Background() //nolint:forbidigo // Package init has no request context.
	registerPlaywrightRoutes = []func(apiGroup *echo.Group, deps api.HandlerDeps){
		func(apiGroup *echo.Group, deps api.HandlerDeps) {
			playwrightService := playwright.NewPlaywrightService(playwright.PlaywrightServiceParams{
				APIKeyService:     deps.ApiKey.Service(),
				UserService:       deps.User.Service(),
				RepositoryService: deps.GitRepository.Service(),
				SyncService:       deps.GitOpsSync.Service(),
				ProjectService:    deps.Project.Service(),
			})
			playwright.SetupRoutes(apiGroup, playwrightService, deps.Federated)
			slog.InfoContext(ctx, "Playwright routes registered for E2E testing")
		},
	}
}
