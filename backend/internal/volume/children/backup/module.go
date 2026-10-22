package backup

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume/children/backup/children/browser"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume/children/backup/children/policies"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume/children/backup/children/restore"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// RegisterRoutes registers the volume backup endpoints, including policies,
// file browsing, and restores.
func RegisterRoutes(api huma.API, h *Handler) {
	var policiesService *policies.Service
	var browserService *browser.Service
	var restoreService *restore.Service
	if h.service != nil {
		policiesService, browserService, restoreService = h.service.policies, h.service.browser, h.service.restore
	}
	policies.RegisterRoutes(api, policies.NewHandler(policiesService, h.activityService, h.environmentService, h.appCtx))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-volume-backups",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/backups",
		Summary:     "List volume backups",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.ListBackups)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "create-volume-backup",
		DefaultStatus: http.StatusAccepted,
		Method:        http.MethodPost,
		Path:          "/environments/{id}/volumes/{volumeName}/backups",
		Summary:       "Create volume backup",
		Tags:          []string{"Volume Backup"},
		Security:      handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesBackup, h.CreateBackup)

	restore.RegisterRoutes(api, restore.NewHandler(restoreService, h.activityService, h.uploadService, h.appCtx))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-volume-backup",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/volumes/backups/{backupId}",
		Summary:     "Delete volume backup",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesBackup, h.DeleteBackup)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "discover-volume-backups",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/backups/discover",
		Summary:     "Discover volume backups on an S3 destination",
		Description: "Import existing volume backups stored on the destination by this or other Arcane instances",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesBackup, h.DiscoverBackups)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "upload-retained-volume-backup-to-s3",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/backups/{backupId}/upload",
		Summary:     "Upload volume backup",
		Description: "Upload an existing local volume backup to the selected S3 destination",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesBackup, h.UploadBackup)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "download-volume-backup",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/download",
		Summary:     "Download volume backup",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.DownloadBackup)

	browser.RegisterRoutes(api, browser.NewHandler(browserService))
}
