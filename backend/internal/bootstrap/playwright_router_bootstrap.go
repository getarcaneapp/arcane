//go:build playwright

package bootstrap

import (
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/api"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
)

func init() {
	registerPlaywrightRoutes = []func(apiGroup *gin.RouterGroup, services *Services){
		func(apiGroup *gin.RouterGroup, svc *Services) {
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
