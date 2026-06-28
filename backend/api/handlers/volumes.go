package handlers

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/base"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
)

// volumeHandler provides Huma-based volume management endpoints.
type volumeHandler struct {
	volumeService   *services.VolumeService
	dockerService   *services.DockerClientService
	activityService *services.ActivityService
	appCtx          context.Context
}

// --- Huma Input/Output Wrappers ---

// volumeUsageCountsData represents the counts of volumes by usage status.
// This is a local type to avoid schema naming conflicts with image.UsageCounts.
type volumeUsageCountsData struct {
	Inuse  int `json:"inuse"`
	Unused int `json:"unused"`
	Total  int `json:"total"`
}

// volumePaginatedResponse is the paginated response for volumes.
type volumePaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []volumetypes.Volume    `json:"data"`
	Counts     volumeUsageCountsData   `json:"counts"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type listVolumesInput struct {
	EnvironmentID   string `path:"id" doc:"Environment ID"`
	Search          string `query:"search" doc:"Search query"`
	Sort            string `query:"sort" doc:"Column to sort by"`
	Order           string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start           int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit           int    `query:"limit" default:"20" doc:"Number of items per page"`
	InUse           string `query:"inUse" doc:"Filter by in-use status (true/false)"`
	IncludeInternal bool   `query:"includeInternal" default:"false" doc:"Include internal volumes"`
}

type listVolumesOutput struct {
	Body volumePaginatedResponse
}

type getVolumeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type getVolumeOutput struct {
	Body base.ApiResponse[*volumetypes.Volume]
}

type createVolumeInput struct {
	EnvironmentID string             `path:"id" doc:"Environment ID"`
	Body          volumetypes.Create `doc:"Volume creation data"`
}

type createVolumeOutput struct {
	Body base.ApiResponse[*volumetypes.Volume]
}

type removeVolumeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Force         bool   `query:"force" doc:"Force removal"`
}

type removeVolumeOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type pruneVolumesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// volumePruneReportData represents the result of a volume prune operation.
// This is a local type to avoid schema naming conflicts with image.PruneReport.
type volumePruneReportData struct {
	VolumesDeleted []string `json:"volumesDeleted,omitempty"`
	SpaceReclaimed uint64   `json:"spaceReclaimed"`
	ActivityID     *string  `json:"activityId,omitempty"`
}

type pruneVolumesOutput struct {
	Body base.ApiResponse[volumePruneReportData]
}

type getVolumeUsageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

// volumeUsageResponse represents volume usage information.
type volumeUsageResponse struct {
	InUse      bool     `json:"inUse"`
	Containers []string `json:"containers"`
}

type getVolumeUsageOutput struct {
	Body base.ApiResponse[volumeUsageResponse]
}

type getVolumeUsageCountsInput struct {
	EnvironmentID   string `path:"id" doc:"Environment ID"`
	IncludeInternal bool   `query:"includeInternal" default:"false" doc:"Include internal volumes"`
}

type getVolumeUsageCountsOutput struct {
	Body base.ApiResponse[volumeUsageCountsData]
}

type getVolumeSizesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// volumeSizeInfo represents size information for a single volume.
type volumeSizeInfo struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	RefCount int64  `json:"refCount"`
}

type getVolumeSizesOutput struct {
	Body base.ApiResponse[[]volumeSizeInfo]
}

// --- Volume Browser & Backup ---

type browseDirectoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" default:"/" doc:"Directory path to browse"`
}

type browseDirectoryOutput struct {
	Body base.ApiResponse[[]volumetypes.FileEntry]
}

type getFileContentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" doc:"File path"`
	MaxBytes      int64  `query:"maxBytes" default:"1048576" doc:"Maximum bytes to read (default 1MB)"`
}

type fileContentResponse struct {
	Content  []byte `json:"content"`
	MimeType string `json:"mimeType"`
}

type getFileContentOutput struct {
	Body base.ApiResponse[fileContentResponse]
}

type downloadFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" doc:"File path"`
}

type uploadFileInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	VolumeName    string         `path:"volumeName" doc:"Volume name"`
	Path          string         `query:"path" default:"/" doc:"Destination path"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

type createDirectoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" doc:"Directory path to create"`
}

type deleteFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" doc:"File or directory path to delete"`
}

type listBackupsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Limit"`
}

type volumeBackupPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []models.VolumeBackup   `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
	Warnings   []string                `json:"warnings,omitempty"`
}

type listBackupsOutput struct {
	Body volumeBackupPaginatedResponse
}

type createBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type createBackupOutput struct {
	Body base.ApiResponse[*models.VolumeBackup]
}

type restoreBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type restoreBackupOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type restoreBackupFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Body          struct {
		Paths []string `json:"paths" doc:"Paths to restore from backup"`
	}
}

type restoreBackupFilesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type backupHasPathInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Path          string `query:"path" doc:"Path to check"`
}

type backupHasPathResponse struct {
	Exists bool `json:"exists"`
}

type backupHasPathOutput struct {
	Body base.ApiResponse[backupHasPathResponse]
}

type listBackupFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type listBackupFilesOutput struct {
	Body base.ApiResponse[[]string]
}

type deleteBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type deleteBackupOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type downloadBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type uploadAndRestoreInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	VolumeName    string         `path:"volumeName" doc:"Volume name"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

type uploadAndRestoreOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// RegisterVolumes registers volume management routes using Huma.
//
//nolint:maintidx // long but flat Huma route-registration function; complexity is sequential, not branching
func RegisterVolumes(api huma.API, dockerService *services.DockerClientService, volumeService *services.VolumeService, activityService *services.ActivityService, appCtx ActivityAppContext) {
	h := &volumeHandler{
		volumeService:   volumeService,
		dockerService:   dockerService,
		activityService: activityService,
		appCtx:          appCtx.contextInternal(),
	}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-usage-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/counts",
		Summary:     "Get volume usage counts",
		Description: "Get counts of volumes in use, unused, and total",
		Tags:        []string{"Volumes"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesList, h.getVolumeUsageCountsInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-volumes",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes",
		Summary:     "List volumes",
		Description: "Get a paginated list of Docker volumes",
		Tags:        []string{"Volumes"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesList, h.listVolumesInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}",
		Summary:     "Get volume by name",
		Description: "Get a Docker volume by its name",
		Tags:        []string{"Volumes"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesRead, h.getVolumeInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-volume",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes",
		Summary:     "Create a volume",
		Description: "Create a new Docker volume",
		Tags:        []string{"Volumes"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesCreate, h.createVolumeInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "remove-volume",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/volumes/{volumeName}",
		Summary:     "Remove a volume",
		Description: "Remove a Docker volume by name",
		Tags:        []string{"Volumes"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesDelete, h.removeVolumeInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "prune-volumes",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/prune",
		Summary:     "Prune unused volumes",
		Description: "Remove all unused Docker volumes",
		Tags:        []string{"Volumes"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesPrune, h.pruneVolumesInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-usage",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/usage",
		Summary:     "Get volume usage",
		Description: "Get containers using a specific volume",
		Tags:        []string{"Volumes"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesRead, h.getVolumeUsageInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-sizes",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/sizes",
		Summary:     "Get volume sizes",
		Description: "Get disk usage sizes for all volumes (slow operation)",
		Tags:        []string{"Volumes"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesList, h.getVolumeSizesInternal)

	// --- Volume Browsing Endpoints ---

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "browse-volume-directory",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/browse",
		Summary:     "List volume directory",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBrowse, h.browseDirectoryInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-file-content",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/browse/content",
		Summary:     "Get file content preview",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBrowse, h.getFileContentInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "download-volume-file",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/browse/download",
		Summary:     "Download file from volume",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBrowse, h.downloadFileInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "upload-volume-file",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/browse/upload",
		Summary:     "Upload file to volume",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		RequestBody: &huma.RequestBody{
			Content: map[string]*huma.MediaType{
				"multipart/form-data": {
					Schema: &huma.Schema{
						Type: "object",
						Properties: map[string]*huma.Schema{
							"file": {
								Type:        "string",
								Format:      "binary",
								Description: "File to upload",
							},
						},
						Required: []string{"file"},
					},
				},
			},
		},
	}, authz.PermVolumesUpload, h.uploadFileInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-volume-directory",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/browse/mkdir",
		Summary:     "Create directory in volume",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesUpload, h.createDirectoryInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-volume-file",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/volumes/{volumeName}/browse",
		Summary:     "Delete file or directory in volume",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesDelete, h.deleteFileInternal)

	// --- Volume Backup Endpoints ---

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-volume-backups",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/backups",
		Summary:     "List volume backups",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBrowse, h.listBackupsInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-volume-backup",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups",
		Summary:     "Create volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBackup, h.createBackupInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "restore-volume-backup",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/{backupId}/restore",
		Summary:     "Restore volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBackup, h.restoreBackupInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "restore-volume-backup-files",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/{backupId}/restore-files",
		Summary:     "Restore specific files from a volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBackup, h.restoreBackupFilesInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-volume-backup",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/volumes/backups/{backupId}",
		Summary:     "Delete volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBackup, h.deleteBackupInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "download-volume-backup",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/download",
		Summary:     "Download volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBrowse, h.downloadBackupInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "backup-has-path",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/has-path",
		Summary:     "Check if backup contains path",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBrowse, h.backupHasPathInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-backup-files",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/files",
		Summary:     "List files in a volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermVolumesBrowse, h.listBackupFilesInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "upload-volume-backup",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/upload",
		Summary:     "Upload and restore volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		RequestBody: &huma.RequestBody{
			Content: map[string]*huma.MediaType{
				"multipart/form-data": {
					Schema: &huma.Schema{
						Type: "object",
						Properties: map[string]*huma.Schema{
							"file": {
								Type:        "string",
								Format:      "binary",
								Description: "Backup archive (tar.gz)",
							},
						},
						Required: []string{"file"},
					},
				},
			},
		},
	}, authz.PermVolumesUpload, h.uploadAndRestoreInternal)
}

// ListVolumes returns a paginated list of volumes.
func (h *volumeHandler) listVolumesInternal(ctx context.Context, input *listVolumesInput) (*listVolumesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.InUse != "" {
		params.Filters["inUse"] = input.InUse
	}

	if params.Limit == 0 {
		params.Limit = 20
	}

	volumes, paginationResp, counts, err := h.volumeService.ListVolumesPaginated(ctx, params, input.IncludeInternal)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeListError{Err: err}).Error())
	}

	if volumes == nil {
		volumes = []volumetypes.Volume{}
	}

	return &listVolumesOutput{
		Body: volumePaginatedResponse{
			Success: true,
			Data:    volumes,
			Counts: volumeUsageCountsData{
				Inuse:  counts.Inuse,
				Unused: counts.Unused,
				Total:  counts.Total,
			},
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// GetVolume returns a volume by name.
func (h *volumeHandler) getVolumeInternal(ctx context.Context, input *getVolumeInput) (*getVolumeOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	vol, err := h.volumeService.GetVolumeByName(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.Error404NotFound((&common.VolumeNotFoundError{Err: err}).Error())
	}

	return &getVolumeOutput{
		Body: base.ApiResponse[*volumetypes.Volume]{
			Success: true,
			Data:    vol,
		},
	}, nil
}

// CreateVolume creates a new Docker volume.
func (h *volumeHandler) createVolumeInternal(ctx context.Context, input *createVolumeInput) (*createVolumeOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
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
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.Body.Name,
		ResourceName:   input.Body.Name,
		User:           user,
		Step:           "Creating volume",
		Message:        "Creating volume",
		SuccessMessage: "Volume created successfully",
		Metadata: models.JSON{
			"action": "create_volume",
			"driver": input.Body.Driver,
		},
	}, func(runtimeCtx context.Context) error {
		var createErr error
		response, createErr = h.volumeService.CreateVolume(runtimeCtx, options, *user)
		return createErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeCreationError{Err: err}).Error())
	}
	response.ActivityID = utils.StringPtrFromTrimmed(activityID)

	return &createVolumeOutput{
		Body: base.ApiResponse[*volumetypes.Volume]{
			Success: true,
			Data:    response,
		},
	}, nil
}

