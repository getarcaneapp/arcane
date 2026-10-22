package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

// RegisterWithPermission registers a Huma operation that requires perm. It
// attaches the RequirePermission middleware AND records perm in the operation
// metadata (authz.MetaRequiredPermission) so the remote environment proxy can
// enforce the same permission for environment-scoped operations before
// forwarding a request to an agent.
//
// Use this instead of huma.Register with an inline RequirePermission middleware
// for every operation served under /environments/{id}/..., so the required
// permission stays the single source of truth for both local enforcement and
// remote-proxy enforcement. It is safe to use for org-level operations too; the
// recorded metadata is simply unused by the proxy for non-environment paths.
func RegisterWithPermission[I, O any](api huma.API, op huma.Operation, perm string, handler func(context.Context, *I) (*O, error)) {
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata[authz.MetaRequiredPermission] = perm
	op.Middlewares = append(op.Middlewares, RequirePermission(api, perm)...)
	huma.Register(api, op, handler)
}

// RegisterWithActionPermissions registers a Huma operation whose required
// permission depends on the value of the param query parameter. perms maps each
// accepted value to its permission. The same map is enforced locally by
// RequireActionPermission and recorded in the operation's OpenAPI metadata
// (authz.MetaActionParam, authz.MetaActionPermissions), so the remote
// environment proxy looks up the identical permission per action before
// forwarding to an agent.
func RegisterWithActionPermissions[I, O any](api huma.API, op huma.Operation, param string, perms map[string]string, handler func(context.Context, *I) (*O, error)) {
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata[authz.MetaActionParam] = param
	op.Metadata[authz.MetaActionPermissions] = perms
	op.Middlewares = append(op.Middlewares, RequireActionPermission(api, param, perms)...)
	huma.Register(api, op, handler)
}

func RegisterAuthenticated[I, O any](api huma.API, op huma.Operation, handler func(context.Context, *I) (*O, error)) {
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata[authz.MetaAuthenticatedOnly] = true
	huma.Register(api, op, handler)
}

// RequireEchoPermission rejects Echo requests lacking perm for the environment
// in the request path, or globally for organization-level permissions. It must
// run after auth.AuthMiddleware has attached the caller's permission set.
func RequireEchoPermission(perm string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			ps, ok := c.Get(string(ContextKeyUserPermissions)).(*authz.PermissionSet)
			if !ok && c.Get(string(ContextKeyUserID)) != nil {
				return c.JSON(http.StatusInternalServerError, common.APIError{
					Code:    "INTERNAL_ERROR",
					Message: "permission set unavailable for authenticated request",
				})
			}
			envID := kit.Ternary(authz.IsEnvScoped(perm), authz.EnvIDFromPath(c.Request().URL.Path), "")
			if !ps.Allows(perm, envID) {
				recordEchoDenial(c, perm, envID)
				return c.JSON(http.StatusForbidden, common.APIError{
					Code:    "FORBIDDEN",
					Message: "permission denied: " + perm,
				})
			}
			c.SetRequest(c.Request().WithContext(authz.WithRequestEnvironment(c.Request().Context(), envID)))
			return next(c)
		}
	}
}

// RequirePermission returns per-operation Huma middleware that rejects callers
// lacking `perm`. For env-scoped permissions, the env ID is extracted
// from the request path (/environments/{id}/...). For org-level permissions,
// the env ID segment, if any, is ignored.
//
// Attach via Operation.Middlewares:
//
//	huma.Register(api, huma.Operation{..., Middlewares: middleware.RequirePermission(api, authz.PermContainersStart)}, h.Handler)
func RequirePermission(api huma.API, perm string) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		authorizePermission(api, ctx, next, perm)
	}}
}

