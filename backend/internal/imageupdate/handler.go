package imageupdate

import (
	"context"
	"net/http"
	"strings"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
)

type ImageUpdateHandler struct {
	imageUpdateService       *ImageUpdateService
	getUpdateInfoByImageRefs func(context.Context, []string) (map[string]*imagetypes.UpdateInfo, error)
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
	Body          imageupdate.BatchImageUpdateRequest
}

type CheckAllImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          imageupdate.CheckAllImagesRequest
}

type GetUpdateInfoByRefsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageRefs     string `query:"imageRefs" doc:"Comma-separated image references"`
}

type GetUpdateSummaryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// RegisterImageUpdates registers image update endpoints.
func RegisterImageUpdates(api huma.API, imageUpdateSvc *ImageUpdateService, getUpdateInfoByImageRefs func(context.Context, []string) (map[string]*imagetypes.UpdateInfo, error), appCtx handlerutil.ActivityAppContext) {
	h := &ImageUpdateHandler{
		imageUpdateService:       imageUpdateSvc,
		getUpdateInfoByImageRefs: getUpdateInfoByImageRefs,
		appCtx:                   appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/check",
		Summary:     "Check image update by reference",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckImageUpdate)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update-by-id",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/check/{imageId}",
		Summary:     "Check image update by ID",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckImageUpdateByID)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update-by-id-post",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check/{imageId}",
		Summary:     "Check image update by ID (POST)",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckImageUpdateByID)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-multiple-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check-batch",
		Summary:     "Check multiple images",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckMultipleImages)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-all-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check-all",
		Summary:     "Check all images",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckAllImages)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-update-info-by-refs",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/by-refs",
		Summary:     "Get persisted update info for image references",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetUpdateInfoByRefs)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-update-summary",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/summary",
		Summary:     "Get update summary",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetUpdateSummary)
}

func (h *ImageUpdateHandler) CheckImageUpdate(ctx context.Context, input *CheckImageUpdateInput) (*handlerutil.Out[imageupdate.Response], error) {
	if input.ImageRef == "" {
		return nil, huma.Error400BadRequest("imageRef query parameter is required")
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.imageUpdateService.CheckImageUpdate(runtimeCtx, input.ImageRef)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to check image update").Error())
	}

	return &handlerutil.Out[imageupdate.Response]{
		Body: base.ApiResponse[imageupdate.Response]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

func (h *ImageUpdateHandler) CheckImageUpdateByID(ctx context.Context, input *CheckImageUpdateByIDInput) (*handlerutil.Out[imageupdate.Response], error) {
	if input.ImageID == "" {
		return nil, huma.Error400BadRequest("imageId parameter is required")
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.imageUpdateService.CheckImageUpdateByID(runtimeCtx, input.ImageID)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to check image update").Error())
	}

	return &handlerutil.Out[imageupdate.Response]{
		Body: base.ApiResponse[imageupdate.Response]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

func (h *ImageUpdateHandler) CheckMultipleImages(ctx context.Context, input *CheckMultipleImagesInput) (*handlerutil.Out[imageupdate.BatchResponse], error) {
	// Empty batch is valid - return empty results
	if len(input.Body.ImageRefs) == 0 {
		return &handlerutil.Out[imageupdate.BatchResponse]{
			Body: base.ApiResponse[imageupdate.BatchResponse]{
				Success: true,
				Data:    imageupdate.BatchResponse{},
			},
		}, nil
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	results, err := h.imageUpdateService.CheckMultipleImages(runtimeCtx, input.Body.ImageRefs, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to check image updates").Error())
	}

	return &handlerutil.Out[imageupdate.BatchResponse]{
		Body: base.ApiResponse[imageupdate.BatchResponse]{
			Success: true,
			Data:    results,
		},
	}, nil
}

func (h *ImageUpdateHandler) CheckAllImages(ctx context.Context, input *CheckAllImagesInput) (*handlerutil.Out[imageupdate.BatchResponse], error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	results, err := h.imageUpdateService.CheckAllImages(runtimeCtx, 0, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to check all images").Error())
	}

	return &handlerutil.Out[imageupdate.BatchResponse]{
		Body: base.ApiResponse[imageupdate.BatchResponse]{
			Success: true,
			Data:    results,
		},
	}, nil
}

func (h *ImageUpdateHandler) GetUpdateInfoByRefs(ctx context.Context, input *GetUpdateInfoByRefsInput) (*handlerutil.Out[map[string]*imagetypes.UpdateInfo], error) {
	imageRefs := parseImageRefsQueryInternal(input.ImageRefs)
	if len(imageRefs) == 0 {
		return &handlerutil.Out[map[string]*imagetypes.UpdateInfo]{
			Body: base.ApiResponse[map[string]*imagetypes.UpdateInfo]{
				Success: true,
				Data:    map[string]*imagetypes.UpdateInfo{},
			},
		}, nil
	}

	result, err := h.getUpdateInfoByImageRefs(ctx, imageRefs)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to check image updates").Error())
	}

	return &handlerutil.Out[map[string]*imagetypes.UpdateInfo]{
		Body: base.ApiResponse[map[string]*imagetypes.UpdateInfo]{
			Success: true,
			Data:    result,
		},
	}, nil
}

func (h *ImageUpdateHandler) GetUpdateSummary(ctx context.Context, input *GetUpdateSummaryInput) (*handlerutil.Out[imageupdate.Summary], error) {
	summary, err := h.imageUpdateService.GetUpdateSummary(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get update summary").Error())
	}

	return &handlerutil.Out[imageupdate.Summary]{
		Body: base.ApiResponse[imageupdate.Summary]{
			Success: true,
			Data:    *summary,
		},
	}, nil
}

func parseImageRefsQueryInternal(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		ref := strings.TrimSpace(part)
		if ref == "" {
			continue
		}
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		result = append(result, ref)
	}

	return result
}
