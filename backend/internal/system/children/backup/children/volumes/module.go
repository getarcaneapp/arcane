package volumes

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
)

func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-system-volume-backup-config",
			Method:      http.MethodGet,
			Path:        "/backups/volumes/config",
			Summary:     "Get global volume backup policies",
			Tags: []string{
				"System Backups",
			},
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
			Summary:     "Update global volume backup policies",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsUpdate,
		h.UpdateSystemVolumeConfig,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-system-volume-backup-options",
			Method:      http.MethodGet,
			Path:        "/backups/volumes/options",
			Summary:     "List volumes available to global volume backups",
			Tags: []string{
				"System Backups",
			},
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
			Summary:       "Run global volume backups",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsCreate,
		h.RunSystemVolumeBackups,
	)
}
