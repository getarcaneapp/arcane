package handlers

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"path"

	"github.com/danielgtaylor/huma/v2"
	"github.com/docker/docker/api/types/volume"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/base"
	volumetypes "github.com/getarcaneapp/arcane/types/volume"
)

// VolumeHandler provides Huma-based volume management endpoints.
type VolumeHandler struct {
	volumeService *services.VolumeService
	dockerService *services.DockerClientService
}

// --- Huma Input/Output Wrappers ---

// VolumeUsageCountsData represents the counts of volumes by usage status.
// This is a local type to avoid schema naming conflicts with image.UsageCounts.
type VolumeUsageCountsData struct {
	Inuse  int `json:"inuse"`
	Unused int `json:"unused"`
	Total  int `json:"total"`
}

// VolumePaginatedResponse is the paginated response for volumes.
type VolumePaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []volumetypes.Volume    `json:"data"`
	Counts     VolumeUsageCountsData   `json:"counts"`
	Pagination base.PaginationResponse `json:"pagination"`
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
	Body VolumePaginatedResponse
}

type GetVolumeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type GetVolumeOutput struct {
	Body base.ApiResponse[*volumetypes.Volume]
}

type CreateVolumeInput struct {
	EnvironmentID string             `path:"id" doc:"Environment ID"`
	Body          volumetypes.Create `doc:"Volume creation data"`
}

type CreateVolumeOutput struct {
	Body base.ApiResponse[*volumetypes.Volume]
}

type RemoveVolumeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Force         bool   `query:"force" doc:"Force removal"`
}

type RemoveVolumeOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PruneVolumesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// VolumePruneReportData represents the result of a volume prune operation.
// This is a local type to avoid schema naming conflicts with image.PruneReport.
type VolumePruneReportData struct {
	VolumesDeleted []string `json:"volumesDeleted,omitempty"`
	SpaceReclaimed uint64   `json:"spaceReclaimed"`
}

type PruneVolumesOutput struct {
	Body base.ApiResponse[VolumePruneReportData]
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

type GetVolumeUsageOutput struct {
	Body base.ApiResponse[VolumeUsageResponse]
}

type GetVolumeUsageCountsInput struct {
	EnvironmentID   string `path:"id" doc:"Environment ID"`
	IncludeInternal bool   `query:"includeInternal" default:"false" doc:"Include internal volumes"`
}

type GetVolumeUsageCountsOutput struct {
	Body base.ApiResponse[VolumeUsageCountsData]
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

type GetVolumeSizesOutput struct {
	Body base.ApiResponse[[]VolumeSizeInfo]
}

// --- Volume Browser & Backup ---

type BrowseDirectoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" default:"/" doc:"Directory path to browse"`
}

type BrowseDirectoryOutput struct {
	Body base.ApiResponse[[]volumetypes.FileEntry]
}

type GetFileContentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" doc:"File path"`
	MaxBytes      int64  `query:"maxBytes" default:"1048576" doc:"Maximum bytes to read (default 1MB)"`
}

type FileContentResponse struct {
	Content  []byte `json:"content"`
	MimeType string `json:"mimeType"`
}

type GetFileContentOutput struct {
	Body base.ApiResponse[FileContentResponse]
}

type DownloadFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" doc:"File path"`
}

type DownloadFileOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	ContentLength      int64  `header:"Content-Length"`
	Body               io.ReadCloser
}

type UploadFileInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	VolumeName    string         `path:"volumeName" doc:"Volume name"`
	Path          string         `query:"path" default:"/" doc:"Destination path"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

type CreateDirectoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" doc:"Directory path to create"`
}

type DeleteFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Path          string `query:"path" doc:"File or directory path to delete"`
}

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
	Data       []models.VolumeBackup   `json:"data"`
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

type CreateBackupOutput struct {
	Body base.ApiResponse[*models.VolumeBackup]
}

type RestoreBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type RestoreBackupOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type RestoreBackupFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Body          struct {
		Paths []string `json:"paths" doc:"Paths to restore from backup"`
	}
}

type RestoreBackupFilesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type BackupHasPathInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Path          string `query:"path" doc:"Path to check"`
}

type BackupHasPathResponse struct {
	Exists bool `json:"exists"`
}

type BackupHasPathOutput struct {
	Body base.ApiResponse[BackupHasPathResponse]
}

type ListBackupFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type ListBackupFilesOutput struct {
	Body base.ApiResponse[[]string]
}

type DeleteBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type DeleteBackupOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type DownloadBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type DownloadBackupOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	ContentLength      int64  `header:"Content-Length"`
	Body               io.ReadCloser
}

type UploadAndRestoreInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	VolumeName    string         `path:"volumeName" doc:"Volume name"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

type UploadAndRestoreOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// RegisterVolumes registers volume management routes using Huma.
func RegisterVolumes(api huma.API, dockerService *services.DockerClientService, volumeService *services.VolumeService) {
	h := &VolumeHandler{
		volumeService: volumeService,
		dockerService: dockerService,
	}

	huma.Register(api, huma.Operation{
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
	}, h.GetVolumeUsageCounts)

	huma.Register(api, huma.Operation{
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
	}, h.ListVolumes)

	huma.Register(api, huma.Operation{
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
	}, h.GetVolume)

	huma.Register(api, huma.Operation{
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
	}, h.CreateVolume)

	huma.Register(api, huma.Operation{
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
	}, h.RemoveVolume)

	huma.Register(api, huma.Operation{
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
	}, h.PruneVolumes)

	huma.Register(api, huma.Operation{
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
	}, h.GetVolumeUsage)

	huma.Register(api, huma.Operation{
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
	}, h.GetVolumeSizes)

	// --- Volume Browsing Endpoints ---

	huma.Register(api, huma.Operation{
		OperationID: "browse-volume-directory",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/browse",
		Summary:     "List volume directory",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.BrowseDirectory)

	huma.Register(api, huma.Operation{
		OperationID: "get-volume-file-content",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/browse/content",
		Summary:     "Get file content preview",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetFileContent)

	huma.Register(api, huma.Operation{
		OperationID: "download-volume-file",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/browse/download",
		Summary:     "Download file from volume",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DownloadFile)

	huma.Register(api, huma.Operation{
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
	}, h.UploadFile)

	huma.Register(api, huma.Operation{
		OperationID: "create-volume-directory",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/browse/mkdir",
		Summary:     "Create directory in volume",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateDirectory)

	huma.Register(api, huma.Operation{
		OperationID: "delete-volume-file",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/volumes/{volumeName}/browse",
		Summary:     "Delete file or directory in volume",
		Tags:        []string{"Volume Browser"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteFile)

	// --- Volume Backup Endpoints ---

	huma.Register(api, huma.Operation{
		OperationID: "list-volume-backups",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/backups",
		Summary:     "List volume backups",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListBackups)

	huma.Register(api, huma.Operation{
		OperationID: "create-volume-backup",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups",
		Summary:     "Create volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateBackup)

	huma.Register(api, huma.Operation{
		OperationID: "restore-volume-backup",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/{backupId}/restore",
		Summary:     "Restore volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.RestoreBackup)

	huma.Register(api, huma.Operation{
		OperationID: "restore-volume-backup-files",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/backups/{backupId}/restore-files",
		Summary:     "Restore specific files from a volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.RestoreBackupFiles)

	huma.Register(api, huma.Operation{
		OperationID: "delete-volume-backup",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/volumes/backups/{backupId}",
		Summary:     "Delete volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteBackup)

	huma.Register(api, huma.Operation{
		OperationID: "download-volume-backup",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/download",
		Summary:     "Download volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DownloadBackup)

	huma.Register(api, huma.Operation{
		OperationID: "backup-has-path",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/has-path",
		Summary:     "Check if backup contains path",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.BackupHasPath)

	huma.Register(api, huma.Operation{
		OperationID: "list-backup-files",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/backups/{backupId}/files",
		Summary:     "List files in a volume backup",
		Tags:        []string{"Volume Backup"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListBackupFiles)

	huma.Register(api, huma.Operation{
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
	}, h.UploadAndRestore)
}

// ListVolumes returns a paginated list of volumes.
func (h *VolumeHandler) ListVolumes(ctx context.Context, input *ListVolumesInput) (*ListVolumesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	filters := make(map[string]string)
	if input.InUse != "" {
		filters["inUse"] = input.InUse
	}

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: input.Search,
		},
		SortParams: pagination.SortParams{
			Sort:  input.Sort,
			Order: pagination.SortOrder(input.Order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: input.Start,
			Limit: input.Limit,
		},
		Filters: filters,
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

	return &ListVolumesOutput{
		Body: VolumePaginatedResponse{
			Success: true,
			Data:    volumes,
			Counts: VolumeUsageCountsData{
				Inuse:  counts.Inuse,
				Unused: counts.Unused,
				Total:  counts.Total,
			},
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

// GetVolume returns a volume by name.
func (h *VolumeHandler) GetVolume(ctx context.Context, input *GetVolumeInput) (*GetVolumeOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	vol, err := h.volumeService.GetVolumeByName(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.Error404NotFound((&common.VolumeNotFoundError{Err: err}).Error())
	}

	return &GetVolumeOutput{
		Body: base.ApiResponse[*volumetypes.Volume]{
			Success: true,
			Data:    vol,
		},
	}, nil
}

// CreateVolume creates a new Docker volume.
func (h *VolumeHandler) CreateVolume(ctx context.Context, input *CreateVolumeInput) (*CreateVolumeOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	options := volume.CreateOptions{
		Name:       input.Body.Name,
		Driver:     input.Body.Driver,
		Labels:     input.Body.Labels,
		DriverOpts: input.Body.DriverOpts,
	}

	response, err := h.volumeService.CreateVolume(ctx, options, *user)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeCreationError{Err: err}).Error())
	}

	return &CreateVolumeOutput{
		Body: base.ApiResponse[*volumetypes.Volume]{
			Success: true,
			Data:    response,
		},
	}, nil
}

// RemoveVolume removes a Docker volume.
func (h *VolumeHandler) RemoveVolume(ctx context.Context, input *RemoveVolumeInput) (*RemoveVolumeOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	if err := h.volumeService.DeleteVolume(ctx, input.VolumeName, input.Force, *user); err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeDeletionError{Err: err}).Error())
	}

	return &RemoveVolumeOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Volume removed successfully",
			},
		},
	}, nil
}

// PruneVolumes removes all unused Docker volumes.
func (h *VolumeHandler) PruneVolumes(ctx context.Context, input *PruneVolumesInput) (*PruneVolumesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	report, err := h.volumeService.PruneVolumes(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumePruneError{Err: err}).Error())
	}

	return &PruneVolumesOutput{
		Body: base.ApiResponse[VolumePruneReportData]{
			Success: true,
			Data: VolumePruneReportData{
				VolumesDeleted: report.VolumesDeleted,
				SpaceReclaimed: report.SpaceReclaimed,
			},
		},
	}, nil
}

// GetVolumeUsage returns containers using a specific volume.
func (h *VolumeHandler) GetVolumeUsage(ctx context.Context, input *GetVolumeUsageInput) (*GetVolumeUsageOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	inUse, containers, err := h.volumeService.GetVolumeUsage(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeUsageError{Err: err}).Error())
	}

	return &GetVolumeUsageOutput{
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
func (h *VolumeHandler) GetVolumeUsageCounts(ctx context.Context, input *GetVolumeUsageCountsInput) (*GetVolumeUsageCountsOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	_, _, counts, err := h.volumeService.ListVolumesPaginated(ctx, pagination.QueryParams{}, input.IncludeInternal)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeCountsError{Err: err}).Error())
	}

	return &GetVolumeUsageCountsOutput{
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
func (h *VolumeHandler) GetVolumeSizes(ctx context.Context, input *GetVolumeSizesInput) (*GetVolumeSizesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

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

	return &GetVolumeSizesOutput{
		Body: base.ApiResponse[[]VolumeSizeInfo]{
			Success: true,
			Data:    result,
		},
	}, nil
}

// --- Volume Browser Handler Methods ---

func (h *VolumeHandler) BrowseDirectory(ctx context.Context, input *BrowseDirectoryInput) (*BrowseDirectoryOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	entries, err := h.volumeService.ListDirectory(ctx, input.VolumeName, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &BrowseDirectoryOutput{
		Body: base.ApiResponse[[]volumetypes.FileEntry]{
			Success: true,
			Data:    entries,
		},
	}, nil
}

func (h *VolumeHandler) GetFileContent(ctx context.Context, input *GetFileContentInput) (*GetFileContentOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	content, mimeType, err := h.volumeService.GetFileContent(ctx, input.VolumeName, input.Path, input.MaxBytes)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetFileContentOutput{
		Body: base.ApiResponse[FileContentResponse]{
			Success: true,
			Data: FileContentResponse{
				Content:  content,
				MimeType: mimeType,
			},
		},
	}, nil
}

func (h *VolumeHandler) DownloadFile(ctx context.Context, input *DownloadFileInput) (*DownloadFileOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	reader, size, err := h.volumeService.DownloadFile(ctx, input.VolumeName, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &DownloadFileOutput{
		ContentType:        "application/octet-stream",
		ContentDisposition: "attachment; filename=" + path.Base(input.Path),
		ContentLength:      size,
		Body:               reader,
	}, nil
}

func (h *VolumeHandler) UploadFile(ctx context.Context, input *UploadFileInput) (*base.ApiResponse[base.MessageResponse], error) {
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
	err = h.volumeService.UploadFile(ctx, input.VolumeName, input.Path, file, fileHeader.Filename, user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "File uploaded successfully"},
	}, nil
}

func (h *VolumeHandler) CreateDirectory(ctx context.Context, input *CreateDirectoryInput) (*base.ApiResponse[base.MessageResponse], error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, _ := humamw.GetCurrentUserFromContext(ctx)
	err := h.volumeService.CreateDirectory(ctx, input.VolumeName, input.Path, user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "Directory created successfully"},
	}, nil
}

func (h *VolumeHandler) DeleteFile(ctx context.Context, input *DeleteFileInput) (*base.ApiResponse[base.MessageResponse], error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, _ := humamw.GetCurrentUserFromContext(ctx)
	err := h.volumeService.DeleteFile(ctx, input.VolumeName, input.Path, user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "Deleted successfully"},
	}, nil
}

// --- Volume Backup Handler Methods ---

func (h *VolumeHandler) ListBackups(ctx context.Context, input *ListBackupsInput) (*ListBackupsOutput, error) {
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
		PaginationParams: pagination.PaginationParams{
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

	return &ListBackupsOutput{
		Body: VolumeBackupPaginatedResponse{
			Success: true,
			Data:    backups,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
			Warnings: func() []string {
				if warning == "" {
					return nil
				}
				return []string{warning}
			}(),
		},
	}, nil
}

func (h *VolumeHandler) CreateBackup(ctx context.Context, input *CreateBackupInput) (*CreateBackupOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	backup, err := h.volumeService.CreateBackup(ctx, input.VolumeName, *user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &CreateBackupOutput{
		Body: base.ApiResponse[*models.VolumeBackup]{
			Success: true,
			Data:    backup,
		},
	}, nil
}

func (h *VolumeHandler) RestoreBackup(ctx context.Context, input *RestoreBackupInput) (*RestoreBackupOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	err := h.volumeService.RestoreBackup(ctx, input.VolumeName, input.BackupID, *user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &RestoreBackupOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Restore initiated successfully"},
		},
	}, nil
}

func (h *VolumeHandler) RestoreBackupFiles(ctx context.Context, input *RestoreBackupFilesInput) (*RestoreBackupFilesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	if len(input.Body.Paths) == 0 {
		return nil, huma.Error400BadRequest("paths are required")
	}

	if err := h.volumeService.RestoreBackupFiles(ctx, input.VolumeName, input.BackupID, input.Body.Paths, *user); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &RestoreBackupFilesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Restore initiated successfully"},
		},
	}, nil
}

func (h *VolumeHandler) BackupHasPath(ctx context.Context, input *BackupHasPathInput) (*BackupHasPathOutput, error) {
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

	return &BackupHasPathOutput{
		Body: base.ApiResponse[BackupHasPathResponse]{
			Success: true,
			Data:    BackupHasPathResponse{Exists: exists},
		},
	}, nil
}

func (h *VolumeHandler) ListBackupFiles(ctx context.Context, input *ListBackupFilesInput) (*ListBackupFilesOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	files, err := h.volumeService.ListBackupFiles(ctx, input.BackupID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &ListBackupFilesOutput{
		Body: base.ApiResponse[[]string]{
			Success: true,
			Data:    files,
		},
	}, nil
}

func (h *VolumeHandler) DeleteBackup(ctx context.Context, input *DeleteBackupInput) (*DeleteBackupOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, _ := humamw.GetCurrentUserFromContext(ctx)
	err := h.volumeService.DeleteBackup(ctx, input.BackupID, user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &DeleteBackupOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Backup deleted successfully"},
		},
	}, nil
}

func (h *VolumeHandler) DownloadBackup(ctx context.Context, input *DownloadBackupInput) (*DownloadBackupOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, _ := humamw.GetCurrentUserFromContext(ctx)
	reader, size, err := h.volumeService.DownloadBackup(ctx, input.BackupID, user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &DownloadBackupOutput{
		ContentType:        "application/x-gzip",
		ContentDisposition: "attachment; filename=" + input.BackupID + ".tar.gz",
		ContentLength:      size,
		Body:               reader,
	}, nil
}

func (h *VolumeHandler) UploadAndRestore(ctx context.Context, input *UploadAndRestoreInput) (*UploadAndRestoreOutput, error) {
	if h.volumeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
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

	err = h.volumeService.UploadAndRestore(ctx, input.VolumeName, file, fileHeader.Filename, *user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &UploadAndRestoreOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Backup uploaded and restored successfully"},
		},
	}, nil
}
