package volume

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	volumeops "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/base"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
)

// VolumeHandler provides Huma-based volume management endpoints.
type VolumeHandler struct {
	volumeService      *VolumeService
	dockerService      *docker.DockerClientService
	activityService    *activity.ActivityService
	environmentService *environment.EnvironmentService
	uploadService      *upload.UploadService
	appCtx             context.Context
}

// --- Huma Input/Output Wrappers ---

// VolumeUsageCountsData represents the counts of volumes by usage status.
// This is a local type to avoid schema naming conflicts with image.UsageCounts.
type VolumeUsageCountsData struct {
	Inuse  int `json:"inuse"`
	Unused int `json:"unused"`
	Total  int `json:"total"`
}

type ListVolumesInput struct {
	EnvironmentID   string `path:"id" doc:"Environment ID"`
	Search          string `query:"search" doc:"Search query"`
	Sort            string `query:"sort" doc:"Column to sort by"`
	Order           string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start           int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit           int    `query:"limit" default:"20" doc:"Number of items per page"`
	InUse           string `query:"inUse" doc:"Filter by in-use status (true/false)"`
	IncludeInternal bool   `query:"includeInternal" default:"false" doc:"Include internal volumes"`
}

type ListVolumesOutput struct {
	Body base.PaginatedWithCounts[volumetypes.Volume, VolumeUsageCountsData]
}

type GetVolumeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type CreateVolumeInput struct {
	EnvironmentID string             `path:"id" doc:"Environment ID"`
	Body          volumetypes.Create `doc:"Volume creation data"`
}

type RenameVolumeInput struct {
	EnvironmentID string             `path:"id" doc:"Environment ID"`
	VolumeName    string             `path:"volumeName" doc:"Current volume name"`
	Body          volumetypes.Rename `doc:"Volume rename data"`
}

type RemoveVolumeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Force         bool   `query:"force" doc:"Force removal"`
}

type PruneVolumesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// VolumePruneReportData represents the result of a volume prune operation.
// This is a local type to avoid schema naming conflicts with image.PruneReport.
type VolumePruneReportData struct {
	VolumesDeleted []string `json:"volumesDeleted,omitempty"`
	SpaceReclaimed uint64   `json:"spaceReclaimed"`
	ActivityID     *string  `json:"activityId,omitempty"`
}

type GetVolumeUsageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

// VolumeUsageResponse represents volume usage information.
type VolumeUsageResponse struct {
	InUse      bool     `json:"inUse"`
	Containers []string `json:"containers"`
}

type GetVolumeUsageCountsInput struct {
	EnvironmentID   string `path:"id" doc:"Environment ID"`
	IncludeInternal bool   `query:"includeInternal" default:"false" doc:"Include internal volumes"`
}

type GetVolumeSizesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// VolumeSizeInfo represents size information for a single volume.
type VolumeSizeInfo struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	RefCount int64  `json:"refCount"`
}

// --- Volume Backup ---

type ListBackupsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Limit"`
	Type          string `query:"type" doc:"Management origin filter"`
}

type VolumeBackupPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []VolumeBackup          `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
	Warnings   []string                `json:"warnings,omitempty"`
}

type ListBackupsOutput struct {
	Body VolumeBackupPaginatedResponse
}

type CreateBackupInput struct {
	EnvironmentID string                           `path:"id" doc:"Environment ID"`
	VolumeName    string                           `path:"volumeName" doc:"Volume name"`
	Body          *volumetypes.CreateBackupRequest `json:"body,omitempty"`
}

type GetVolumeBackupPolicyInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type UpdateVolumeBackupPolicyInput struct {
	EnvironmentID string                           `path:"id" doc:"Environment ID"`
	VolumeName    string                           `path:"volumeName" doc:"Volume name"`
	Body          volumetypes.UpdateBackupPolicies `doc:"Scheduled volume backup policies"`
}

type RestoreBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type RestoreBackupFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Body          backuptypes.RestoreSelection
}

type BackupHasPathInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Path          string `query:"path" doc:"Path to check"`
}

type BackupHasPathResponse struct {
	Exists bool `json:"exists"`
}

type ListBackupFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type BrowseBackupFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Path          string `query:"path" doc:"Folder path relative to the backup root"`
	Search        string `query:"search" doc:"Case-insensitive full-path search"`
	Start         int    `query:"start" default:"0" doc:"Start index for the page"`
	Limit         int    `query:"limit" default:"20" doc:"Requested page size"`
}
type DeleteBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type DownloadBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type UploadAndRestoreInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Body          uploadtypes.ConsumeRequest
}

type UploadBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Body          volumetypes.UploadBackupRequest
}

// RegisterVolumes registers volume management routes using Huma.
func RegisterVolumes(api huma.API, dockerService *docker.DockerClientService, volumeService *VolumeService, activityService *activity.ActivityService, environmentService *environment.EnvironmentService, uploadService *upload.UploadService, appCtx handlerutil.ActivityAppContext) {
	h := &VolumeHandler{
		volumeService:      volumeService,
		dockerService:      dockerService,
		activityService:    activityService,
		environmentService: environmentService,
		uploadService:      uploadService,
		appCtx:             appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-usage-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/counts",
		Summary:     "Get volume usage counts",
		Description: "Get counts of volumes in use, unused, and total",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesList, h.GetVolumeUsageCounts)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-volumes",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes",
		Summary:     "List volumes",
		Description: "Get a paginated list of Docker volumes",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesList, h.ListVolumes)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}",
		Summary:     "Get volume by name",
		Description: "Get a Docker volume by its name",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.GetVolume)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-volume",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes",
		Summary:     "Create a volume",
		Description: "Create a new Docker volume",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesCreate, h.CreateVolume)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "remove-volume",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/volumes/{volumeName}",
		Summary:     "Remove a volume",
		Description: "Remove a Docker volume by name",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesDelete, h.RemoveVolume)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "rename-volume",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/rename",
		Summary:     "Rename a volume",
		Description: "Copy an unused Docker volume to a new name and remove the source",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRename, h.RenameVolume)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "prune-volumes",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/prune",
		Summary:     "Prune unused volumes",
		Description: "Remove all unused Docker volumes",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesPrune, h.PruneVolumes)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-usage",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/usage",
		Summary:     "Get volume usage",
		Description: "Get containers using a specific volume",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.GetVolumeUsage)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-sizes",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/sizes",
		Summary:     "Get volume sizes",
		Description: "Get disk usage sizes for all volumes (slow operation)",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesList, h.GetVolumeSizes)

	// --- Volume Browsing Endpoints ---

	registerVolumeWorkspaceRoutesInternal(api, h)

	// --- Volume Backup Endpoints ---

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-backup-policy",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/backup-policy",
		Summary:     "Get volume backup policies",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.GetBackupPolicy)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-volume-backup-policy",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/volumes/{volumeName}/backup-policy",
		Summary:     "Update volume backup policies",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesBackup, h.UpdateBackupPolicy)

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

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "restore-volume-backup",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/{backupId}/restore",
		Summary:     "Restore volume backup",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesBackup, h.RestoreBackup)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "restore-volume-backup-files",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/{backupId}/restore-files",
		Summary:     "Restore specific files from a volume backup",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesBackup, h.RestoreBackupFiles)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-volume-backup",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/volumes/backups/{backupId}",
		Summary:     "Delete volume backup",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesBackup, h.DeleteBackup)

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

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "backup-has-path",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/has-path",
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

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "upload-volume-backup",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/upload",
		Summary:     "Upload and restore volume backup",
		Description: "Restore a volume from a complete chunked upload session containing a tar.gz backup archive. multipart/form-data bodies are still accepted for backward compatibility; that form is deprecated and will be removed in a future release.",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: upload.LegacyMultipartMiddleware(api, h.uploadService, uploadtypes.KindVolumeBackup),
	}, authz.PermVolumesUpload, h.UploadAndRestore)
}

func (h *VolumeHandler) DownloadBackup(ctx context.Context, input *DownloadBackupInput) (*huma.StreamResponse, error) {
	user, _ := common.CurrentUserFromContext(ctx)
	reader, size, err := h.volumeService.DownloadBackup(ctx, input.BackupID, user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			defer func() { _ = reader.Close() }()

			humaCtx.SetHeader("Content-Type", "application/x-gzip")
			humaCtx.SetHeader("Content-Disposition", "attachment; filename="+input.BackupID+".tar.gz")
			humaCtx.SetHeader("Content-Length", strconv.FormatInt(size, 10))

			writer := humaCtx.BodyWriter()
			_, _ = io.Copy(writer, reader)
		},
	}, nil
}

func (h *VolumeHandler) GetBackupPolicy(ctx context.Context, input *GetVolumeBackupPolicyInput) (*handlerutil.Out[volumetypes.BackupPolicyCollection], error) {
	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		remotePath := fmt.Sprintf("/api/environments/0/volumes/%s/backup-policy", url.PathEscape(input.VolumeName))
		response, err := handlerutil.RemoteJSONProxy(h.environmentService.ProxyJSONRequest).JSON[base.ApiResponse[volumetypes.BackupPolicyCollection]](ctx, input.EnvironmentID, http.MethodGet, remotePath, nil)
		if err != nil {
			return nil, err
		}
		return &handlerutil.Out[volumetypes.BackupPolicyCollection]{Body: *response}, nil
	}

	policies, err := h.volumeService.GetBackupPolicies(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[volumetypes.BackupPolicyCollection]{Body: base.ApiResponse[volumetypes.BackupPolicyCollection]{Success: true, Data: *policies}}, nil
}

func (h *VolumeHandler) UpdateBackupPolicy(ctx context.Context, input *UpdateVolumeBackupPolicyInput) (*handlerutil.Out[volumetypes.BackupPolicyCollection], error) {
	user, _ := common.CurrentUserFromContext(ctx)
	var policies *volumetypes.BackupPolicyCollection
	var remoteResponse *base.ApiResponse[volumetypes.BackupPolicyCollection]
	errorStatus := http.StatusBadRequest
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	_, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume_backup_policy",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Saving backup policies",
		Message:        "Saving volume backup policies",
		SuccessMessage: "Volume backup policies saved successfully",
		Metadata: database.JSON{
			"action":      "update_volume_backup_policy",
			"policyCount": len(input.Body.Policies),
		},
	}, func(activityCtx context.Context) error {
		if input.EnvironmentID != "0" {
			if h.environmentService == nil {
				errorStatus = http.StatusInternalServerError
				return errors.New("environment service not available")
			}
			hasS3 := false
			for _, policy := range input.Body.Policies {
				if policy.S3Enabled {
					hasS3 = true
					break
				}
			}
			// Destinations are reconciled before every policy write so the agent
			// can validate S3 references. Only batches that actually need S3
			// fail on a sync error; local-only edits proceed.
			if syncErr := h.environmentService.SyncS3DestinationsToEnvironment(activityCtx, input.EnvironmentID); syncErr != nil {
				if hasS3 {
					errorStatus = http.StatusBadGateway
					return fmt.Errorf("failed to synchronize S3 destinations to environment: %w", syncErr)
				}
				slog.WarnContext(activityCtx, "S3 destination sync failed before local-only volume backup policy write", "environment_id", input.EnvironmentID, "error", syncErr)
			}
			remotePath := fmt.Sprintf("/api/environments/0/volumes/%s/backup-policy", url.PathEscape(input.VolumeName))
			var proxyErr error
			remoteResponse, proxyErr = handlerutil.RemoteJSONProxy(h.environmentService.ProxyJSONRequest).JSON[base.ApiResponse[volumetypes.BackupPolicyCollection]](activityCtx, input.EnvironmentID, http.MethodPut, remotePath, input.Body)
			if proxyErr != nil {
				errorStatus = http.StatusBadGateway
			}
			return proxyErr
		}
		var updateErr error
		policies, updateErr = h.volumeService.UpdateBackupPolicies(activityCtx, input.VolumeName, input.Body.Policies)
		return updateErr
	})
	if err != nil {
		if errorStatus == http.StatusInternalServerError {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if errorStatus == http.StatusBadGateway {
			return nil, huma.NewError(http.StatusBadGateway, err.Error())
		}
		return nil, huma.Error400BadRequest(err.Error())
	}
	if remoteResponse != nil {
		return &handlerutil.Out[volumetypes.BackupPolicyCollection]{Body: *remoteResponse}, nil
	}
	return &handlerutil.Out[volumetypes.BackupPolicyCollection]{Body: base.ApiResponse[volumetypes.BackupPolicyCollection]{Success: true, Data: *policies}}, nil
}

// ListVolumes returns a paginated list of volumes.
func (h *VolumeHandler) ListVolumes(ctx context.Context, input *ListVolumesInput) (*ListVolumesOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.InUse != "" {
		params.Filters["inUse"] = input.InUse
	}

	if params.Limit == 0 {
		params.Limit = 20
	}

	volumes, paginationResp, counts, err := h.volumeService.ListVolumesPaginated(ctx, params, input.IncludeInternal)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list volumes").Error())
	}

	if volumes == nil {
		volumes = []volumetypes.Volume{}
	}

	return &ListVolumesOutput{
		Body: base.PaginatedWithCounts[volumetypes.Volume, VolumeUsageCountsData]{
			Success: true,
			Data:    volumes,
			Counts: VolumeUsageCountsData{
				Inuse:  counts.Inuse,
				Unused: counts.Unused,
				Total:  counts.Total,
			},
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetVolume returns a volume by name.
func (h *VolumeHandler) GetVolume(ctx context.Context, input *GetVolumeInput) (*handlerutil.Out[*volumetypes.Volume], error) {
	vol, err := h.volumeService.GetVolumeByName(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.Error404NotFound(errors.WithMessage(err, "Volume not found").Error())
	}

	return &handlerutil.Out[*volumetypes.Volume]{
		Body: base.ApiResponse[*volumetypes.Volume]{
			Success: true,
			Data:    vol,
		},
	}, nil
}

// CreateVolume creates a new Docker volume.
func (h *VolumeHandler) CreateVolume(ctx context.Context, input *CreateVolumeInput) (*handlerutil.Out[*volumetypes.Volume], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	options := client.VolumeCreateOptions{
		Name:       input.Body.Name,
		Driver:     input.Body.Driver,
		Labels:     input.Body.Labels,
		DriverOpts: input.Body.DriverOpts,
	}

	var response *volumetypes.Volume
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.Body.Name,
		ResourceName:   input.Body.Name,
		User:           user,
		Step:           "Creating volume",
		Message:        "Creating volume",
		SuccessMessage: "Volume created successfully",
		Metadata: database.JSON{
			"action": "create_volume",
			"driver": input.Body.Driver,
		},
	}, func(runtimeCtx context.Context) error {
		var createErr error
		response, createErr = h.volumeService.CreateVolume(runtimeCtx, options, *user)
		return createErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to create volume").Error())
	}
	response.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()

	return &handlerutil.Out[*volumetypes.Volume]{
		Body: base.ApiResponse[*volumetypes.Volume]{
			Success: true,
			Data:    response,
		},
	}, nil
}

// RenameVolume renames an unused Docker volume.
func (h *VolumeHandler) RenameVolume(ctx context.Context, input *RenameVolumeInput) (*handlerutil.Out[*volumetypes.Volume], error) {
	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		remotePath := fmt.Sprintf("/api/environments/0/volumes/%s/rename", url.PathEscape(input.VolumeName))
		response, err := handlerutil.RemoteJSONProxy(h.environmentService.ProxyJSONRequest).JSON[base.ApiResponse[*volumetypes.Volume]](ctx, input.EnvironmentID, http.MethodPost, remotePath, input.Body)
		if err != nil {
			return nil, err
		}
		return &handlerutil.Out[*volumetypes.Volume]{Body: *response}, nil
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	var response *volumetypes.Volume
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Renaming volume",
		Message:        "Renaming volume",
		SuccessMessage: "Volume renamed successfully",
		Metadata: database.JSON{
			"action":  "rename_volume",
			"oldName": input.VolumeName,
			"newName": input.Body.Name,
		},
	}, func(runtimeCtx context.Context) error {
		var renameErr error
		response, renameErr = h.volumeService.RenameVolume(runtimeCtx, input.VolumeName, input.Body.Name, *user)
		return renameErr
	})
	if err != nil {
		var conflictErr *volumeops.ProjectVolumeRenameConflictError
		var inUseErr *volumeops.ProjectVolumeRenameInUseError
		var spaceErr *volumeops.ProjectVolumeRenameInsufficientSpaceError
		switch {
		case errors.Is(err, common.ErrBadRequest):
			return nil, huma.Error400BadRequest(err.Error())
		case errors.As(err, &conflictErr), errors.As(err, &inUseErr):
			return nil, huma.Error409Conflict(err.Error())
		case errors.As(err, &spaceErr):
			return nil, huma.NewError(http.StatusInsufficientStorage, err.Error())
		case errors.Is(err, common.ErrNotFound):
			return nil, huma.Error404NotFound(err.Error())
		default:
			return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to rename volume").Error())
		}
	}
	response.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()

	return &handlerutil.Out[*volumetypes.Volume]{Body: base.ApiResponse[*volumetypes.Volume]{Success: true, Data: response}}, nil
}

// RemoveVolume removes a Docker volume.
func (h *VolumeHandler) RemoveVolume(ctx context.Context, input *RemoveVolumeInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Removing volume",
		Message:        "Removing volume",
		SuccessMessage: "Volume removed successfully",
		Metadata: database.JSON{
			"action": "remove_volume",
			"force":  input.Force,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.DeleteVolume(runtimeCtx, input.VolumeName, input.Force, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to delete volume").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message:    "Volume removed successfully",
				ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			},
		},
	}, nil
}

// PruneVolumes removes all unused Docker volumes.
func (h *VolumeHandler) PruneVolumes(ctx context.Context, input *PruneVolumesInput) (*handlerutil.Out[VolumePruneReportData], error) {
	var report *volumetypes.PruneReport
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume",
		Step:           "Pruning unused volumes",
		Message:        "Pruning unused volumes",
		SuccessMessage: "Volumes pruned successfully",
		Metadata:       database.JSON{"action": "prune_volumes"},
	}, func(runtimeCtx context.Context) error {
		var pruneErr error
		report, pruneErr = h.volumeService.PruneVolumes(runtimeCtx)
		return pruneErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to prune volumes").Error())
	}

	return &handlerutil.Out[VolumePruneReportData]{
		Body: base.ApiResponse[VolumePruneReportData]{
			Success: true,
			Data: VolumePruneReportData{
				VolumesDeleted: report.VolumesDeleted,
				SpaceReclaimed: report.SpaceReclaimed,
				ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			},
		},
	}, nil
}

// GetVolumeUsage returns containers using a specific volume.
func (h *VolumeHandler) GetVolumeUsage(ctx context.Context, input *GetVolumeUsageInput) (*handlerutil.Out[VolumeUsageResponse], error) {
	inUse, containers, err := h.volumeService.GetVolumeUsage(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get volume usage").Error())
	}

	return &handlerutil.Out[VolumeUsageResponse]{
		Body: base.ApiResponse[VolumeUsageResponse]{
			Success: true,
			Data: VolumeUsageResponse{
				InUse:      inUse,
				Containers: containers,
			},
		},
	}, nil
}

// GetVolumeUsageCounts returns counts of volumes by usage status.
func (h *VolumeHandler) GetVolumeUsageCounts(ctx context.Context, input *GetVolumeUsageCountsInput) (*handlerutil.Out[VolumeUsageCountsData], error) {
	_, _, counts, err := h.volumeService.ListVolumesPaginated(ctx, pagination.QueryParams{}, input.IncludeInternal)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get volume counts").Error())
	}

	return &handlerutil.Out[VolumeUsageCountsData]{
		Body: base.ApiResponse[VolumeUsageCountsData]{
			Success: true,
			Data: VolumeUsageCountsData{
				Inuse:  counts.Inuse,
				Unused: counts.Unused,
				Total:  counts.Total,
			},
		},
	}, nil
}

// GetVolumeSizes returns disk usage sizes for all volumes.
// This is a slow operation as it requires calculating disk usage.
func (h *VolumeHandler) GetVolumeSizes(ctx context.Context, input *GetVolumeSizesInput) (*handlerutil.Out[[]VolumeSizeInfo], error) {
	sizes, err := h.volumeService.GetVolumeSizes(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	result := make([]VolumeSizeInfo, 0, len(sizes))
	for name, info := range sizes {
		result = append(result, VolumeSizeInfo{
			Name:     name,
			Size:     info.Size,
			RefCount: info.RefCount,
		})
	}

	return &handlerutil.Out[[]VolumeSizeInfo]{
		Body: base.ApiResponse[[]VolumeSizeInfo]{
			Success: true,
			Data:    result,
		},
	}, nil
}

// --- Volume Backup Handler Methods ---

func (h *VolumeHandler) ListBackups(ctx context.Context, input *ListBackupsInput) (*ListBackupsOutput, error) {
	params := pagination.QueryParams{
		Search: input.Search,
		Sort:   input.Sort,
		Order:  pagination.SortOrder(input.Order),
		Start:  input.Start,
		Limit:  input.Limit,
		Filters: map[string]string{
			"type": input.Type,
		},
	}

	if params.Limit == 0 {
		params.Limit = 20
	}

	backups, paginationResp, err := h.volumeService.ListBackupsPaginated(ctx, input.VolumeName, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	warning := h.volumeService.BackupMountWarning(ctx)

	return &ListBackupsOutput{
		Body: VolumeBackupPaginatedResponse{
			Success:    true,
			Data:       backups,
			Pagination: handlerutil.PaginationResponse(paginationResp),
			Warnings: func() []string {
				if warning == "" {
					return nil
				}
				return []string{warning}
			}(),
		},
	}, nil
}

func (h *VolumeHandler) CreateBackup(ctx context.Context, input *CreateBackupInput) (*handlerutil.Out[volumetypes.BackupEntry], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	request := volumetypes.CreateBackupRequest{}
	if input.Body != nil {
		request = *input.Body
	}
	entry, err := h.volumeService.StartBackup(utils.ActivityRuntimeContext(ctx, h.appCtx), input.EnvironmentID, input.VolumeName, *user, request)
	if errors.Is(err, ErrVolumeBackupAlreadyRunning) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[volumetypes.BackupEntry]{
		Body: base.ApiResponse[volumetypes.BackupEntry]{Success: true, Data: entry},
	}, nil
}

func (h *VolumeHandler) RestoreBackup(ctx context.Context, input *RestoreBackupInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Restoring backup",
		Message:        "Restoring volume backup",
		SuccessMessage: "Restore initiated successfully",
		Metadata: database.JSON{
			"action":   "restore_volume_backup",
			"backupId": input.BackupID,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.RestoreBackup(runtimeCtx, input.VolumeName, input.BackupID, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Restore initiated successfully", ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()},
		},
	}, nil
}

func (h *VolumeHandler) RestoreBackupFiles(ctx context.Context, input *RestoreBackupFilesInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Restoring backup files",
		Message:        "Restoring files from volume backup",
		SuccessMessage: "Restore initiated successfully",
		Metadata: database.JSON{
			"action":    "restore_volume_backup_files",
			"backupId":  input.BackupID,
			"paths":     input.Body.Paths,
			"selectAll": input.Body.SelectAll,
			"search":    input.Body.Search,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.RestoreBackupFiles(runtimeCtx, input.VolumeName, input.BackupID, input.Body, *user)
	})
	if errors.Is(err, common.ErrBadRequest) {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Restore initiated successfully", ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()},
		},
	}, nil
}

func (h *VolumeHandler) BackupHasPath(ctx context.Context, input *BackupHasPathInput) (*handlerutil.Out[BackupHasPathResponse], error) {
	if input.Path == "" {
		return nil, huma.Error400BadRequest("path is required")
	}

	exists, err := h.volumeService.BackupHasPath(ctx, input.BackupID, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[BackupHasPathResponse]{
		Body: base.ApiResponse[BackupHasPathResponse]{
			Success: true,
			Data:    BackupHasPathResponse{Exists: exists},
		},
	}, nil
}

func (h *VolumeHandler) ListBackupFiles(ctx context.Context, input *ListBackupFilesInput) (*handlerutil.Out[[]string], error) {
	files, err := h.volumeService.ListBackupFiles(ctx, input.BackupID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[[]string]{
		Body: base.ApiResponse[[]string]{
			Success: true,
			Data:    files,
		},
	}, nil
}

func (h *VolumeHandler) BrowseBackupFiles(ctx context.Context, input *BrowseBackupFilesInput) (*handlerutil.Page[backuptypes.BackupFileEntry], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, "", "", input.Search)
	items, page, err := h.volumeService.BrowseBackupFiles(ctx, input.BackupID, input.Path, params)
	if errors.Is(err, common.ErrBadRequest) {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Page[backuptypes.BackupFileEntry]{Body: base.Paginated[backuptypes.BackupFileEntry]{
		Success: true, Data: items, Pagination: handlerutil.PaginationResponse(page),
	}}, nil
}

func (h *VolumeHandler) DeleteBackup(ctx context.Context, input *DeleteBackupInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, _ := common.CurrentUserFromContext(ctx)
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume_backup",
		ResourceID:     input.BackupID,
		ResourceName:   input.BackupID,
		User:           user,
		Step:           "Deleting backup",
		Message:        "Deleting volume backup",
		SuccessMessage: "Backup deleted successfully",
		Metadata: database.JSON{
			"action":   "delete_volume_backup",
			"backupId": input.BackupID,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.DeleteBackup(runtimeCtx, input.BackupID, user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Backup deleted successfully", ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()},
		},
	}, nil
}

func (h *VolumeHandler) UploadBackup(ctx context.Context, input *UploadBackupInput) (*handlerutil.Out[*VolumeBackup], error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, _ := common.CurrentUserFromContext(ctx)
	var backup *VolumeBackup
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume_backup",
		ResourceID:     input.BackupID,
		ResourceName:   input.BackupID,
		User:           user,
		Step:           "Uploading backup",
		Message:        "Uploading volume backup to S3",
		SuccessMessage: "Volume backup uploaded successfully",
		Metadata: database.JSON{
			"action":          "upload_volume_backup",
			"backupId":        input.BackupID,
			"s3DestinationId": input.Body.S3DestinationID,
		},
	}, func(runtimeCtx context.Context) error {
		var uploadErr error
		backup, uploadErr = h.volumeService.UploadBackup(runtimeCtx, input.BackupID, input.Body.S3DestinationID)
		return uploadErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	backup.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	return &handlerutil.Out[*VolumeBackup]{Body: base.ApiResponse[*VolumeBackup]{Success: true, Data: backup}}, nil
}

func (h *VolumeHandler) UploadAndRestore(ctx context.Context, input *UploadAndRestoreInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	file, session, cleanup, err := h.uploadService.Consume(ctx, uploadtypes.KindVolumeBackup, input.Body.UploadID)
	if err != nil {
		if httpErr := upload.SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to open upload").Error())
	}
	defer cleanup()

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Uploading backup",
		Message:        "Uploading and restoring volume backup",
		SuccessMessage: "Backup uploaded and restored successfully",
		Metadata: database.JSON{
			"action":   "upload_restore_volume_backup",
			"filename": session.Filename,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.UploadAndRestore(runtimeCtx, input.VolumeName, file, session.Filename, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Backup uploaded and restored successfully", ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()},
		},
	}, nil
}
