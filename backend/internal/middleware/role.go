package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
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
				return c.JSON(http.StatusForbidden, models.APIError{
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
		envID := ""
		if authz.IsEnvScoped(perm) {
			envID = authz.EnvIDFromPath(ctx.URL().Path)
		}
		if !ps.Allows(perm, envID) {
			if err := huma.WriteErr(api, ctx, http.StatusForbidden, "permission denied: "+perm); err != nil {
				slog.WarnContext(ctx.Context(), "failed to write 403 response", "error", err)
			}
			return
		}
		next(ctx)
	}}
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
