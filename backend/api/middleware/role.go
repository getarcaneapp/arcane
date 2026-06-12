package middleware

import (
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
)

// RequirePermission returns a per-operation Huma middleware that rejects
// callers lacking `perm`. For env-scoped permissions, the env ID is extracted
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
