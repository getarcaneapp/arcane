package policies

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

// RegisterRoutes registers the volume backup policy endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-backup-policy",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/policy",
		Summary:     "Get volume backup policies",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.GetBackupPolicy)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-volume-backup-policy",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/policy",
		Summary:     "Update volume backup policies",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesBackup, h.UpdateBackupPolicy)
}
