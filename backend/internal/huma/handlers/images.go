package handlers

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/image"
)

// ImageHandler provides Huma-based image management endpoints.
type ImageHandler struct {
	dockerService      *services.DockerClientService
	imageService       *services.ImageService
	imageUpdateService *services.ImageUpdateService
	settingsService    *services.SettingsService
}

// --- Huma Input/Output Wrappers ---

// ImagePaginatedResponse is the paginated response for images.
type ImagePaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []image.Summary         `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	InUse         string `query:"inUse" doc:"Filter by in-use status (true/false)"`
	Updates       string `query:"updates" doc:"Filter by update availability (true/false)"`
}

type ListImagesOutput struct {
	Body ImagePaginatedResponse
}

type GetImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID"`
}

type GetImageOutput struct {
	Body base.ApiResponse[image.DetailSummary]
}

type RemoveImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID"`
	Force         bool   `query:"force" doc:"Force removal"`
}

type RemoveImageOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PullImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          image.PullOptions
}

type PruneImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Dangling      bool   `query:"dangling" doc:"Only remove dangling images"`
	Body          *struct {
		Dangling *bool               `json:"dangling,omitempty"`
		Filters  map[string][]string `json:"filters,omitempty"`
	}
}

type PruneImagesOutput struct {
	Body base.ApiResponse[image.PruneReport]
}

type GetImageUsageCountsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type ImageUsageCountsResponse struct {
	Success bool              `json:"success"`
	Data    image.UsageCounts `json:"data"`
}

type GetImageUsageCountsOutput struct {
	Body ImageUsageCountsResponse
}

type UploadImageInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

type UploadImageOutput struct {
	Body base.ApiResponse[image.LoadResult]
}

// RegisterImages registers image management routes using Huma.
func RegisterImages(api huma.API, dockerService *services.DockerClientService, imageService *services.ImageService, imageUpdateService *services.ImageUpdateService, settingsService *services.SettingsService) {
	h := &ImageHandler{
		dockerService:      dockerService,
		imageService:       imageService,
		imageUpdateService: imageUpdateService,
		settingsService:    settingsService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "list-images",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images",
		Summary:     "List images",
		Description: "Get a paginated list of Docker images",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListImages)

	huma.Register(api, huma.Operation{
		OperationID: "get-image-usage-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/counts",
		Summary:     "Get image usage counts",
		Description: "Get counts of images in use, unused, total, and total size",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetImageUsageCounts)

	huma.Register(api, huma.Operation{
		OperationID: "get-image",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{imageId}",
		Summary:     "Get image by ID",
		Description: "Get a Docker image by its ID",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetImage)

	huma.Register(api, huma.Operation{
		OperationID: "remove-image",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/images/{imageId}",
		Summary:     "Remove an image",
		Description: "Remove a Docker image by ID",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.RemoveImage)

	huma.Register(api, huma.Operation{
		OperationID: "pull-image",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/pull",
		Summary:     "Pull an image",
		Description: "Pull a Docker image from a registry with streaming progress output",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.PullImage)

	huma.Register(api, huma.Operation{
		OperationID: "prune-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/prune",
		Summary:     "Prune unused images",
		Description: "Remove unused Docker images",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.PruneImages)

	huma.Register(api, huma.Operation{
		OperationID: "upload-image",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/upload",
		Summary:     "Upload an image",
		Description: "Upload a Docker image from a tar archive",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		RequestBody: &huma.RequestBody{
			Content: map[string]*huma.MediaType{
				"multipart/form-data": {
					Schema: &huma.Schema{
						Type: "object",
						Properties: map[string]*huma.Schema{
							"file": {
								Type:        "string",
								Format:      "binary",
								Description: "Docker image tar archive",
							},
						},
						Required: []string{"file"},
					},
				},
			},
		},
	}, h.UploadImage)
}

// ListImages returns a paginated list of images.
func (h *ImageHandler) ListImages(ctx context.Context, input *ListImagesInput) (*ListImagesOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	filters := make(map[string]string)
	if input.InUse != "" {
		filters["inUse"] = input.InUse
	}
	if input.Updates != "" {
		filters["updates"] = input.Updates
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

	images, paginationResp, err := h.imageService.ListImagesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ImageListError{Err: err}).Error())
	}

	if images == nil {
		images = []image.Summary{}
	}

	return &ListImagesOutput{
		Body: ImagePaginatedResponse{
			Success: true,
			Data:    images,
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

// GetImage returns an image by ID.
func (h *ImageHandler) GetImage(ctx context.Context, input *GetImageInput) (*GetImageOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	img, err := h.imageService.GetImageByID(ctx, input.ImageID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.ImageNotFoundError{Err: err}).Error())
	}

	out := image.NewDetailSummary(img)

	return &GetImageOutput{
		Body: base.ApiResponse[image.DetailSummary]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// RemoveImage removes a Docker image.
func (h *ImageHandler) RemoveImage(ctx context.Context, input *RemoveImageInput) (*RemoveImageOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	if err := h.imageService.RemoveImage(ctx, input.ImageID, input.Force, *user); err != nil {
		return nil, huma.Error500InternalServerError((&common.ImageRemovalError{Err: err}).Error())
	}

	return &RemoveImageOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Image removed successfully",
			},
		},
	}, nil
}

// PullImage pulls a Docker image with streaming progress.
func (h *ImageHandler) PullImage(ctx context.Context, input *PullImageInput) (*huma.StreamResponse, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.Body.ImageName == "" {
		return nil, huma.Error400BadRequest("image name is required")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	// Get full image name with tag and credentials
	fullImageName := input.Body.GetFullImageName()
	credentials := input.Body.GetCredentials()

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			humaCtx.SetHeader("Content-Type", "application/x-json-stream")
			humaCtx.SetHeader("Cache-Control", "no-cache")
			humaCtx.SetHeader("Connection", "keep-alive")
			humaCtx.SetHeader("X-Accel-Buffering", "no")

			writer := humaCtx.BodyWriter()

			if err := h.imageService.PullImage(humaCtx.Context(), fullImageName, writer, *user, credentials); err != nil {
				_, _ = fmt.Fprintf(writer, `{"error":%q}`+"\n", err.Error())
				return
			}
		},
	}, nil
}

// PruneImages removes unused Docker images.
func (h *ImageHandler) PruneImages(ctx context.Context, input *PruneImagesInput) (*PruneImagesOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	dangling := input.Dangling
	if input.Body != nil {
		if input.Body.Dangling != nil {
			dangling = *input.Body.Dangling
		} else if vals, ok := input.Body.Filters["dangling"]; ok {
			for _, v := range vals {
				if v == "true" || v == "1" {
					dangling = true
					break
				}
			}
		}
	}

	report, err := h.imageService.PruneImages(ctx, dangling)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ImagePruneError{Err: err}).Error())
	}

	out := image.NewPruneReport(*report)

	return &PruneImagesOutput{
		Body: base.ApiResponse[image.PruneReport]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetImageUsageCounts returns counts of images by usage status.
func (h *ImageHandler) GetImageUsageCounts(ctx context.Context, input *GetImageUsageCountsInput) (*GetImageUsageCountsOutput, error) {
	if h.dockerService == nil || h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	var (
		inuse, unused, total int
		totalSize            int64
		errs                 []error
	)

	_, iu, un, tot, err := h.dockerService.GetAllImages(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("get images: %w", err))
	} else {
		inuse, unused, total = iu, un, tot
	}

	sz, err := h.imageService.GetTotalImageSize(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("get total image size: %w", err))
	} else {
		totalSize = sz
	}

	if len(errs) > 0 {
		return nil, huma.Error500InternalServerError((&common.ImageUsageCountsError{Err: errors.Join(errs...)}).Error())
	}

	return &GetImageUsageCountsOutput{
		Body: ImageUsageCountsResponse{
			Success: true,
			Data: image.UsageCounts{
				Inuse:     inuse,
				Unused:    unused,
				Total:     total,
				TotalSize: totalSize,
			},
		},
	}, nil
}

