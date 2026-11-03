//go:build playwright

package api

import (
	"log/slog"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/playwright"
)

func init() {
	featureRoutes = append(featureRoutes, registerPlaywright)
}

func registerPlaywright(api huma.API, p SetupParams) {
	if p.Config.Environment == "production" {
		return
	}
	playwright.RegisterRoutes(api, playwright.NewPlaywrightService(playwright.PlaywrightServiceParams{
		APIKeyService:     p.Deps.ApiKey.Service(),
		UserService:       p.Deps.User.Service(),
		RepositoryService: p.Deps.GitRepository.Service(),
		SyncService:       p.Deps.GitOpsSync.Service(),
		ProjectService:    p.Deps.Project.Service(),
	}), p.Deps.Federated)
	slog.InfoContext(p.AppContext.Context(), "Playwright routes registered for E2E testing")
}
