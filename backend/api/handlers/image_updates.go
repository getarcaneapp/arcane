package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/getarcaneapp/arcane/types/base"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/getarcaneapp/arcane/types/imageupdate"
)

type ImageUpdateHandler struct {
	imageUpdateService *services.ImageUpdateService
	imageService       *services.ImageService
	appCtx             context.Context
}

type CheckImageUpdateInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageRef      string `query:"imageRef" doc:"Image reference"`
}

type CheckImageUpdateOutput struct {
	Body base.ApiResponse[imageupdate.Response]
}

type CheckImageUpdateByIDInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID"`
}

type CheckImageUpdateByIDOutput struct {
	Body base.ApiResponse[imageupdate.Response]
}

type CheckMultipleImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          imageupdate.BatchImageUpdateRequest
}

type CheckMultipleImagesOutput struct {
	Body base.ApiResponse[imageupdate.BatchResponse]
}

type CheckAllImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          imageupdate.CheckAllImagesRequest
}

type CheckAllImagesOutput struct {
	Body base.ApiResponse[imageupdate.BatchResponse]
}

type GetUpdateInfoByRefsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageRefs     string `query:"imageRefs" doc:"Comma-separated image references"`
}

type GetUpdateInfoByRefsOutput struct {
	Body base.ApiResponse[map[string]*imagetypes.UpdateInfo]
}

type GetUpdateSummaryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetUpdateSummaryOutput struct {
	Body base.ApiResponse[imageupdate.Summary]
}

// RegisterImageUpdates registers image update endpoints.
func RegisterImageUpdates(api huma.API, imageUpdateSvc *services.ImageUpdateService, imageSvc *services.ImageService, appCtx ActivityAppContext) {
	h := &ImageUpdateHandler{
		imageUpdateService: imageUpdateSvc,
		imageService:       imageSvc,
		appCtx:             appCtx.contextInternal(),
	}

	huma.Register(api, huma.Operation{
		OperationID: "check-image-update",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/check",
		Summary:     "Check image update by reference",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermImageUpdatesCheck),
	}, h.CheckImageUpdate)

	huma.Register(api, huma.Operation{
		OperationID: "check-image-update-by-id",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/check/{imageId}",
		Summary:     "Check image update by ID",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermImageUpdatesCheck),
	}, h.CheckImageUpdateByID)

	huma.Register(api, huma.Operation{
		OperationID: "check-image-update-by-id-post",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check/{imageId}",
		Summary:     "Check image update by ID (POST)",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermImageUpdatesCheck),
	}, h.CheckImageUpdateByID)

	huma.Register(api, huma.Operation{
		OperationID: "check-multiple-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check-batch",
		Summary:     "Check multiple images",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermImageUpdatesCheck),
	}, h.CheckMultipleImages)

	huma.Register(api, huma.Operation{
		OperationID: "check-all-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check-all",
		Summary:     "Check all images",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermImageUpdatesCheck),
	}, h.CheckAllImages)

	huma.Register(api, huma.Operation{
		OperationID: "get-update-info-by-refs",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/by-refs",
		Summary:     "Get persisted update info for image references",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermImageUpdatesRead),
	}, h.GetUpdateInfoByRefs)

	huma.Register(api, huma.Operation{
		OperationID: "get-update-summary",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/summary",
		Summary:     "Get update summary",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermImageUpdatesRead),
	}, h.GetUpdateSummary)
}

func (h *ImageUpdateHandler) CheckImageUpdate(ctx context.Context, input *CheckImageUpdateInput) (*CheckImageUpdateOutput, error) {
	if input.ImageRef == "" {
		return nil, huma.Error400BadRequest((&common.ImageRefRequiredError{}).Error())
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.imageUpdateService.CheckImageUpdate(runtimeCtx, input.ImageRef)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ImageUpdateCheckError{Err: err}).Error())
	}

	return &CheckImageUpdateOutput{
		Body: base.ApiResponse[imageupdate.Response]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

func (h *ImageUpdateHandler) CheckImageUpdateByID(ctx context.Context, input *CheckImageUpdateByIDInput) (*CheckImageUpdateByIDOutput, error) {
	if input.ImageID == "" {
		return nil, huma.Error400BadRequest((&common.ImageIDRequiredError{}).Error())
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.imageUpdateService.CheckImageUpdateByID(runtimeCtx, input.ImageID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ImageUpdateCheckError{Err: err}).Error())
	}

	return &CheckImageUpdateByIDOutput{
		Body: base.ApiResponse[imageupdate.Response]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

func (h *ImageUpdateHandler) CheckMultipleImages(ctx context.Context, input *CheckMultipleImagesInput) (*CheckMultipleImagesOutput, error) {
	// Empty batch is valid - return empty results
	if len(input.Body.ImageRefs) == 0 {
		return &CheckMultipleImagesOutput{
			Body: base.ApiResponse[imageupdate.BatchResponse]{
				Success: true,
				Data:    imageupdate.BatchResponse{},
			},
		}, nil
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	results, err := h.imageUpdateService.CheckMultipleImages(runtimeCtx, input.Body.ImageRefs, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.BatchImageUpdateCheckError{Err: err}).Error())
	}

	return &CheckMultipleImagesOutput{
		Body: base.ApiResponse[imageupdate.BatchResponse]{
			Success: true,
			Data:    imageupdate.BatchResponse(results),
		},
	}, nil
}

func (h *ImageUpdateHandler) CheckAllImages(ctx context.Context, input *CheckAllImagesInput) (*CheckAllImagesOutput, error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	results, err := h.imageUpdateService.CheckAllImages(runtimeCtx, 0, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.AllImageUpdateCheckError{Err: err}).Error())
	}

	return &CheckAllImagesOutput{
		Body: base.ApiResponse[imageupdate.BatchResponse]{
			Success: true,
			Data:    imageupdate.BatchResponse(results),
		},
	}, nil
}

func (h *ImageUpdateHandler) GetUpdateInfoByRefs(ctx context.Context, input *GetUpdateInfoByRefsInput) (*GetUpdateInfoByRefsOutput, error) {
	imageRefs := parseImageRefsQueryInternal(input.ImageRefs)
	if len(imageRefs) == 0 {
		return &GetUpdateInfoByRefsOutput{
			Body: base.ApiResponse[map[string]*imagetypes.UpdateInfo]{
				Success: true,
				Data:    map[string]*imagetypes.UpdateInfo{},
			},
		}, nil
	}

	result, err := h.imageService.GetUpdateInfoByImageRefs(ctx, imageRefs)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.BatchImageUpdateCheckError{Err: err}).Error())
	}

	return &GetUpdateInfoByRefsOutput{
		Body: base.ApiResponse[map[string]*imagetypes.UpdateInfo]{
			Success: true,
			Data:    result,
		},
	}, nil
}

func (h *ImageUpdateHandler) GetUpdateSummary(ctx context.Context, input *GetUpdateSummaryInput) (*GetUpdateSummaryOutput, error) {
	summary, err := h.imageUpdateService.GetUpdateSummary(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UpdateSummaryError{Err: err}).Error())
	}

	return &GetUpdateSummaryOutput{
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
