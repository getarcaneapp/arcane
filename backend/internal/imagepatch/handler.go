package imagepatch

import (
	"context"
	"net/http"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/imagepatch"
)

// ImagePatchHandler provides Huma-based image patching endpoints.
type ImagePatchHandler struct {
	imagePatchService *ImagePatchService
	appCtx            context.Context
}

type PatchImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID to patch"`
	Body          imagepatch.PatchOptions
}

type PatchImageOutput struct {
	Body base.ApiResponse[imagepatch.PatchRecord]
}

type ListPatchTargetsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Start         int    `query:"start" doc:"Start offset"`
	Limit         int    `query:"limit" doc:"Limit"`
}

type ListPatchTargetsOutput struct {
	Body base.Paginated[imagepatch.PatchTarget]
}

type ListImagePatchesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Sort field"`
	Order         string `query:"order" doc:"Sort order"`
	Start         int    `query:"start" doc:"Start offset"`
	Limit         int    `query:"limit" doc:"Limit"`
	Status        string `query:"status" doc:"Filter by patch status"`
}

type ListImagePatchesOutput struct {
	Body base.Paginated[imagepatch.PatchRecord]
}

// RegisterImagePatches registers image patching routes using Huma.
func RegisterImagePatches(api huma.API, imagePatchService *ImagePatchService, appCtx handlerutil.ActivityAppContext) {
	h := &ImagePatchHandler{
		imagePatchService: imagePatchService,
		appCtx:            appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-image-patches",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/patches",
		Summary:     "List image patches",
		Description: "Retrieves the paginated image patch history for the environment",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesList, h.ListImagePatches)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-image-patch-targets",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/patch-targets",
		Summary:     "List image patch targets",
		Description: "Retrieves scanned images with fixable vulnerability counts and their latest patch run",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVulnsRead, h.ListPatchTargets)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "patch-image",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/{imageId}/patch",
		Summary:     "Patch image",
		Description: "Patches OS package vulnerabilities in the image using Copacetic, producing a new patched tag",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesPatch, h.PatchImage)
}

// PatchImage starts a background patch run for an image.
func (h *ImagePatchHandler) PatchImage(ctx context.Context, input *PatchImageInput) (*PatchImageOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	record, err := h.imagePatchService.PatchImage(runtimeCtx, input.EnvironmentID, input.ImageID, input.Body, *user)
	if err != nil {
		switch {
		case errors.Is(err, common.ErrBadRequest):
			return nil, huma.Error400BadRequest(err.Error())
		case errors.Is(err, common.ErrNotFound):
			return nil, huma.Error404NotFound(err.Error())
		default:
			return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to patch image").Error())
		}
	}

	return &PatchImageOutput{
		Body: base.ApiResponse[imagepatch.PatchRecord]{
			Success: true,
			Data:    *record,
		},
	}, nil
}

// ListPatchTargets returns scanned images with fixable counts and latest patch runs.
func (h *ImagePatchHandler) ListPatchTargets(ctx context.Context, input *ListPatchTargetsInput) (*ListPatchTargetsOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, "", "", input.Search)

	targets, paginationResp, err := h.imagePatchService.ListPatchTargets(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list image patch targets").Error())
	}
	if targets == nil {
		targets = []imagepatch.PatchTarget{}
	}

	return &ListPatchTargetsOutput{
		Body: base.Paginated[imagepatch.PatchTarget]{
			Success:    true,
			Data:       targets,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// ListImagePatches returns the paginated patch history for the environment.
func (h *ImagePatchHandler) ListImagePatches(ctx context.Context, input *ListImagePatchesInput) (*ListImagePatchesOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Status != "" {
		params.Filters["status"] = input.Status
	}

	records, paginationResp, err := h.imagePatchService.ListPatches(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list image patches").Error())
	}
	if records == nil {
		records = []imagepatch.PatchRecord{}
	}

	return &ListImagePatchesOutput{
		Body: base.Paginated[imagepatch.PatchRecord]{
			Success:    true,
			Data:       records,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}
