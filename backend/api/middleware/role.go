package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
)

// MetaRequiredPermission is re-exported from the authz package for callers that
// reference it via this middleware package. The authoritative definition (and
// the matcher that consumes it) lives in authz.
const MetaRequiredPermission = authz.MetaRequiredPermission

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

// RequireAnyEnvironmentPermission protects aggregate operations that span
// environments but do not carry an environment ID in their path. The caller
// must hold perm globally or for at least one environment; handlers remain
// responsible for filtering aggregate output to the exact allowed scopes.
func RequireAnyEnvironmentPermission(api huma.API, perm string) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		ps, _ := PermissionsFromContext(ctx.Context())
		if !ps.AllowsAny(perm) {
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
