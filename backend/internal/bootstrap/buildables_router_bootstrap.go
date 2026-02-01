//go:build buildables

package bootstrap

import (
	"github.com/getarcaneapp/arcane/backend/internal/api"
	"github.com/gin-gonic/gin"
)

func init() {
	registerBuildableRoutes = append(registerBuildableRoutes, func(apiGroup *gin.RouterGroup, svc *Services) {
		api.SetupBuildablesRoutes(apiGroup, svc.Auth)
	})
}
