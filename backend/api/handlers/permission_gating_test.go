package handlers

import (
	"context"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/labstack/echo/v4"
)

func newPermissionGatingRouterInternal(t *testing.T, ps *authz.PermissionSet) (*echo.Echo, huma.API) {
	t.Helper()
	if ps == nil {
		ps = authz.NewPermissionSet()
	}

	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), humamw.ContextKeyUserPermissions, ps)))
	})
	return router, api
}
