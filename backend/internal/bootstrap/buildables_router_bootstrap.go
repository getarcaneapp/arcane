//go:build buildables

package bootstrap

import (
	"github.com/getarcaneapp/arcane/backend/api"
	"github.com/labstack/echo/v4"
)

func init() {
	registerBuildableRoutes = append(registerBuildableRoutes, func(apiGroup *echo.Group, svc *Services) {
		api.SetupBuildablesRoutes(apiGroup, svc.Auth)
	})
}
