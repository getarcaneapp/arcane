package volume

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
	"github.com/samber/mo"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// VolumeHandler provides Huma-based volume management endpoints.
type VolumeHandler struct {
	volumeService      *VolumeService
	dockerService      *docker.DockerClientService
	activityService    *activity.ActivityService
	environmentService *environment.EnvironmentService
	appCtx             context.Context
}

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
	Body base.PaginatedWithCounts[volume.Volume, VolumeUsageCountsData]
}

type GetVolumeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type CreateVolumeInput struct {
	EnvironmentID string        `path:"id" doc:"Environment ID"`
	Body          volume.Create `doc:"Volume creation data"`
}

type RenameVolumeInput struct {
	EnvironmentID string        `path:"id" doc:"Environment ID"`
	VolumeName    string        `path:"volumeName" doc:"Current volume name"`
	Body          volume.Rename `doc:"Volume rename data"`
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

// ListVolumes returns a paginated list of volumes.
func (h *VolumeHandler) ListVolumes(ctx context.Context, input *ListVolumesInput) (*ListVolumesOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.InUse != "" {
		params.Filters["inUse"] = input.InUse
	}

	params.Limit = cmp.Or(params.Limit, 20)

	volumes, paginationResp, counts, err := h.volumeService.ListVolumesPaginated(ctx, params, input.IncludeInternal)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list volumes: " + err.Error())
	}

	if volumes == nil {
		volumes = []volume.Volume{}
	}

	return &ListVolumesOutput{
		Body: base.PaginatedWithCounts[volume.Volume, VolumeUsageCountsData]{
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
func (h *VolumeHandler) GetVolume(ctx context.Context, input *GetVolumeInput) (*handlerutil.Out[*volume.Volume], error) {
	vol, err := h.volumeService.GetVolumeByName(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.Error404NotFound("Volume not found: " + err.Error())
	}

	return &handlerutil.Out[*volume.Volume]{
		Body: base.ApiResponse[*volume.Volume]{
			Success: true,
			Data:    vol,
		},
	}, nil
}

// CreateVolume creates a new Docker volume.
func (h *VolumeHandler) CreateVolume(ctx context.Context, input *CreateVolumeInput) (*handlerutil.Out[*volume.Volume], error) {
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

	var response *volume.Volume
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
		return nil, huma.Error500InternalServerError("Failed to create volume: " + err.Error())
	}
	response.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()

	return &handlerutil.Out[*volume.Volume]{
		Body: base.ApiResponse[*volume.Volume]{
			Success: true,
			Data:    response,
		},
	}, nil
}

// RenameVolume renames an unused Docker volume.
func (h *VolumeHandler) RenameVolume(ctx context.Context, input *RenameVolumeInput) (*handlerutil.Out[*volume.Volume], error) {
	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		remotePath := fmt.Sprintf("/api/environments/0/volumes/%s/rename", url.PathEscape(input.VolumeName))
		response, err := handlerutil.RemoteJSONProxy(h.environmentService.ProxyJSONRequest).JSON[base.ApiResponse[*volume.Volume]](ctx, input.EnvironmentID, http.MethodPost, remotePath, input.Body)
		if err != nil {
			return nil, err
		}
		return &handlerutil.Out[*volume.Volume]{Body: *response}, nil
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	var response *volume.Volume
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
		var conflictErr *volume.ProjectVolumeRenameConflictError
		var inUseErr *volume.ProjectVolumeRenameInUseError
		var spaceErr *volume.ProjectVolumeRenameInsufficientSpaceError
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
			return nil, huma.Error500InternalServerError("Failed to rename volume: " + err.Error())
		}
	}
	response.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()

	return &handlerutil.Out[*volume.Volume]{Body: base.ApiResponse[*volume.Volume]{Success: true, Data: response}}, nil
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
		return nil, huma.Error500InternalServerError("Failed to delete volume: " + err.Error())
	}

	return handlerutil.MessageOutput("Volume removed successfully", activityID), nil
}

// PruneVolumes removes all unused Docker volumes.
func (h *VolumeHandler) PruneVolumes(ctx context.Context, input *PruneVolumesInput) (*handlerutil.Out[VolumePruneReportData], error) {
	var report *volume.PruneReport
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
		return nil, huma.Error500InternalServerError("Failed to prune volumes: " + err.Error())
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
		return nil, huma.Error500InternalServerError("Failed to get volume usage: " + err.Error())
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
		return nil, huma.Error500InternalServerError("Failed to get volume counts: " + err.Error())
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
