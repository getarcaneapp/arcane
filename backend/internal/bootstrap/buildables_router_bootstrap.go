//go:build buildables

package bootstrap

import (
	"github.com/labstack/echo/v5"

	"github.com/getarcaneapp/arcane/backend/v3/api"
)

func init() {
	registerBuildableRoutes = append(registerBuildableRoutes, func(apiGroup *echo.Group, deps api.HandlerDeps) {
		api.SetupBuildablesRoutes(apiGroup, deps.Auth.Service())
	})
}
