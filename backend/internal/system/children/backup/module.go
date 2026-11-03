package backup

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/system/children/backup/children/volumes"
	"getarcane.dev/arcane/v3/pkg/authz"
)

// RegisterRoutes registers the system backup endpoints, including the
// system-managed volume backup endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-system-backups",
			Method:      http.MethodGet,
			Path:        "/backups",
			Summary:     "List Arcane system backups",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsList,
		h.List,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-backup-history",
			Method:      http.MethodGet,
			Path:        "/backups/history",
			Summary:     "List unified backup history",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsList,
		h.ListHistory,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-system-backup-policies",
			Method:      http.MethodGet,
			Path:        "/backups/policies",
			Summary:     "Get Arcane system backup policies",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsRead,
		h.GetPolicies,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "update-system-backup-policies",
			Method:      http.MethodPut,
			Path:        "/backups/policies",
			Summary:     "Update Arcane system backup policies",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsUpdate,
		h.UpdatePolicies,
	)
	middleware.RegisterWithActionPermissions(
		api,
		middleware.RequireGlobalAdmin(api, huma.Operation{
			OperationID: "generate-system-backup-recovery-key",
			Method:      http.MethodPost,
			Path:        "/backups/recovery/key",
			Summary:     "Generate an Arcane system backup recovery key",
			Tags: []string{
				"System Backups",
			},
		}),
		"action",
		map[string]string{
			"generate": authz.PermSystemBackupsRecoveryKey,
		},
		h.GenerateRecoveryKey,
	)
	middleware.RegisterWithPermission(
		api,
		middleware.RequireGlobalAdmin(api, huma.Operation{
			OperationID: "set-system-backup-recovery-key",
			Method:      http.MethodPut,
			Path:        "/backups/recovery/key",
			Summary:     "Configure Arcane system backup recovery key",
			Tags: []string{
				"System Backups",
			},
		}),
		authz.PermSystemBackupsRecoveryKey,
		h.SetRecoveryKey,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID:   "create-system-backup",
			DefaultStatus: http.StatusAccepted,
			Method:        http.MethodPost,
			Path:          "/backups",
			Summary:       "Create Arcane system backup",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsCreate,
		h.Create,
	)
	middleware.RegisterWithPermission(
		api,
		middleware.RequireGlobalAdmin(api, huma.Operation{
			OperationID: "discover-system-backups",
			Method:      http.MethodPost,
			Path:        "/backups/discover",
			Summary:     "Discover Arcane system backups in S3",
			Tags: []string{
				"System Backups",
			},
		}),
		authz.PermSystemBackupsUpdate,
		h.Discover,
	)
	middleware.RegisterWithActionPermissions(
		api,
		middleware.RequireGlobalAdmin(api, huma.Operation{
			OperationID: "system-backup-action",
			Method:      http.MethodPost,
			Path:        "/backups/{id}",
			Summary:     "Restore Arcane system backup",
			Description: "Restore a full Arcane system backup, or selected project files with action restorefiles",
			Tags: []string{
				"System Backups",
			},
		}),
		"action",
		map[string]string{
			"restore":      authz.PermSystemBackupsRestore,
			"restorefiles": authz.PermSystemBackupsRestore,
		},
		h.RestoreAction,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "browse-system-backup-files",
			Method:      http.MethodPost,
			Path:        "/backups/{id}/files/browse",
			Summary:     "Browse project files in an Arcane system backup",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsRead,
		h.BrowseFiles,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "upload-system-backup",
			Method:      http.MethodPost,
			Path:        "/backups/{id}/upload",
			Summary:     "Upload Arcane system backup",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsUpload,
		h.Upload,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "delete-system-backup",
			Method:      http.MethodDelete,
			Path:        "/backups/{id}",
			Summary:     "Delete Arcane system backup",
			Tags: []string{
				"System Backups",
			},
		},
		authz.PermSystemBackupsDelete,
		h.Delete,
	)

	volumes.RegisterRoutes(api, volumes.NewHandler(kit.FromPtr(h.service).volumes, h.appCtx))
}
