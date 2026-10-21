package patch

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

// ImagePatchHandler provides Huma-based image patching endpoints.
type ImagePatchHandler struct {
	imagePatchService *Service
	appCtx            context.Context
}

type PatchImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID to patch"`
	Body          types.PatchOptions
}

type ListPatchTargetsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"desc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start offset"`
	Limit         int    `query:"limit" default:"20" doc:"Limit"`
}

type ListImagePatchesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Sort field"`
	Order         string `query:"order" doc:"Sort order"`
	Start         int    `query:"start" doc:"Start offset"`
	Limit         int    `query:"limit" doc:"Limit"`
	Status        string `query:"status" doc:"Filter by patch status"`
	IncludeTotal  bool   `query:"includeTotal" doc:"Return exact totalItems and totalPages; otherwise they are -1 and hasNextPage reports whether more rows exist"`
}

// PatchImage starts a background patch run for an image.
func (h *ImagePatchHandler) PatchImage(ctx context.Context, input *PatchImageInput) (*handlerutil.Out[types.PatchRecord], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	record, err := h.imagePatchService.PatchImage(runtimeCtx, input.EnvironmentID, input.ImageID, input.Body, *user)
	if err != nil {
		if errors.Is(err, common.ErrFeatureDisabled) {
			return nil, handlerutil.FeatureDisabledError(types.FeatureVulnerabilityManagement)
		}
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	return &handlerutil.Out[types.PatchRecord]{
		Body: types.ApiResponse[types.PatchRecord]{
			Success: true,
			Data:    *record,
		},
	}, nil
}

// ListPatchTargets returns scanned images with fixable counts and latest patch runs.
func (h *ImagePatchHandler) ListPatchTargets(ctx context.Context, input *ListPatchTargetsInput) (*handlerutil.Page[types.PatchTarget], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.imagePatchService.AggregatePatchTargets(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list image patch targets: " + err.Error())
		}
		return &handlerutil.Page[types.PatchTarget]{Body: body}, nil
	}

	targets, paginationResp, err := h.imagePatchService.ListPatchTargets(ctx, input.EnvironmentID, params)
	if err != nil {
		if errors.Is(err, common.ErrFeatureDisabled) {
			return nil, handlerutil.FeatureDisabledError(types.FeatureVulnerabilityManagement)
		}
		return nil, huma.Error500InternalServerError("Failed to list image patch targets: " + err.Error())
	}
	if targets == nil {
		targets = []types.PatchTarget{}
	}

	return &handlerutil.Page[types.PatchTarget]{
		Body: types.Paginated[types.PatchTarget]{
			Success:    true,
			Data:       targets,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// ListImagePatches returns the paginated patch history for the environment.
func (h *ImagePatchHandler) ListImagePatches(ctx context.Context, input *ListImagePatchesInput) (*handlerutil.Page[types.PatchRecord], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	params.SkipCount = !input.IncludeTotal
	if input.Status != "" {
		params.Filters["status"] = input.Status
	}

	records, paginationResp, err := h.imagePatchService.ListPatches(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list image patches: " + err.Error())
	}
	if records == nil {
		records = []types.PatchRecord{}
	}

	return &handlerutil.Page[types.PatchRecord]{
		Body: types.Paginated[types.PatchRecord]{
			Success:    true,
			Data:       records,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}
