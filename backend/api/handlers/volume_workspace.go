package handlers

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
	"github.com/getarcaneapp/arcane/types/v2/base"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/samber/mo"
)

type GetVolumeWorkspaceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type GetVolumeWorkspaceOutput struct {
	Body base.ApiResponse[workspacetypes.Workspace]
}

type GetVolumeWorkspaceFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	RelativePath  string `query:"relativePath" doc:"Path relative to the volume workspace root"`
}

type GetVolumeWorkspaceFileOutput struct {
	Body base.ApiResponse[workspacetypes.FileContent]
}

type UpdateVolumeWorkspaceInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	VolumeName    string         `path:"volumeName" doc:"Volume name"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

type UpdateVolumeWorkspaceOutput struct {
	Body base.ApiResponse[workspacetypes.Workspace]
}

func registerVolumeWorkspaceRoutesInternal(api huma.API, h *VolumeHandler) {
	basePath := "/environments/{id}/volumes/{volumeName}/workspace"
	tag := "Volume Workspace"
	registerSecuredInternal(api, operationInternal("get-volume-workspace", http.MethodGet, basePath, "Get volume workspace", "", tag), authz.PermVolumesRead, h.GetVolumeWorkspace)
	registerSecuredInternal(api, operationInternal("get-volume-workspace-file", http.MethodGet, basePath+"/file", "Get volume workspace file", "", tag), authz.PermVolumesRead, h.GetVolumeWorkspaceFile)
	registerSecuredInternal(api, operationInternal("download-volume-workspace-file", http.MethodGet, basePath+"/file/download", "Download volume workspace file", "", tag), authz.PermVolumesRead, h.DownloadVolumeWorkspaceFile)
	updateOperation := operationInternal("update-volume-workspace", http.MethodPut, basePath, "Update volume workspace", "", tag)
	updateOperation.RequestBody = workspaceMultipartRequestBodyInternal("JSON encoded volume workspace manifest")
	registerSecuredInternal(api, updateOperation, authz.PermVolumesRead, h.UpdateVolumeWorkspace)
}

func volumeWorkspaceHTTPErrorInternal(err error) error {
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

func (h *VolumeHandler) GetVolumeWorkspace(ctx context.Context, input *GetVolumeWorkspaceInput) (*GetVolumeWorkspaceOutput, error) {
	result, err := h.volumeService.GetVolumeWorkspace(ctx, input.VolumeName)
	if err != nil {
		return nil, volumeWorkspaceHTTPErrorInternal(err)
	}
	return &GetVolumeWorkspaceOutput{Body: base.ApiResponse[workspacetypes.Workspace]{Success: true, Data: *result}}, nil
}

func (h *VolumeHandler) GetVolumeWorkspaceFile(ctx context.Context, input *GetVolumeWorkspaceFileInput) (*GetVolumeWorkspaceFileOutput, error) {
	result, err := h.volumeService.GetVolumeWorkspaceFile(ctx, input.VolumeName, input.RelativePath)
	if err != nil {
		return nil, volumeWorkspaceHTTPErrorInternal(err)
	}
	return &GetVolumeWorkspaceFileOutput{Body: base.ApiResponse[workspacetypes.FileContent]{Success: true, Data: *result}}, nil
}

func (h *VolumeHandler) DownloadVolumeWorkspaceFile(ctx context.Context, input *GetVolumeWorkspaceFileInput) (*huma.StreamResponse, error) {
	reader, size, err := h.volumeService.DownloadVolumeWorkspaceFile(ctx, input.VolumeName, input.RelativePath)
	if err != nil {
		return nil, volumeWorkspaceHTTPErrorInternal(err)
	}
	return &huma.StreamResponse{Body: func(humaCtx huma.Context) {
		defer func() { _ = reader.Close() }()
		humaCtx.SetHeader("Content-Type", "application/octet-stream")
		humaCtx.SetHeader("Content-Disposition", "attachment; filename="+path.Base(input.RelativePath))
		humaCtx.SetHeader("Content-Length", strconv.FormatInt(size, 10))
		_, _ = io.Copy(humaCtx.BodyWriter(), reader)
	}}, nil
}

func requireVolumeWorkspacePermissionsInternal(ctx context.Context, environmentID string, changes []volumetypes.WorkspaceFileChange) error {
	permissions, ok := humamw.PermissionsFromContext(ctx)
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

func (h *VolumeHandler) UpdateVolumeWorkspace(ctx context.Context, input *UpdateVolumeWorkspaceInput) (*UpdateVolumeWorkspaceOutput, error) {
	manifest, err := parseWorkspaceJSONPartInternal[volumetypes.WorkspaceUpdateManifest](input.RawBody, "manifest")
	if err != nil {
		return nil, err
	}
	if err := requireVolumeWorkspacePermissionsInternal(ctx, input.EnvironmentID, manifest.FileChanges); err != nil {
		return nil, err
	}
	maxFileSizeMB := workspacepkg.DefaultMaxFileSizeMB
	if h.cfg != nil {
		maxFileSizeMB = h.cfg.VolumeWorkspaceMaxFileSizeMB
	}
	uploads, err := readWorkspaceUploadsInternal(input.RawBody, workspacepkg.MaxFileSizeBytes(maxFileSizeMB))
	if err != nil {
		return nil, err
	}
	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}
	var result *workspacetypes.Workspace
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID: input.EnvironmentID, Type: models.ActivityTypeResourceAction, ResourceType: "volume", ResourceID: input.VolumeName, ResourceName: input.VolumeName, User: user,
		Step: "Updating volume workspace", Message: "Updating volume workspace", SuccessMessage: "Volume workspace updated successfully",
		Metadata: models.JSON{"action": "update_volume_workspace", "fileChangeCount": len(manifest.FileChanges)},
	}, func(runtimeCtx context.Context) error {
		var updateErr error
		result, updateErr = h.volumeService.UpdateVolumeWorkspace(runtimeCtx, input.VolumeName, manifest, uploads, *user)
		return updateErr
	})
	if err != nil {
		return nil, volumeWorkspaceHTTPErrorInternal(err)
	}
	result.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	return &UpdateVolumeWorkspaceOutput{Body: base.ApiResponse[workspacetypes.Workspace]{Success: true, Data: *result}}, nil
}
