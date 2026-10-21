package imageupdate

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type ImageUpdateHandler struct {
	imageUpdateService       *ImageUpdateService
	getUpdateInfoByImageRefs func(context.Context, []string) (map[string]*types.ImageUpdateInfo, error)
	appCtx                   context.Context
}

type CheckImageUpdateInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageRef      string `query:"imageRef" doc:"Image reference"`
}

type CheckImageUpdateByIDInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID"`
}

type CheckMultipleImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          types.BatchImageUpdateRequest
}

type CheckAllImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          types.CheckAllImagesRequest
}

type GetUpdateInfoByRefsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageRefs     string `query:"imageRefs" doc:"Comma-separated image references"`
}

type GetUpdateSummaryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

func (h *ImageUpdateHandler) CheckImageUpdate(ctx context.Context, input *CheckImageUpdateInput) (*handlerutil.Out[types.ImageUpdateResponse], error) {
	if input.ImageRef == "" {
		return nil, huma.Error400BadRequest("imageRef query parameter is required")
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.imageUpdateService.CheckImageUpdate(runtimeCtx, input.ImageRef)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to check image update: " + err.Error())
	}

	return &handlerutil.Out[types.ImageUpdateResponse]{
		Body: types.ApiResponse[types.ImageUpdateResponse]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

func (h *ImageUpdateHandler) CheckImageUpdateByID(ctx context.Context, input *CheckImageUpdateByIDInput) (*handlerutil.Out[types.ImageUpdateResponse], error) {
	if input.ImageID == "" {
		return nil, huma.Error400BadRequest("imageId parameter is required")
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.imageUpdateService.CheckImageUpdateByID(runtimeCtx, input.ImageID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to check image update: " + err.Error())
	}

	return &handlerutil.Out[types.ImageUpdateResponse]{
		Body: types.ApiResponse[types.ImageUpdateResponse]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

func (h *ImageUpdateHandler) CheckMultipleImages(ctx context.Context, input *CheckMultipleImagesInput) (*handlerutil.Out[types.ImageUpdateBatchResponse], error) {
	// Empty batch is valid - return empty results
	if len(input.Body.ImageRefs) == 0 {
		return &handlerutil.Out[types.ImageUpdateBatchResponse]{
			Body: types.ApiResponse[types.ImageUpdateBatchResponse]{
				Success: true,
				Data:    types.ImageUpdateBatchResponse{},
			},
		}, nil
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	results, err := h.imageUpdateService.CheckImages(runtimeCtx, types.ImageUpdateCheckRequest{ImageRefs: input.Body.ImageRefs}, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to check image updates: " + err.Error())
	}

	return &handlerutil.Out[types.ImageUpdateBatchResponse]{
		Body: types.ApiResponse[types.ImageUpdateBatchResponse]{
			Success: true,
			Data:    results,
		},
	}, nil
}

func (h *ImageUpdateHandler) CheckAllImages(ctx context.Context, input *CheckAllImagesInput) (*handlerutil.Out[types.ImageUpdateBatchResponse], error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	results, err := h.imageUpdateService.CheckImages(runtimeCtx, types.ImageUpdateCheckRequest{All: true}, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to check all images: " + err.Error())
	}

	return &handlerutil.Out[types.ImageUpdateBatchResponse]{
		Body: types.ApiResponse[types.ImageUpdateBatchResponse]{
			Success: true,
			Data:    results,
		},
	}, nil
}

func (h *ImageUpdateHandler) GetUpdateInfoByRefs(ctx context.Context, input *GetUpdateInfoByRefsInput) (*handlerutil.Out[map[string]*types.ImageUpdateInfo], error) {
	imageRefs := kit.Unique(kit.TrimNonEmpty(strings.Split(input.ImageRefs, ",")))
	if len(imageRefs) == 0 {
		return &handlerutil.Out[map[string]*types.ImageUpdateInfo]{
			Body: types.ApiResponse[map[string]*types.ImageUpdateInfo]{
				Success: true,
				Data:    map[string]*types.ImageUpdateInfo{},
			},
		}, nil
	}

	result, err := h.getUpdateInfoByImageRefs(ctx, imageRefs)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to check image updates: " + err.Error())
	}

	return &handlerutil.Out[map[string]*types.ImageUpdateInfo]{
		Body: types.ApiResponse[map[string]*types.ImageUpdateInfo]{
			Success: true,
			Data:    result,
		},
	}, nil
}

func (h *ImageUpdateHandler) GetUpdateSummary(ctx context.Context, input *GetUpdateSummaryInput) (*handlerutil.Out[types.ImageUpdateSummary], error) {
	summary, err := h.imageUpdateService.GetUpdateSummary(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get update summary: " + err.Error())
	}

	return &handlerutil.Out[types.ImageUpdateSummary]{
		Body: types.ApiResponse[types.ImageUpdateSummary]{
			Success: true,
			Data:    *summary,
		},
	}, nil
}
