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
	if p.Config.Environment == "production" || p.Config.AgentEnabled {
		return
	}
	playwright.RegisterRoutes(api, playwright.NewPlaywrightService(playwright.PlaywrightServiceParams{
		APIKeyService:     p.ManagerDeps.ApiKey.Service(),
		UserService:       p.ManagerDeps.User.Service(),
		RepositoryService: p.Deps.GitRepository.Service(),
		SyncService:       p.Deps.GitOpsSync.Service(),
		ProjectService:    p.Deps.Project.Service(),
	}), p.ManagerDeps.Federated)
	slog.InfoContext(p.AppContext.Context(), "Playwright routes registered for E2E testing")
}
