package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/base"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
)

type imageUpdateHandler struct {
	imageUpdateService *services.ImageUpdateService
	imageService       *services.ImageService
	appCtx             context.Context
}

type checkImageUpdateInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageRef      string `query:"imageRef" doc:"Image reference"`
}

type checkImageUpdateOutput struct {
	Body base.ApiResponse[imageupdate.Response]
}

type checkImageUpdateByIDInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID"`
}

type checkImageUpdateByIDOutput struct {
	Body base.ApiResponse[imageupdate.Response]
}

type checkMultipleImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          imageupdate.BatchImageUpdateRequest
}

type checkMultipleImagesOutput struct {
	Body base.ApiResponse[imageupdate.BatchResponse]
}

type checkAllImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          imageupdate.CheckAllImagesRequest
}

type checkAllImagesOutput struct {
	Body base.ApiResponse[imageupdate.BatchResponse]
}

type getUpdateInfoByRefsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageRefs     string `query:"imageRefs" doc:"Comma-separated image references"`
}

type getUpdateInfoByRefsOutput struct {
	Body base.ApiResponse[map[string]*imagetypes.UpdateInfo]
}

type getUpdateSummaryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type getUpdateSummaryOutput struct {
	Body base.ApiResponse[imageupdate.Summary]
}

// RegisterImageUpdates registers image update endpoints.
func RegisterImageUpdates(api huma.API, imageUpdateSvc *services.ImageUpdateService, imageSvc *services.ImageService, appCtx ActivityAppContext) {
	h := &imageUpdateHandler{
		imageUpdateService: imageUpdateSvc,
		imageService:       imageSvc,
		appCtx:             appCtx.contextInternal(),
	}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/check",
		Summary:     "Check image update by reference",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermImageUpdatesCheck, h.checkImageUpdateInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update-by-id",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/check/{imageId}",
		Summary:     "Check image update by ID",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermImageUpdatesCheck, h.checkImageUpdateByIDInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update-by-id-post",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check/{imageId}",
		Summary:     "Check image update by ID (POST)",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermImageUpdatesCheck, h.checkImageUpdateByIDInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-multiple-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check-batch",
		Summary:     "Check multiple images",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermImageUpdatesCheck, h.checkMultipleImagesInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-all-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check-all",
		Summary:     "Check all images",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermImageUpdatesCheck, h.checkAllImagesInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-update-info-by-refs",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/by-refs",
		Summary:     "Get persisted update info for image references",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermImageUpdatesRead, h.getUpdateInfoByRefsInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-update-summary",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/summary",
		Summary:     "Get update summary",
		Tags:        []string{"Image Updates"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermImageUpdatesRead, h.getUpdateSummaryInternal)
}

func (h *imageUpdateHandler) checkImageUpdateInternal(ctx context.Context, input *checkImageUpdateInput) (*checkImageUpdateOutput, error) {
	if input.ImageRef == "" {
		return nil, huma.Error400BadRequest((&common.ImageRefRequiredError{}).Error())
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.imageUpdateService.CheckImageUpdate(runtimeCtx, input.ImageRef)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ImageUpdateCheckError{Err: err}).Error())
	}

	return &checkImageUpdateOutput{
		Body: base.ApiResponse[imageupdate.Response]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

func (h *imageUpdateHandler) checkImageUpdateByIDInternal(ctx context.Context, input *checkImageUpdateByIDInput) (*checkImageUpdateByIDOutput, error) {
	if input.ImageID == "" {
		return nil, huma.Error400BadRequest((&common.ImageIDRequiredError{}).Error())
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.imageUpdateService.CheckImageUpdateByID(runtimeCtx, input.ImageID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ImageUpdateCheckError{Err: err}).Error())
	}

	return &checkImageUpdateByIDOutput{
		Body: base.ApiResponse[imageupdate.Response]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

func (h *imageUpdateHandler) checkMultipleImagesInternal(ctx context.Context, input *checkMultipleImagesInput) (*checkMultipleImagesOutput, error) {
	// Empty batch is valid - return empty results
	if len(input.Body.ImageRefs) == 0 {
		return &checkMultipleImagesOutput{
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

	return &checkMultipleImagesOutput{
		Body: base.ApiResponse[imageupdate.BatchResponse]{
			Success: true,
			Data:    results,
		},
	}, nil
}

func (h *imageUpdateHandler) checkAllImagesInternal(ctx context.Context, input *checkAllImagesInput) (*checkAllImagesOutput, error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	results, err := h.imageUpdateService.CheckAllImages(runtimeCtx, 0, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.AllImageUpdateCheckError{Err: err}).Error())
	}

	return &checkAllImagesOutput{
		Body: base.ApiResponse[imageupdate.BatchResponse]{
			Success: true,
			Data:    results,
		},
	}, nil
}

func (h *imageUpdateHandler) getUpdateInfoByRefsInternal(ctx context.Context, input *getUpdateInfoByRefsInput) (*getUpdateInfoByRefsOutput, error) {
	imageRefs := parseImageRefsQueryInternal(input.ImageRefs)
	if len(imageRefs) == 0 {
		return &getUpdateInfoByRefsOutput{
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

	return &getUpdateInfoByRefsOutput{
		Body: base.ApiResponse[map[string]*imagetypes.UpdateInfo]{
			Success: true,
			Data:    result,
		},
	}, nil
}

func (h *imageUpdateHandler) getUpdateSummaryInternal(ctx context.Context, _ *getUpdateSummaryInput) (*getUpdateSummaryOutput, error) {
	summary, err := h.imageUpdateService.GetUpdateSummary(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UpdateSummaryError{Err: err}).Error())
	}

	return &getUpdateSummaryOutput{
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