// RemoveVolume removes a Docker volume.
func (h *volumeHandler) removeVolumeInternal(ctx context.Context, input *removeVolumeInput) (*removeVolumeOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Removing volume",
		Message:        "Removing volume",
		SuccessMessage: "Volume removed successfully",
		Metadata: models.JSON{
			"action": "remove_volume",
			"force":  input.Force,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.DeleteVolume(runtimeCtx, input.VolumeName, input.Force, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeDeletionError{Err: err}).Error())
	}

	return &removeVolumeOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message:    "Volume removed successfully",
				ActivityID: utils.StringPtrFromTrimmed(activityID),
			},
		},
	}, nil
}

// PruneVolumes removes all unused Docker volumes.
func (h *volumeHandler) pruneVolumesInternal(ctx context.Context, input *pruneVolumesInput) (*pruneVolumesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	var report *volumetypes.PruneReport
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume",
		Step:           "Pruning unused volumes",
		Message:        "Pruning unused volumes",
		SuccessMessage: "Volumes pruned successfully",
		Metadata:       models.JSON{"action": "prune_volumes"},
	}, func(runtimeCtx context.Context) error {
		var pruneErr error
		report, pruneErr = h.volumeService.PruneVolumes(runtimeCtx)
		return pruneErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumePruneError{Err: err}).Error())
	}

	return &pruneVolumesOutput{
		Body: base.ApiResponse[volumePruneReportData]{
			Success: true,
			Data: volumePruneReportData{
				VolumesDeleted: report.VolumesDeleted,
				SpaceReclaimed: report.SpaceReclaimed,
				ActivityID:     utils.StringPtrFromTrimmed(activityID),
			},
		},
	}, nil
}

