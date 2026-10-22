package browser

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// RegisterRoutes registers the volume backup file browsing endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "backup-has-path",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/files/exists",
		Summary:     "Check if backup contains path",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.BackupHasPath)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-backup-files",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/files",
		Summary:     "List files in a volume backup",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.ListBackupFiles)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "browse-volume-backup-files",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/files/browse",
		Summary:     "Browse files in a volume backup",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.BrowseBackupFiles)
}
