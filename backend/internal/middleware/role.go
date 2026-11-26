package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/httpx"
)

type EchoRoute struct {
	authz.PermissionRoute

	Handler     echo.HandlerFunc
	Middlewares []echo.MiddlewareFunc
}

func RegisterEchoRoutes(g *echo.Group, routes []EchoRoute, shared ...echo.MiddlewareFunc) {
	for _, route := range routes {
		middlewares := slices.Concat(shared, route.Middlewares)
		if route.Permission != "" {
			middlewares = append(middlewares, RequireEchoPermission(route.Permission))
		}
		g.Add(route.Method, route.Path, route.Handler, middlewares...)
	}
}

func setOpMetadata(op *huma.Operation, key string, value any) {
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata[key] = value
}

func writeMiddlewareErr(api huma.API, ctx huma.Context, status int, message string) {
	if err := huma.WriteErr(api, ctx, status, message); err != nil {
		slog.WarnContext(ctx.Context(), "failed to write error response", "status", status, "error", err)
	}
}

func RegisterWithPermission[I, O any](api huma.API, op huma.Operation, perm string, handler func(context.Context, *I) (*O, error)) {
	setOpMetadata(&op, authz.MetaRequiredPermission, perm)
	op.Middlewares = append(op.Middlewares, RequirePermission(api, perm)...)
	huma.Register(api, op, handler)
}

func RegisterWithActionPermissions[I, O any](api huma.API, op huma.Operation, param string, perms map[string]string, handler func(context.Context, *I) (*O, error)) {
	setOpMetadata(&op, authz.MetaActionParam, param)
	setOpMetadata(&op, authz.MetaActionPermissions, perms)
	op.Middlewares = append(op.Middlewares, RequireActionPermission(api, param, perms)...)
	huma.Register(api, op, handler)
}

func RequireEchoPermission(perm string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			ps, _ := c.Get(string(ContextKeyUserPermissions)).(*authz.PermissionSet)
			envID := ""
			if authz.IsEnvScoped(perm) {
				envID = authz.EnvIDFromPath(c.Request().URL.Path)
			}
			if projectID := c.Param("projectId"); projectID != "" && strings.HasPrefix(perm, "projects:") {
				if !ps.AllowsResource(perm, envID, authz.ResourceTypeProject, projectID) {
					return c.JSON(http.StatusForbidden, common.APIError{
						Code:    "FORBIDDEN",
						Message: "permission denied: " + perm,
					})
				}
				return next(c)
			}
			if !ps.AllowsRoute(perm, envID) {
				return c.JSON(http.StatusForbidden, common.APIError{
					Code:    "FORBIDDEN",
					Message: "permission denied: " + perm,
				})
			}
			return next(c)
		}
	}
}

func RequirePermission(api huma.API, perm string) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		ps, _ := PermissionsFromContext(ctx.Context())
		if !permissionAllowed(ctx, ps, perm) {
			writeMiddlewareErr(api, ctx, http.StatusForbidden, "permission denied: "+perm)
			return
		}
		next(ctx)
	}}
}

func RequireActionPermission(api huma.API, param string, perms map[string]string) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		perm, ok := perms[ctx.Query(param)]
		if !ok {
			writeMiddlewareErr(api, ctx, http.StatusBadRequest, "invalid "+param+": "+ctx.Query(param))
			return
		}
		ps, _ := PermissionsFromContext(ctx.Context())
		if !permissionAllowed(ctx, ps, perm) {
			writeMiddlewareErr(api, ctx, http.StatusForbidden, "permission denied: "+perm)
			return
		}
		next(ctx)
	}}
}

func RequirePermissions(ctx context.Context, environmentID string, perms []string) error {
	ps, _ := PermissionsFromContext(ctx)
	for _, perm := range perms {
		if !ps.Allows(perm, environmentID) {
			return huma.Error403Forbidden("permission denied: " + perm)
		}
	}
	return nil
}

func RequireResourcePermissions(ctx context.Context, environmentID, resourceType, resourceID string, perms []string) error {
	ps, _ := PermissionsFromContext(ctx)
	for _, perm := range perms {
		if !ps.AllowsResource(perm, environmentID, resourceType, resourceID) {
			return huma.Error403Forbidden("permission denied: " + perm)
		}
	}
	return nil
}

func permissionAllowed(ctx huma.Context, ps *authz.PermissionSet, perm string) bool {
	if scope, scoped := httpx.EnvironmentScopeFromContext(ctx.Context()); scoped && authz.IsEnvScoped(perm) {
		denied := slices.DeleteFunc(slices.Clone(scope.IDs), func(id string) bool {
			return ps.AllowsRoute(perm, id)
		})
		return kit.Ternary(ctx.Method() == http.MethodGet, len(denied) < len(scope.IDs), len(denied) == 0)
	}
	envID := kit.Ternary(authz.IsEnvScoped(perm), authz.EnvIDFromPath(ctx.URL().Path), "")
	if projectID := ctx.Param("projectId"); projectID != "" && strings.HasPrefix(perm, "projects:") {
		return ps.AllowsResource(perm, envID, authz.ResourceTypeProject, projectID)
	}
	return ps.AllowsRoute(perm, envID)
}

func RequireGlobalAdmin(api huma.API, op huma.Operation) huma.Operation {
	setOpMetadata(&op, authz.MetaRequireGlobalAdmin, true)
	op.Middlewares = append(op.Middlewares, func(ctx huma.Context, next func(huma.Context)) {
		ps, _ := PermissionsFromContext(ctx.Context())
		if !ps.IsGlobalAdmin() {
			writeMiddlewareErr(api, ctx, http.StatusForbidden, "permission denied: global admin required")
			return
		}
		next(ctx)
	})
	return op
}

func RequireSudo(api huma.API, op huma.Operation) huma.Operation {
	setOpMetadata(&op, authz.MetaRequireSudo, true)
	op.Middlewares = append(op.Middlewares, func(ctx huma.Context, next func(huma.Context)) {
		ps, _ := PermissionsFromContext(ctx.Context())
		if ps == nil || !ps.Sudo {
			writeMiddlewareErr(api, ctx, http.StatusForbidden, "permission denied: agent authentication required")
			return
		}
		next(ctx)
	})
	return op
}
