package restore

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// RegisterRoutes registers the volume backup restore endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "volume-backup-action",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/{backupId}",
		Summary:     "Restore volume backup",
		Description: "Restore a full volume backup, or selected files with action restorefiles",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"restore":      authz.PermVolumesBackup,
		"restorefiles": authz.PermVolumesBackup,
	}, h.RestoreAction)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "upload-volume-backup",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/upload",
		Summary:     "Upload and restore volume backup",
		Description: "Restore a volume from a complete chunked upload session containing a tar.gz backup archive. " +
			"multipart/form-data bodies are still accepted for backward compatibility; that form is deprecated " +
			"and will be removed in a future release.",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: upload.LegacyMultipartMiddleware(api, h.uploadService, types.UploadKindVolumeBackup),
	}, authz.PermVolumesUpload, h.UploadAndRestore)
}
