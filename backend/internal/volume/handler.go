package volume

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
)

// VolumeHandler provides Huma-based volume management endpoints.
type VolumeHandler struct {
	volumeService   *VolumeService
	dockerService   *docker.DockerClientService
	activityService *activity.ActivityService
	uploadService   *upload.UploadService
	appCtx          context.Context
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
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
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
	Body          struct {
		Paths []string `json:"paths" doc:"Paths to restore from backup"`
	}
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

// RegisterVolumes registers volume management routes using Huma.
func RegisterVolumes(api huma.API, dockerService *docker.DockerClientService, volumeService *VolumeService, activityService *activity.ActivityService, uploadService *upload.UploadService, appCtx handlerutil.ActivityAppContext) {
	h := &VolumeHandler{
		volumeService:   volumeService,
		dockerService:   dockerService,
		activityService: activityService,
		uploadService:   uploadService,
		appCtx:          appCtx.Context(),
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
		OperationID: "list-volume-backups",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/backups",
		Summary:     "List volume backups",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.ListBackups)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-volume-backup",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups",
		Summary:     "Create volume backup",
		Tags:        []string{"Volume Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
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

func (h *VolumeHandler) CreateBackup(ctx context.Context, input *CreateBackupInput) (*handlerutil.Out[*VolumeBackup], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	var backup *VolumeBackup
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Creating backup",
		Message:        "Creating volume backup",
		SuccessMessage: "Volume backup created successfully",
		Metadata:       database.JSON{"action": "create_volume_backup"},
	}, func(runtimeCtx context.Context) error {
		var backupErr error
		backup, backupErr = h.volumeService.CreateBackup(runtimeCtx, input.VolumeName, *user)
		return backupErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	backup.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	return &handlerutil.Out[*VolumeBackup]{
		Body: base.ApiResponse[*VolumeBackup]{
			Success: true,
			Data:    backup,
		},
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

	if len(input.Body.Paths) == 0 {
		return nil, huma.Error400BadRequest("paths are required")
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
			"action":   "restore_volume_backup_files",
			"backupId": input.BackupID,
			"paths":    input.Body.Paths,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.RestoreBackupFiles(runtimeCtx, input.VolumeName, input.BackupID, input.Body.Paths, *user)
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