// UploadImage uploads a Docker image from a tar archive.
func (h *ImageHandler) UploadImage(ctx context.Context, input *UploadImageInput) (*UploadImageOutput, error) {
	if h.imageService == nil || h.settingsService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	// Get file from multipart form
	files := input.RawBody.File["file"]
	if len(files) == 0 {
		return nil, huma.Error400BadRequest((&common.NoFileUploadedError{}).Error())
	}

	fileHeader := files[0]
	fileName := fileHeader.Filename

	// Validate file extension
	lowerName := strings.ToLower(fileName)
	if !strings.HasSuffix(lowerName, ".tar") && !strings.HasSuffix(lowerName, ".tar.gz") && !strings.HasSuffix(lowerName, ".tgz") && !strings.HasSuffix(lowerName, ".tar.xz") {
		return nil, huma.Error400BadRequest((&common.InvalidFileFormatError{}).Error())
	}

	// Get max upload size from settings
	maxSizeMB := h.settingsService.GetIntSetting(ctx, "maxImageUploadSize", 500)
	maxSizeBytes := int64(maxSizeMB) * 1024 * 1024

	// Check file size
	if fileHeader.Size > maxSizeBytes {
		return nil, huma.NewError(http.StatusRequestEntityTooLarge, fmt.Sprintf("file size exceeds maximum allowed size of %d MB", maxSizeMB))
	}

	// Open the file
	file, err := fileHeader.Open()
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.FileUploadReadError{Err: err}).Error())
	}
	defer file.Close()

	// Load the image
	result, err := h.imageService.LoadImageFromReader(ctx, file, fileName, *user, maxSizeBytes)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ImageLoadError{Err: err}).Error())
	}

	return &UploadImageOutput{
		Body: base.ApiResponse[image.LoadResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}