// GetVolumeUsage returns containers using a specific volume.
func (h *volumeHandler) getVolumeUsageInternal(ctx context.Context, input *getVolumeUsageInput) (*getVolumeUsageOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	inUse, containers, err := h.volumeService.GetVolumeUsage(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeUsageError{Err: err}).Error())
	}

	return &getVolumeUsageOutput{
		Body: base.ApiResponse[volumeUsageResponse]{
			Success: true,
			Data: volumeUsageResponse{
				InUse:      inUse,
				Containers: containers,
			},
		},
	}, nil
}

// GetVolumeUsageCounts returns counts of volumes by usage status.
func (h *volumeHandler) getVolumeUsageCountsInternal(ctx context.Context, input *getVolumeUsageCountsInput) (*getVolumeUsageCountsOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	_, _, counts, err := h.volumeService.ListVolumesPaginated(ctx, pagination.QueryParams{}, input.IncludeInternal)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeCountsError{Err: err}).Error())
	}

	return &getVolumeUsageCountsOutput{
		Body: base.ApiResponse[volumeUsageCountsData]{
			Success: true,
			Data: volumeUsageCountsData{
				Inuse:  counts.Inuse,
				Unused: counts.Unused,
				Total:  counts.Total,
			},
		},
	}, nil
}

// GetVolumeSizes returns disk usage sizes for all volumes.
// This is a slow operation as it requires calculating disk usage.
func (h *volumeHandler) getVolumeSizesInternal(ctx context.Context, _ *getVolumeSizesInput) (*getVolumeSizesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	sizes, err := h.volumeService.GetVolumeSizes(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	result := make([]volumeSizeInfo, 0, len(sizes))
	for name, info := range sizes {
		result = append(result, volumeSizeInfo{
			Name:     name,
			Size:     info.Size,
			RefCount: info.RefCount,
		})
	}

	return &getVolumeSizesOutput{
		Body: base.ApiResponse[[]volumeSizeInfo]{
			Success: true,
			Data:    result,
		},
	}, nil
}

// --- Volume Browser Handler Methods ---

func (h *volumeHandler) browseDirectoryInternal(ctx context.Context, input *browseDirectoryInput) (*browseDirectoryOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	entries, err := h.volumeService.ListDirectory(ctx, input.VolumeName, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &browseDirectoryOutput{
		Body: base.ApiResponse[[]volumetypes.FileEntry]{
			Success: true,
			Data:    entries,
		},
	}, nil
}

func (h *volumeHandler) getFileContentInternal(ctx context.Context, input *getFileContentInput) (*getFileContentOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	content, mimeType, err := h.volumeService.GetFileContent(ctx, input.VolumeName, input.Path, input.MaxBytes)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &getFileContentOutput{
		Body: base.ApiResponse[fileContentResponse]{
			Success: true,
			Data: fileContentResponse{
				Content:  content,
				MimeType: mimeType,
			},
		},
	}, nil
}

func (h *volumeHandler) downloadFileInternal(ctx context.Context, input *downloadFileInput) (*huma.StreamResponse, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	reader, size, err := h.volumeService.DownloadFile(ctx, input.VolumeName, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			defer func() { _ = reader.Close() }()

			humaCtx.SetHeader("Content-Type", "application/octet-stream")
			humaCtx.SetHeader("Content-Disposition", "attachment; filename="+path.Base(input.Path))
			humaCtx.SetHeader("Content-Length", strconv.FormatInt(size, 10))

			writer := humaCtx.BodyWriter()
			_, _ = io.Copy(writer, reader)
		},
	}, nil
}

func (h *volumeHandler) uploadFileInternal(ctx context.Context, input *uploadFileInput) (*base.ApiResponse[base.MessageResponse], error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	files := input.RawBody.File["file"]
	if len(files) == 0 {
		return nil, huma.Error400BadRequest((&common.NoFileUploadedError{}).Error())
	}

	fileHeader := files[0]
	file, err := fileHeader.Open()
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.FileUploadReadError{Err: err}).Error())
	}
	defer func() { _ = file.Close() }()

	user, _ := humamw.GetCurrentUserFromContext(ctx)
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Uploading file",
		Message:        "Uploading file to volume",
		SuccessMessage: "File uploaded successfully",
		Metadata: models.JSON{
			"action":   "upload_volume_file",
			"path":     input.Path,
			"filename": fileHeader.Filename,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.UploadFile(runtimeCtx, input.VolumeName, input.Path, file, fileHeader.Filename, user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "File uploaded successfully", ActivityID: utils.StringPtrFromTrimmed(activityID)},
	}, nil
}

