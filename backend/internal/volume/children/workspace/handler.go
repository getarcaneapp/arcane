package workspace

import (
	"context"
	"errors"
	"mime/multipart"
	"path"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
)

type Handler struct {
	service         *Service
	activityService *activity.ActivityService
	appCtx          context.Context
}

type GetVolumeWorkspaceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type GetVolumeWorkspaceFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	RelativePath  string `query:"relativePath" doc:"Path relative to the volume workspace root"`
}

type UpdateVolumeWorkspaceInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	VolumeName    string         `path:"volumeName" doc:"Volume name"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

func NewHandler(service *Service, activityService *activity.ActivityService, appCtx context.Context) *Handler {
	return &Handler{service: service, activityService: activityService, appCtx: appCtx}
}

func volumeWorkspaceHTTPError(err error) error {
	switch {
	case errors.Is(err, common.ErrVolumeWorkspaceConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, common.ErrVolumeWorkspaceForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, common.ErrVolumeWorkspaceNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, common.ErrVolumeWorkspaceBadRequest):
		return huma.Error400BadRequest(err.Error())
	default:
		return huma.Error500InternalServerError("internal error")
	}
}

func (h *Handler) GetVolumeWorkspace(ctx context.Context, input *GetVolumeWorkspaceInput) (*handlerutil.Out[types.Workspace], error) {
	result, err := h.service.GetVolumeWorkspace(ctx, input.VolumeName)
	if err != nil {
		return nil, volumeWorkspaceHTTPError(err)
	}
	return &handlerutil.Out[types.Workspace]{Body: types.ApiResponse[types.Workspace]{Success: true, Data: *result}}, nil
}

func (h *Handler) GetVolumeWorkspaceFile(ctx context.Context, input *GetVolumeWorkspaceFileInput) (*handlerutil.Out[types.WorkspaceFileContent], error) {
	result, err := h.service.GetVolumeWorkspaceFile(ctx, input.VolumeName, input.RelativePath)
	if err != nil {
		return nil, volumeWorkspaceHTTPError(err)
	}
	return &handlerutil.Out[types.WorkspaceFileContent]{Body: types.ApiResponse[types.WorkspaceFileContent]{Success: true, Data: *result}}, nil
}

func (h *Handler) DownloadVolumeWorkspaceFile(ctx context.Context, input *GetVolumeWorkspaceFileInput) (*huma.StreamResponse, error) {
	reader, size, err := h.service.DownloadVolumeWorkspaceFile(ctx, input.VolumeName, input.RelativePath)
	if err != nil {
		return nil, volumeWorkspaceHTTPError(err)
	}
	return handlerutil.DownloadResponse(reader, size, path.Base(input.RelativePath)), nil
}

func requireVolumeWorkspacePermissions(ctx context.Context, environmentID string, changes []types.VolumeWorkspaceFileChange) error {
	permissions, ok := middleware.PermissionsFromContext(ctx)
	if !ok || permissions == nil {
		return huma.Error403Forbidden("insufficient permissions")
	}
	required, _ := authz.VolumeWorkspaceRequiredPermissions(changes)
	for _, permission := range required {
		if !permissions.Allows(permission, environmentID) {
			return huma.Error403Forbidden("insufficient permissions for volume workspace operation")
		}
	}
	return nil
}

func (h *Handler) UpdateVolumeWorkspace(ctx context.Context, input *UpdateVolumeWorkspaceInput) (*handlerutil.Out[types.Workspace], error) {
	manifest, err := handlerutil.ParseMultipartJSONPart[types.VolumeWorkspaceUpdateManifest](input.RawBody, "manifest")
	if err != nil {
		return nil, err
	}
	if requireVolumeWorkspacePermissionsErr := requireVolumeWorkspacePermissions(ctx, input.EnvironmentID, manifest.FileChanges); requireVolumeWorkspacePermissionsErr != nil {
		return nil, requireVolumeWorkspacePermissionsErr
	}
	maxFileSizeBytes := workspacepkg.MaxFileSizeBytes(workspacepkg.DefaultMaxFileSizeMB)
	if h.service != nil {
		maxFileSizeBytes = h.service.MaxFileSizeBytes()
	}
	uploads, err := handlerutil.ReadWorkspaceUploads(input.RawBody, maxFileSizeBytes)
	if err != nil {
		return nil, err
	}
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var result *types.Workspace
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID: input.EnvironmentID, Type: types.ActivityTypeResourceAction, ResourceType: "volume", ResourceID: input.VolumeName, ResourceName: input.VolumeName, User: user,
		Step: "Updating volume workspace", Message: "Updating volume workspace", SuccessMessage: "Volume workspace updated successfully",
		Metadata: database.JSON{"action": "update_volume_workspace", "fileChangeCount": len(manifest.FileChanges)},
	}, func(runtimeCtx context.Context) error {
		var updateErr error
		result, updateErr = h.service.UpdateVolumeWorkspace(runtimeCtx, input.VolumeName, manifest, uploads, *user)
		return updateErr
	})
	if err != nil {
		return nil, volumeWorkspaceHTTPError(err)
	}
	result.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	return &handlerutil.Out[types.Workspace]{Body: types.ApiResponse[types.Workspace]{Success: true, Data: *result}}, nil
}
