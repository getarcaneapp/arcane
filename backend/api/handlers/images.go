package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/containerd/platforms"
	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"gorm.io/gorm"
)

// ImageHandler provides Huma-based image management endpoints.
type ImageHandler struct {
	dockerService      *services.DockerClientService
	imageService       *services.ImageService
	imageUpdateService *services.ImageUpdateService
	settingsService    *services.SettingsService
	buildService       *services.BuildService
	activityService    *services.ActivityService
	appCtx             context.Context
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

type GetImageAttestationsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageName     string `path:"name" doc:"Image ID or image reference"`
	Platform      string `query:"platform" doc:"OCI platform selector, for example linux/amd64"`
	PredicateType string `query:"predicateType" doc:"Exact in-toto predicate type URI to include"`
	WithStatement bool   `query:"statement" default:"false" doc:"Include verbatim statement JSON bodies"`
}

type GetImageAttestationsOutput struct {
	Body base.ApiResponse[image.AttestationList]
}

type TagImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageName     string `path:"name" doc:"Image ID or image reference"`
	Body          image.TagRequest
}

type TagImageOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type GetImageHistoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageName     string `path:"name" doc:"Image ID or image reference"`
}

type GetImageHistoryOutput struct {
	Body base.ApiResponse[[]image.HistoryItem]
}

type SearchImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Term          string `query:"term" doc:"Search term"`
}

type SearchImagesOutput struct {
	Body base.ApiResponse[[]image.SearchResult]
}

type ExportImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageName     string `path:"name" doc:"Image ID or image reference"`
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

type BuildImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          image.BuildRequest
}

type ImageBuildPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []image.BuildRecord     `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListImageBuildsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"desc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	Status        string `query:"status" doc:"Filter by status"`
	Provider      string `query:"provider" doc:"Filter by provider"`
}

type ListImageBuildsOutput struct {
	Body ImageBuildPaginatedResponse
}

type GetImageBuildInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BuildID       string `path:"buildId" doc:"Build ID"`
}

type GetImageBuildOutput struct {
	Body base.ApiResponse[image.BuildRecord]
}

type PruneImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Dangling      bool   `query:"dangling" doc:"Only remove dangling images"`
	Body          *struct {
		Mode     *string             `json:"mode,omitempty"`
		Until    *string             `json:"until,omitempty"`
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
func RegisterImages(api huma.API, dockerService *services.DockerClientService, imageService *services.ImageService, imageUpdateService *services.ImageUpdateService, settingsService *services.SettingsService, buildService *services.BuildService, activityService *services.ActivityService, appCtx ActivityAppContext) {
	h := &ImageHandler{
		dockerService:      dockerService,
		imageService:       imageService,
		imageUpdateService: imageUpdateService,
		settingsService:    settingsService,
		buildService:       buildService,
		activityService:    activityService,
		appCtx:             appCtx.contextInternal(),
	}

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermImagesList, h.ListImages)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermImagesList, h.GetImageUsageCounts)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-image-attestations",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{name}/attestations",
		Summary:     "Get image attestations",
		Description: "Get in-toto attestation statements attached to a Docker image",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImagesRead, h.GetImageAttestations)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "search-images",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/search",
		Summary:     "Search images",
		Description: "Search Docker Hub images",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImagesRead, h.SearchImages)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "tag-image",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/{name}/tag",
		Summary:     "Tag image",
		Description: "Add a repository tag to an image",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImagesTag, h.TagImage)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-image-history",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{name}/history",
		Summary:     "Get image history",
		Description: "Get Docker image layer history",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImagesRead, h.GetImageHistory)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "export-image",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{name}/export",
		Summary:     "Export image",
		Description: "Download a Docker image as a tar archive",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImagesRead, h.ExportImage)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermImagesRead, h.GetImage)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermImagesDelete, h.RemoveImage)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermImagesPull, h.PullImage)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "build-image",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/build",
		Summary:     "Build an image",
		Description: "Build a Docker image using BuildKit with streaming progress output",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImagesBuild, h.BuildImage)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-image-builds",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/builds",
		Summary:     "List image builds",
		Description: "Get a paginated list of image build history for an environment",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImagesList, h.ListImageBuilds)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-image-build",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/builds/{buildId}",
		Summary:     "Get image build",
		Description: "Get a single image build history entry with output",
		Tags:        []string{"Images"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImagesRead, h.GetImageBuild)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermImagesPrune, h.PruneImages)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermImagesUpload, h.UploadImage)
}

// ListImages returns a paginated list of images.
func (h *ImageHandler) ListImages(ctx context.Context, input *ListImagesInput) (*ListImagesOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.InUse != "" {
		params.Filters["inUse"] = input.InUse
	}
	if input.Updates != "" {
		params.Filters["updates"] = input.Updates
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
			Success:    true,
			Data:       images,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// GetImage returns an image by ID.
func (h *ImageHandler) GetImage(ctx context.Context, input *GetImageInput) (*GetImageOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	out, err := h.imageService.GetImageDetail(ctx, input.ImageID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.ImageNotFoundError{Err: err}).Error())
	}

	return &GetImageOutput{
		Body: base.ApiResponse[image.DetailSummary]{
			Success: true,
			Data:    *out,
		},
	}, nil
}

// GetImageAttestations returns in-toto attestation statements attached to an image.
func (h *ImageHandler) GetImageAttestations(ctx context.Context, input *GetImageAttestationsInput) (*GetImageAttestationsOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	imageName, err := url.PathUnescape(input.ImageName)
	if err != nil {
		return nil, huma.Error400BadRequest(fmt.Sprintf("invalid image name %q", input.ImageName))
	}
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return nil, huma.Error400BadRequest("image name is required")
	}

	if input.Platform != "" {
		if _, err := platforms.Parse(input.Platform); err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid platform %q", input.Platform))
		}
	}

	out, err := h.imageService.GetImageAttestations(ctx, imageName, services.ImageAttestationQuery{
		Platform:         strings.TrimSpace(input.Platform),
		PredicateType:    strings.TrimSpace(input.PredicateType),
		IncludeStatement: input.WithStatement,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to get image attestations: %v", err))
	}

	return &GetImageAttestationsOutput{
		Body: base.ApiResponse[image.AttestationList]{
			Success: true,
			Data:    *out,
		},
	}, nil
}

// TagImage adds a repository tag to an image.
func (h *ImageHandler) TagImage(ctx context.Context, input *TagImageInput) (*TagImageOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	imageName, err := decodeImageNameInternal(input.ImageName)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Body.Repository) == "" {
		return nil, huma.Error400BadRequest("repository is required")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.imageService.TagImage(ctx, imageName, input.Body, *user); err != nil {
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to tag image: %v", err))
	}

	return &TagImageOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Image tagged successfully"},
		},
	}, nil
}

// GetImageHistory returns Docker image layer history.
func (h *ImageHandler) GetImageHistory(ctx context.Context, input *GetImageHistoryInput) (*GetImageHistoryOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	imageName, err := decodeImageNameInternal(input.ImageName)
	if err != nil {
		return nil, err
	}

	history, err := h.imageService.GetImageHistory(ctx, imageName)
	if err != nil {
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to get image history: %v", err))
	}
	if history == nil {
		history = []image.HistoryItem{}
	}

	return &GetImageHistoryOutput{
		Body: base.ApiResponse[[]image.HistoryItem]{
			Success: true,
			Data:    history,
		},
	}, nil
}

// SearchImages searches Docker Hub images.
func (h *ImageHandler) SearchImages(ctx context.Context, input *SearchImagesInput) (*SearchImagesOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	results, err := h.imageService.SearchImages(ctx, input.Term)
	if err != nil {
		if strings.Contains(err.Error(), "term is required") {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to search images: %v", err))
	}
	if results == nil {
		results = []image.SearchResult{}
	}

	return &SearchImagesOutput{
		Body: base.ApiResponse[[]image.SearchResult]{
			Success: true,
			Data:    results,
		},
	}, nil
}

// ExportImage streams a Docker image tar archive.
func (h *ImageHandler) ExportImage(ctx context.Context, input *ExportImageInput) (*huma.StreamResponse, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	imageName, err := decodeImageNameInternal(input.ImageName)
	if err != nil {
		return nil, err
	}

	reader, err := h.imageService.ExportImage(ctx, imageName)
	if err != nil {
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to export image: %v", err))
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			defer func() { _ = reader.Close() }()

			humaCtx.SetHeader("Content-Type", "application/x-tar")
			humaCtx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", imageExportFileNameInternal(imageName)))

			_, _ = io.Copy(humaCtx.BodyWriter(), reader)
		},
	}, nil
}

func decodeImageNameInternal(raw string) (string, error) {
	imageName, err := url.PathUnescape(raw)
	if err != nil {
		return "", huma.Error400BadRequest(fmt.Sprintf("invalid image name %q", raw))
	}
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return "", huma.Error400BadRequest("image name is required")
	}
	return imageName, nil
}

func imageExportFileNameInternal(imageName string) string {
	name := strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(imageName)
	name = strings.Trim(name, "._-")
	if name == "" {
		name = "image"
	}
	return name + ".tar"
}

// RemoveImage removes a Docker image.
func (h *ImageHandler) RemoveImage(ctx context.Context, input *RemoveImageInput) (*RemoveImageOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
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

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	// Get full image name with tag and credentials
	fullImageName := input.Body.GetFullImageName()
	credentials := input.Body.GetCredentials()

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			httpx.SetJSONStreamHeaders(humaCtx)

			runtimeCtx := utils.ActivityRuntimeContext(humaCtx.Context(), h.appCtx)
			rawWriter := humaCtx.BodyWriter()
			activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(
				runtimeCtx,
				h.activityService,
				input.EnvironmentID,
				models.ActivityTypeImagePull,
				"image",
				"",
				fullImageName,
				user,
				"Pulling image",
				"Image pull started",
				models.JSON{"imageName": fullImageName},
			)
			activitylib.WriteStartedLine(rawWriter, activityID)
			if f, ok := rawWriter.(http.Flusher); ok {
				f.Flush()
			}

			writer := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, rawWriter, "Pulling image")
			if err := h.imageService.PullImage(runtimeCtx, fullImageName, writer, *user, credentials); err != nil {
				activitylib.FlushWriter(writer)
				activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image pull failed", err)
				_, _ = fmt.Fprintf(writer, `{"error":%q}`+"\n", err.Error())
				return
			}
			activitylib.FlushWriter(writer)
			activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image pull completed", nil)
		},
	}, nil
}

// BuildImage builds a Docker image with streaming progress.
func (h *ImageHandler) BuildImage(ctx context.Context, input *BuildImageInput) (*huma.StreamResponse, error) {
	if h.buildService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if strings.TrimSpace(input.Body.ContextDir) == "" {
		return nil, huma.Error400BadRequest("contextDir is required")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			httpx.SetJSONStreamHeaders(humaCtx)

			runtimeCtx := utils.ActivityRuntimeContext(humaCtx.Context(), h.appCtx)
			rawWriter := humaCtx.BodyWriter()
			resourceName := strings.Join(input.Body.Tags, ", ")
			if strings.TrimSpace(resourceName) == "" {
				resourceName = input.Body.ContextDir
			}
			activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(
				runtimeCtx,
				h.activityService,
				input.EnvironmentID,
				models.ActivityTypeImageBuild,
				"image",
				"",
				resourceName,
				user,
				"Building image",
				"Image build started",
				models.JSON{"contextDir": input.Body.ContextDir, "tags": input.Body.Tags},
			)
			activitylib.WriteStartedLine(rawWriter, activityID)
			if f, ok := rawWriter.(http.Flusher); ok {
				f.Flush()
			}

			writer := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, rawWriter, "Building image")
			if _, err := h.buildService.BuildImage(runtimeCtx, input.EnvironmentID, input.Body, writer, "", user); err != nil {
				activitylib.FlushWriter(writer)
				activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image build failed", err)
				_, _ = fmt.Fprintf(writer, `{"error":%q}`+"\n", err.Error())
				return
			}
			activitylib.FlushWriter(writer)
			activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image build completed", nil)
		},
	}, nil
}

// ListImageBuilds returns a paginated list of image build history entries.
func (h *ImageHandler) ListImageBuilds(ctx context.Context, input *ListImageBuildsInput) (*ListImageBuildsOutput, error) {
	if h.buildService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EnvironmentID == "" {
		return nil, huma.Error400BadRequest((&common.EnvironmentIDRequiredError{}).Error())
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Status != "" {
		params.Filters["status"] = input.Status
	}
	if input.Provider != "" {
		params.Filters["provider"] = input.Provider
	}

	builds, paginationResp, err := h.buildService.ListImageBuildsByEnvironmentPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.BuildHistoryListError{Err: err}).Error())
	}

	if builds == nil {
		builds = []image.BuildRecord{}
	}

	return &ListImageBuildsOutput{
		Body: ImageBuildPaginatedResponse{
			Success:    true,
			Data:       builds,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// GetImageBuild returns a single build history entry.
func (h *ImageHandler) GetImageBuild(ctx context.Context, input *GetImageBuildInput) (*GetImageBuildOutput, error) {
	if h.buildService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EnvironmentID == "" {
		return nil, huma.Error400BadRequest((&common.EnvironmentIDRequiredError{}).Error())
	}

	if input.BuildID == "" {
		return nil, huma.Error400BadRequest("buildId is required")
	}

	buildRecord, err := h.buildService.GetImageBuildByID(ctx, input.EnvironmentID, input.BuildID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("build not found")
		}
		return nil, huma.Error500InternalServerError((&common.BuildHistoryRetrievalError{Err: err}).Error())
	}

	return &GetImageBuildOutput{
		Body: base.ApiResponse[image.BuildRecord]{
			Success: true,
			Data:    *buildRecord,
		},
	}, nil
}

// PruneImages removes unused Docker images.
func (h *ImageHandler) PruneImages(ctx context.Context, input *PruneImagesInput) (*PruneImagesOutput, error) {
	if h.imageService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	mode := resolvePruneImageModeInternal(input)
	until := resolvePruneImageUntilInternal(input)

	report, err := h.imageService.PruneImages(ctx, system.PruneImagesOptions{
		Mode:  system.PruneImageMode(mode),
		Until: until,
	})
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

func resolvePruneImageModeInternal(input *PruneImagesInput) string {
	mode := resolveLegacyPruneImageModeInternal(input.Dangling)
	if input.Body == nil {
		return mode
	}

	if input.Body.Mode != nil && strings.TrimSpace(*input.Body.Mode) != "" {
		return strings.TrimSpace(*input.Body.Mode)
	}

	if input.Body.Dangling != nil {
		return resolveLegacyPruneImageModeInternal(*input.Body.Dangling)
	}

	if vals, ok := input.Body.Filters["dangling"]; ok {
		for _, value := range vals {
			switch value {
			case "true", "1":
				return "dangling"
			case "false", "0":
				return "all"
			}
		}
	}

	return mode
}

func resolvePruneImageUntilInternal(input *PruneImagesInput) string {
	if input.Body == nil {
		return ""
	}

	if input.Body.Until != nil {
		return strings.TrimSpace(*input.Body.Until)
	}

	if vals, ok := input.Body.Filters["until"]; ok && len(vals) > 0 {
		return strings.TrimSpace(vals[0])
	}

	return ""
}

func resolveLegacyPruneImageModeInternal(dangling bool) string {
	if dangling {
		return "dangling"
	}

	return "all"
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

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
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
	defer func() { _ = file.Close() }()

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
