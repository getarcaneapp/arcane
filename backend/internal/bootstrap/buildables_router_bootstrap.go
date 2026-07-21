//go:build buildables

package bootstrap

import (
	"github.com/getarcaneapp/arcane/backend/v2/api"
	"github.com/labstack/echo/v4"
)

func init() {
	registerBuildableRoutes = append(registerBuildableRoutes, func(apiGroup *echo.Group, deps api.HandlerDeps) {
		api.SetupBuildablesRoutes(apiGroup, deps.Auth)
	})
}
