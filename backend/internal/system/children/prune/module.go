package prune

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// RegisterRoutes registers the system prune endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "prune-all",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/prune",
		Summary:     "Prune Docker resources",
		Description: "Remove unused Docker resources (containers, images, volumes, networks)",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemPrune, h.PruneAll)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "df",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/df",
		Summary:     "Get reclaimable disk usage",
		Description: "Get the disk space held by unused images, build cache, unused volumes, and stopped containers",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemRead, h.GetDiskUsage)
}
