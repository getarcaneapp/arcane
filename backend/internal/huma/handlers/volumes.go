package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/docker/docker/api/types/volume"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
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
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	InUse         string `query:"inUse" doc:"Filter by in-use status (true/false)"`
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
	EnvironmentID string `path:"id" doc:"Environment ID"`
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

	volumes, paginationResp, counts, err := h.volumeService.ListVolumesPaginated(ctx, params)
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
	if h.dockerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	_, inuse, unused, total, err := h.dockerService.GetAllVolumes(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VolumeCountsError{Err: err}).Error())
	}

	return &GetVolumeUsageCountsOutput{
		Body: base.ApiResponse[VolumeUsageCountsData]{
			Success: true,
			Data: VolumeUsageCountsData{
				Inuse:  inuse,
				Unused: unused,
				Total:  total,
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
