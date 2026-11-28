package stats

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-backup-storage-stats",
			Method:      http.MethodGet,
			Path:        "/agents/{id}/backups/stats",
			Summary:     "Get backup storage stats",
			Description: "Get the saved size and snapshot count of each backup repository",
			Tags: []string{
				"System Backups",
			},
			Security: handlerutil.DefaultOperationSecurity(),
			Metadata: map[string]any{authz.MetaEnvironmentAggregate: true},
		},
		authz.PermSystemBackupsRead,
		h.Get,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "refresh-backup-storage-stats",
			Method:      http.MethodPost,
			Path:        "/agents/{id}/backups/stats/refresh",
			Summary:     "Refresh backup storage stats",
			Description: "Read every backup repository again and save its size and snapshot count; repositories with a backup in progress are skipped",
			Tags: []string{
				"System Backups",
			},
			Security: handlerutil.DefaultOperationSecurity(),
			Metadata: map[string]any{authz.MetaEnvironmentAggregate: true},
		},
		authz.PermSystemBackupsRead,
		h.Refresh,
	)
}
