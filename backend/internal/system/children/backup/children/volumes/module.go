package volumes

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

// RegisterRoutes registers the system-managed volume backup endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	adminOnly := middleware.RequireAdmin(api)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-system-volume-backup-config",
			Method:      http.MethodGet,
			Path:        "/backups/volumes/config",
			Summary:     "Get system-managed volume backup policies",
			Tags: []string{
				"System Backups",
			},
			Middlewares: adminOnly,
		},
		authz.PermSystemBackupsRead,
		h.GetSystemVolumeConfig,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "update-system-volume-backup-config",
			Method:      http.MethodPut,
			Path:        "/backups/volumes/config",
			Summary:     "Update system-managed volume backup policies",
			Tags: []string{
				"System Backups",
			},
			Middlewares: adminOnly,
		},
		authz.PermSystemBackupsManage,
		h.UpdateSystemVolumeConfig,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-system-volume-backup-options",
			Method:      http.MethodGet,
			Path:        "/backups/volumes/options",
			Summary:     "List volumes available to system-managed backups",
			Tags: []string{
				"System Backups",
			},
			Middlewares: adminOnly,
		},
		authz.PermSystemBackupsRead,
		h.ListSystemVolumeOptions,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID:   "run-system-volume-backups",
			DefaultStatus: http.StatusAccepted,
			Method:        http.MethodPost,
			Path:          "/backups/volumes/run",
			Summary:       "Run system-managed volume backups",
			Tags: []string{
				"System Backups",
			},
			Middlewares: adminOnly,
		},
		authz.PermSystemBackupsManage,
		h.RunSystemVolumeBackups,
	)
}