// RequireActionPermission returns middleware that selects the required
// permission from perms using the value of the param query parameter. A missing
// or unknown value is rejected with 400, and a caller lacking the selected
// permission is rejected with 403.
func RequireActionPermission(api huma.API, param string, perms map[string]string) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		perm, ok := perms[ctx.Query(param)]
		if !ok {
			if err := huma.WriteErr(api, ctx, http.StatusBadRequest, "invalid "+param+": "+ctx.Query(param)); err != nil {
				slog.WarnContext(ctx.Context(), "failed to write 400 response", "error", err)
			}
			return
		}
		authorizePermission(api, ctx, next, perm)
	}}
}

func authorizePermission(api huma.API, ctx huma.Context, next func(huma.Context), perm string) {
	ps, ok := authz.PermissionsFromContext(ctx.Context())
	if _, authenticated := GetUserIDFromContext(ctx.Context()); authenticated && !ok {
		if err := huma.WriteErr(api, ctx, http.StatusInternalServerError, "permission set unavailable for authenticated request"); err != nil {
			slog.WarnContext(ctx.Context(), "failed to write 500 response", "error", err)
		}
		return
	}
	envID := kit.Ternary(authz.IsEnvScoped(perm), authz.EnvIDFromPath(ctx.URL().Path), "")
	if !permissionAllowedInternal(ctx, ps, perm) {
		recordHumaDenial(ctx, perm, envID)
		if err := huma.WriteErr(api, ctx, http.StatusForbidden, "permission denied: "+perm); err != nil {
			slog.WarnContext(ctx.Context(), "failed to write 403 response", "error", err)
		}
		return
	}
	next(huma.WithContext(ctx, authz.WithRequestEnvironment(ctx.Context(), envID)))
}

func permissionAllowedInternal(ctx huma.Context, ps *authz.PermissionSet, perm string) bool {
	if scope, scoped := httpx.EnvironmentScopeFromContext(ctx.Context()); scoped && authz.IsEnvScoped(perm) {
		return slices.ContainsFunc(scope.IDs, func(id string) bool {
			return ps.Allows(perm, id)
		})
	}
	return ps.Allows(perm, kit.Ternary(authz.IsEnvScoped(perm), authz.EnvIDFromPath(ctx.URL().Path), ""))
}

func RequireAdmin(api huma.API) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		ps, _ := authz.PermissionsFromContext(ctx.Context())
		if !ps.IsGlobalAdmin() {
			recordHumaDenial(ctx, authz.PermAll, "")
			if err := huma.WriteErr(api, ctx, http.StatusForbidden, "permission denied: admin required"); err != nil {
				slog.WarnContext(ctx.Context(), "failed to write 403 response", "error", err)
			}
			return
		}
		next(ctx)
	}}
}

// RequireSudo restricts infrastructure-only operations to callers authenticated
// through the agent-token path. Holding every user-facing permission is not
// sufficient because these operations may expose materialized secret values.
func RequireSudo(api huma.API) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		ps, _ := authz.PermissionsFromContext(ctx.Context())
		if ps == nil || !ps.Sudo {
			if err := huma.WriteErr(api, ctx, http.StatusForbidden, "permission denied: agent authentication required"); err != nil {
				slog.WarnContext(ctx.Context(), "failed to write 403 response", "error", err)
			}
			return
		}
		next(ctx)
	}}
}

func recordHumaDenial(ctx huma.Context, perm, envID string) {
	RecordDenial(ctx.Context(), types.AuthzDenial{
		Permission:    perm,
		EnvironmentID: envID,
		Method:        ctx.Method(),
		Path:          ctx.URL().Path,
		RemoteAddr:    ctx.RemoteAddr(),
		UserAgent:     ctx.Header("User-Agent"),
	})
}

func recordEchoDenial(c *echo.Context, perm, envID string) {
	req := c.Request()
	ctx := req.Context()
	if actor := c.Get(string(ContextKeyCurrentUser)); actor != nil {
		ctx = context.WithValue(ctx, types.CurrentUserContextKey{}, actor)
	}
	RecordDenial(ctx, types.AuthzDenial{
		Permission:    perm,
		EnvironmentID: envID,
		Method:        req.Method,
		Path:          req.URL.Path,
		RemoteAddr:    c.RealIP(),
		UserAgent:     req.UserAgent(),
	})
}
