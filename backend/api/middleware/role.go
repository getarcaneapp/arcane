package middleware

import (
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// RequireAdmin returns a per-operation Huma middleware slice that returns 403
// to non-admin callers. Attach via Operation.Middlewares:
//
//	huma.Register(api, huma.Operation{..., Middlewares: middleware.RequireAdmin(api)}, h.Handler)
func RequireAdmin(api huma.API) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		if !IsAdminFromContext(ctx.Context()) {
			if err := huma.WriteErr(api, ctx, http.StatusForbidden, "admin access required"); err != nil {
				slog.WarnContext(ctx.Context(), "failed to write 403 response", "error", err)
			}
			return
		}
		next(ctx)
	}}
}