func (h *volumeHandler) createDirectoryInternal(ctx context.Context, input *createDirectoryInput) (*base.ApiResponse[base.MessageResponse], error) {
	return h.runVolumePathActivityInternal(ctx, input.EnvironmentID, input.VolumeName, input.Path, volumePathActivityConfigInternal{
		Step:           "Creating directory",
		Message:        "Creating directory in volume",
		SuccessMessage: "Directory created successfully",
		MetadataAction: "create_volume_directory",
		Action: func(runtimeCtx context.Context, volumeName, path string, user *models.User) error {
			return h.volumeService.CreateDirectory(runtimeCtx, volumeName, path, user)
		},
	})
}

func (h *volumeHandler) deleteFileInternal(ctx context.Context, input *deleteFileInput) (*base.ApiResponse[base.MessageResponse], error) {
	return h.runVolumePathActivityInternal(ctx, input.EnvironmentID, input.VolumeName, input.Path, volumePathActivityConfigInternal{
		Step:           "Deleting file",
		Message:        "Deleting file or directory from volume",
		SuccessMessage: "Deleted successfully",
		MetadataAction: "delete_volume_file",
		Action: func(runtimeCtx context.Context, volumeName, path string, user *models.User) error {
			return h.volumeService.DeleteFile(runtimeCtx, volumeName, path, user)
		},
	})
}

type volumePathActivityConfigInternal struct {
	Step           string
	Message        string
	SuccessMessage string
	MetadataAction string
	Action         func(context.Context, string, string, *models.User) error
}

func (h *volumeHandler) runVolumePathActivityInternal(ctx context.Context, environmentID, volumeName, volumePath string, cfg volumePathActivityConfigInternal) (*base.ApiResponse[base.MessageResponse], error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, _ := humamw.GetCurrentUserFromContext(ctx)
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  environmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     volumeName,
		ResourceName:   volumeName,
		User:           user,
		Step:           cfg.Step,
		Message:        cfg.Message,
		SuccessMessage: cfg.SuccessMessage,
		Metadata: models.JSON{
			"action": cfg.MetadataAction,
			"path":   volumePath,
		},
	}, func(runtimeCtx context.Context) error {
		return cfg.Action(runtimeCtx, volumeName, volumePath, user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: cfg.SuccessMessage, ActivityID: utils.StringPtrFromTrimmed(activityID)},
	}, nil
}

// --- Volume Backup Handler Methods ---

func (h *volumeHandler) listBackupsInternal(ctx context.Context, input *listBackupsInput) (*listBackupsOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: input.Search,
		},
		SortParams: pagination.SortParams{
			Sort:  input.Sort,
			Order: pagination.SortOrder(input.Order),
		},
		Params: pagination.Params{
			Start: input.Start,
			Limit: input.Limit,
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

	return &listBackupsOutput{
		Body: volumeBackupPaginatedResponse{
			Success:    true,
			Data:       backups,
			Pagination: toPaginationResponseInternal(paginationResp),
			Warnings: func() []string {
				if warning == "" {
					return nil
				}
				return []string{warning}
			}(),
		},
	}, nil
}

