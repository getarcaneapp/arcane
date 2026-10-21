package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
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

// RequireEchoPermission rejects Echo requests lacking perm for the environment
// in the request path, or globally for organization-level permissions. It must
// run after auth.AuthMiddleware has attached the caller's permission set.
func RequireEchoPermission(perm string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			ps, _ := c.Get(string(ContextKeyUserPermissions)).(*authz.PermissionSet)
			envID := ""
			if authz.IsEnvScoped(perm) {
				envID = authz.EnvIDFromPath(c.Request().URL.Path)
			}
			if !ps.Allows(perm, envID) {
				return c.JSON(http.StatusForbidden, common.APIError{
					Code:    "FORBIDDEN",
					Message: "permission denied: " + perm,
				})
			}
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
		ps, _ := PermissionsFromContext(ctx.Context())
		if !permissionAllowedInternal(ctx, ps, perm) {
			if err := huma.WriteErr(api, ctx, http.StatusForbidden, "permission denied: "+perm); err != nil {
				slog.WarnContext(ctx.Context(), "failed to write 403 response", "error", err)
			}
			return
		}
		next(ctx)
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
		ps, _ := PermissionsFromContext(ctx.Context())
		if !permissionAllowedInternal(ctx, ps, perm) {
			if err := huma.WriteErr(api, ctx, http.StatusForbidden, "permission denied: "+perm); err != nil {
				slog.WarnContext(ctx.Context(), "failed to write 403 response", "error", err)
			}
			return
		}
		next(ctx)
	}}
}

func permissionAllowedInternal(ctx huma.Context, ps *authz.PermissionSet, perm string) bool {
	if scope, scoped := httpx.EnvironmentScopeFromContext(ctx.Context()); scoped && authz.IsEnvScoped(perm) {
		return slices.ContainsFunc(scope.IDs, func(id string) bool {
			return ps.Allows(perm, id)
		})
	}
	return ps.Allows(perm, kit.Ternary(authz.IsEnvScoped(perm), authz.EnvIDFromPath(ctx.URL().Path), ""))
}

// RequireGlobalAdmin returns a per-operation Huma middleware that rejects any
// caller who is not a global admin (or sudo). Used for operations that are
// intentionally not exposed as delegated permissions — role creation/edits,
// user role assignment, and OIDC mapping management. Keeping these admin-only
// avoids the meta-escalation surface where a holder of `roles:assign` could
// promote themselves via a custom role.
func RequireGlobalAdmin(api huma.API) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		ps, _ := PermissionsFromContext(ctx.Context())
		if !ps.IsGlobalAdmin() {
			if err := huma.WriteErr(api, ctx, http.StatusForbidden, "permission denied: global admin required"); err != nil {
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
		ps, _ := PermissionsFromContext(ctx.Context())
		if ps == nil || !ps.Sudo {
			if err := huma.WriteErr(api, ctx, http.StatusForbidden, "permission denied: agent authentication required"); err != nil {
				slog.WarnContext(ctx.Context(), "failed to write 403 response", "error", err)
			}
			return
		}
		next(ctx)
	}}
}
