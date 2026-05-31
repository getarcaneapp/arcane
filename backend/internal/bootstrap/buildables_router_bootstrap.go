//go:build buildables

package bootstrap

import (
	"github.com/getarcaneapp/arcane/backend/api"
	"github.com/getarcaneapp/arcane/backend/internal/di"
	"github.com/labstack/echo/v4"
)

func init() {
	registerBuildableRoutes = append(registerBuildableRoutes, func(apiGroup *echo.Group, svc *di.Services) {
		api.SetupBuildablesRoutes(apiGroup, svc.Auth)
	})
}
