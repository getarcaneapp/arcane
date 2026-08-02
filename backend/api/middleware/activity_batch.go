package middleware

import (
	"strings"

	"github.com/danielgtaylor/huma/v2"

	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// NewActivityBatchID lifts the client-supplied activity batch ID header into
// the request context so activities spawned by one logical bulk action can be
// grouped without threading the ID through every handler. Invalid values are
// ignored by utils.WithActivityBatchID.
func NewActivityBatchID() func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if batchID := strings.TrimSpace(ctx.Header(pkgutils.HeaderActivityBatchID)); batchID != "" {
			ctx = huma.WithContext(ctx, pkgutils.WithActivityBatchID(ctx.Context(), batchID))
		}
		next(ctx)
	}
}