func (h *volumeHandler) createBackupInternal(ctx context.Context, input *createBackupInput) (*createBackupOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	var backup *models.VolumeBackup
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Creating backup",
		Message:        "Creating volume backup",
		SuccessMessage: "Volume backup created successfully",
		Metadata:       models.JSON{"action": "create_volume_backup"},
	}, func(runtimeCtx context.Context) error {
		var backupErr error
		backup, backupErr = h.volumeService.CreateBackup(runtimeCtx, input.VolumeName, *user)
		return backupErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	backup.ActivityID = utils.StringPtrFromTrimmed(activityID)
	return &createBackupOutput{
		Body: base.ApiResponse[*models.VolumeBackup]{
			Success: true,
			Data:    backup,
		},
	}, nil
}

func (h *volumeHandler) restoreBackupInternal(ctx context.Context, input *restoreBackupInput) (*restoreBackupOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Restoring backup",
		Message:        "Restoring volume backup",
		SuccessMessage: "Restore initiated successfully",
		Metadata: models.JSON{
			"action":   "restore_volume_backup",
			"backupId": input.BackupID,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.RestoreBackup(runtimeCtx, input.VolumeName, input.BackupID, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &restoreBackupOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Restore initiated successfully", ActivityID: utils.StringPtrFromTrimmed(activityID)},
		},
	}, nil
}

func (h *volumeHandler) restoreBackupFilesInternal(ctx context.Context, input *restoreBackupFilesInput) (*restoreBackupFilesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	if len(input.Body.Paths) == 0 {
		return nil, huma.Error400BadRequest("paths are required")
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Restoring backup files",
		Message:        "Restoring files from volume backup",
		SuccessMessage: "Restore initiated successfully",
		Metadata: models.JSON{
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

	return &restoreBackupFilesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Restore initiated successfully", ActivityID: utils.StringPtrFromTrimmed(activityID)},
		},
	}, nil
}

func (h *volumeHandler) backupHasPathInternal(ctx context.Context, input *backupHasPathInput) (*backupHasPathOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.Path == "" {
		return nil, huma.Error400BadRequest("path is required")
	}

	exists, err := h.volumeService.BackupHasPath(ctx, input.BackupID, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &backupHasPathOutput{
		Body: base.ApiResponse[backupHasPathResponse]{
			Success: true,
			Data:    backupHasPathResponse{Exists: exists},
		},
	}, nil
}

func (h *volumeHandler) listBackupFilesInternal(ctx context.Context, input *listBackupFilesInput) (*listBackupFilesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	files, err := h.volumeService.ListBackupFiles(ctx, input.BackupID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &listBackupFilesOutput{
		Body: base.ApiResponse[[]string]{
			Success: true,
			Data:    files,
		},
	}, nil
}

func (h *volumeHandler) deleteBackupInternal(ctx context.Context, input *deleteBackupInput) (*deleteBackupOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, _ := humamw.GetCurrentUserFromContext(ctx)
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume_backup",
		ResourceID:     input.BackupID,
		ResourceName:   input.BackupID,
		User:           user,
		Step:           "Deleting backup",
		Message:        "Deleting volume backup",
		SuccessMessage: "Backup deleted successfully",
		Metadata: models.JSON{
			"action":   "delete_volume_backup",
			"backupId": input.BackupID,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.DeleteBackup(runtimeCtx, input.BackupID, user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &deleteBackupOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Backup deleted successfully", ActivityID: utils.StringPtrFromTrimmed(activityID)},
		},
	}, nil
}

func (h *volumeHandler) downloadBackupInternal(ctx context.Context, input *downloadBackupInput) (*huma.StreamResponse, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, _ := humamw.GetCurrentUserFromContext(ctx)
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

func (h *volumeHandler) uploadAndRestoreInternal(ctx context.Context, input *uploadAndRestoreInput) (*uploadAndRestoreOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	files := input.RawBody.File["file"]
	if len(files) == 0 {
		return nil, huma.Error400BadRequest((&common.NoFileUploadedError{}).Error())
	}

	fileHeader := files[0]
	file, err := fileHeader.Open()
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.FileUploadReadError{Err: err}).Error())
	}
	defer func() { _ = file.Close() }()

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Uploading backup",
		Message:        "Uploading and restoring volume backup",
		SuccessMessage: "Backup uploaded and restored successfully",
		Metadata: models.JSON{
			"action":   "upload_restore_volume_backup",
			"filename": fileHeader.Filename,
		},
	}, func(runtimeCtx context.Context) error {
		return h.volumeService.UploadAndRestore(runtimeCtx, input.VolumeName, file, fileHeader.Filename, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &uploadAndRestoreOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Backup uploaded and restored successfully", ActivityID: utils.StringPtrFromTrimmed(activityID)},
		},
	}, nil
}
