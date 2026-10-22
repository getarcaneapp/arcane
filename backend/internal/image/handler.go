package image

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"
	buildtypes "go.getarcane.app/builds/types"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/build"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/upload"
	activitylib "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

// ImageHandler provides Huma-based image management endpoints.
type ImageHandler struct {
	dockerService      *docker.DockerClientService
	imageService       *ImageService
	imageUpdateService *imageupdate.ImageUpdateService
	settingsService    *settings.SettingsService
	buildService       *build.BuildService
	activityService    *activity.ActivityService
	uploadService      *upload.UploadService
	appCtx             context.Context
}

// ListImagesOutput is the image list response including the upload limit.
type ListImagesOutput struct {
	Body types.ImageListResponse
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

type GetImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID"`
}

type TagImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageName     string `path:"name" doc:"Image ID or image reference"`
	Body          types.ImageTagRequest
}

type GetImageHistoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageName     string `path:"name" doc:"Image ID or image reference"`
}

type SearchImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Term          string `query:"term" doc:"Search term"`
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

type PullImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          types.ImagePullOptions
}

type BuildImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          buildtypes.BuildRequest
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
	IncludeTotal  bool   `query:"includeTotal" doc:"Return exact totalItems and totalPages; otherwise they are -1 and hasNextPage reports whether more rows exist"`
}

type GetImageBuildInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BuildID       string `path:"buildId" doc:"Build ID"`
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

type GetImageUsageCountsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type UploadImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          types.UploadConsumeRequest
}

// ListImages returns a paginated list of images.
func (h *ImageHandler) ListImages(ctx context.Context, input *ListImagesInput) (*ListImagesOutput, error) {
	params := handlerutil.PaginationParams(input.Start, cmp.Or(input.Limit, 20), input.Sort, input.Order, input.Search)
	if input.InUse != "" {
		params.Filters["inUse"] = input.InUse
	}
	if input.Updates != "" {
		params.Filters["updates"] = input.Updates
	}

	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.imageService.AggregateImages(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list images: " + err.Error())
		}
		return &ListImagesOutput{Body: body}, nil
	}

	images, paginationResp, err := h.imageService.ListImagesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list images: " + err.Error())
	}

	if images == nil {
		images = []types.ImageSummary{}
	}

	return &ListImagesOutput{
		Body: types.ImageListResponse{
			Success:            true,
			Data:               images,
			Pagination:         handlerutil.PaginationResponse(paginationResp),
			MaxImageUploadSize: h.settingsService.GetIntSetting(ctx, "maxImageUploadSize"),
		},
	}, nil
}

// GetImage returns an image by ID.
func (h *ImageHandler) GetImage(ctx context.Context, input *GetImageInput) (*handlerutil.Out[types.ImageDetailSummary], error) {
	out, err := h.imageService.GetImageDetail(ctx, input.ImageID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Image not found: " + err.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to get image: " + err.Error())
	}

	return &handlerutil.Out[types.ImageDetailSummary]{
		Body: types.ApiResponse[types.ImageDetailSummary]{
			Success: true,
			Data:    *out,
		},
	}, nil
}

// TagImage adds a repository tag to an image.
func (h *ImageHandler) TagImage(ctx context.Context, input *TagImageInput) (*handlerutil.Out[types.MessageResponse], error) {
	imageName, err := validateImageName(input.ImageName)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Body.Repository) == "" {
		return nil, huma.Error400BadRequest("repository is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	if tagImageErr := h.imageService.TagImage(ctx, imageName, input.Body, *user); tagImageErr != nil {
		return nil, huma.NewError(common.ToAPIError(common.ClassifyDockerError(tagImageErr)).HTTPStatus(), fmt.Sprintf("failed to tag image: %v", tagImageErr))
	}

	return handlerutil.MessageOutput("Image tagged successfully", ""), nil
}

// GetImageHistory returns Docker image layer history.
func (h *ImageHandler) GetImageHistory(ctx context.Context, input *GetImageHistoryInput) (*handlerutil.Out[[]types.ImageHistoryItem], error) {
	imageName, err := validateImageName(input.ImageName)
	if err != nil {
		return nil, err
	}

	history, err := h.imageService.GetImageHistory(ctx, imageName)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(common.ClassifyDockerError(err)).HTTPStatus(), fmt.Sprintf("failed to get image history: %v", err))
	}
	if history == nil {
		history = []types.ImageHistoryItem{}
	}

	return &handlerutil.Out[[]types.ImageHistoryItem]{
		Body: types.ApiResponse[[]types.ImageHistoryItem]{
			Success: true,
			Data:    history,
		},
	}, nil
}

// SearchImages searches Docker Hub images.
func (h *ImageHandler) SearchImages(ctx context.Context, input *SearchImagesInput) (*handlerutil.Out[[]types.ImageSearchResult], error) {
	results, err := h.imageService.SearchImages(ctx, input.Term)
	if err != nil {
		if strings.Contains(err.Error(), "term is required") {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to search images: %v", err))
	}
	if results == nil {
		results = []types.ImageSearchResult{}
	}

	return &handlerutil.Out[[]types.ImageSearchResult]{
		Body: types.ApiResponse[[]types.ImageSearchResult]{
			Success: true,
			Data:    results,
		},
	}, nil
}

// ExportImage streams a Docker image tar archive.
func (h *ImageHandler) ExportImage(ctx context.Context, input *ExportImageInput) (*huma.StreamResponse, error) {
	imageName, err := validateImageName(input.ImageName)
	if err != nil {
		return nil, err
	}

	reader, err := h.imageService.ExportImage(ctx, imageName)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(common.ClassifyDockerError(err)).HTTPStatus(), fmt.Sprintf("failed to export image: %v", err))
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			defer func() { _ = reader.Close() }()

			humaCtx.SetHeader("Content-Type", "application/x-tar")
			humaCtx.SetHeader("Content-Disposition", fmt.Sprintf(
				"attachment; filename=%q",
				cmp.Or(strings.Trim(strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(imageName), "._-"), "image")+".tar",
			))

			if _, copyErr := io.Copy(humaCtx.BodyWriter(), reader); copyErr != nil {
				slog.WarnContext(humaCtx.Context(), "image export stream interrupted", "image", imageName, "error", copyErr)
			}
		},
	}, nil
}

func validateImageName(raw string) (string, error) {
	imageName := strings.TrimSpace(raw)
	if imageName == "" {
		return "", huma.Error400BadRequest("image name is required")
	}
	return imageName, nil
}

// RemoveImage removes a Docker image.
func (h *ImageHandler) RemoveImage(ctx context.Context, input *RemoveImageInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	if removeImageErr := h.imageService.RemoveImage(ctx, input.ImageID, input.Force, *user); removeImageErr != nil {
		switch {
		case errdefs.IsConflict(removeImageErr):
			return nil, huma.Error409Conflict("Failed to remove image: " + removeImageErr.Error())
		case errdefs.IsNotFound(removeImageErr):
			return nil, huma.Error404NotFound("Failed to remove image: " + removeImageErr.Error())
		default:
			return nil, huma.Error500InternalServerError("Failed to remove image: " + removeImageErr.Error())
		}
	}

	return handlerutil.MessageOutput("Image removed successfully", ""), nil
}

// PullImage pulls a Docker image with streaming progress.
func (h *ImageHandler) PullImage(ctx context.Context, input *PullImageInput) (*huma.StreamResponse, error) {
	if input.Body.ImageName == "" {
		return nil, huma.Error400BadRequest("image name is required")
	}

	user, err := handlerutil.RequireUser(ctx)
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
			activityID, runtimeCtx := activitylib.StartHandlerActivity(
				runtimeCtx,
				h.activityService,
				input.EnvironmentID,
				types.ActivityTypeImagePull,
				"image",
				"",
				fullImageName,
				user,
				"Pulling image",
				"Image pull started",
				database.JSON{"imageName": fullImageName},
				true,
			)
			activitylib.WriteStartedLine(rawWriter, activityID)
			if f, ok := rawWriter.(http.Flusher); ok {
				f.Flush()
			}
			activitylib.AwaitHandlerActivitySlot(runtimeCtx, h.activityService, activityID, input.EnvironmentID)

			writer := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, rawWriter, "Pulling image")
			if pullImageErr := h.imageService.Pull(runtimeCtx, PullRequest{
				Ref:         fullImageName,
				Progress:    writer,
				User:        *user,
				Credentials: credentials,
			}); pullImageErr != nil {
				activitylib.FlushWriter(writer)
				activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image pull failed", pullImageErr)
				activitylib.WriteErrorLine(rawWriter, pullImageErr)
				return
			}
			activitylib.FlushWriter(writer)
			activitylib.WriteDoneLine(rawWriter)
			activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image pull completed", nil)
		},
	}, nil
}

// BuildImage builds a Docker image with streaming progress.
func (h *ImageHandler) BuildImage(ctx context.Context, input *BuildImageInput) (*huma.StreamResponse, error) {
	if strings.TrimSpace(input.Body.ContextDir) == "" {
		return nil, huma.Error400BadRequest("contextDir is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			httpx.SetJSONStreamHeaders(humaCtx)

			runtimeCtx := utils.ActivityRuntimeContext(humaCtx.Context(), h.appCtx)
			rawWriter := humaCtx.BodyWriter()
			activityID, runtimeCtx := activitylib.StartHandlerActivity(
				runtimeCtx,
				h.activityService,
				input.EnvironmentID,
				types.ActivityTypeImageBuild,
				"image",
				"",
				kit.Ternary(strings.TrimSpace(strings.Join(input.Body.Tags, ", ")) == "", input.Body.ContextDir, strings.Join(input.Body.Tags, ", ")),
				user,
				"Building image",
				"Image build started",
				database.JSON{"contextDir": input.Body.ContextDir, "tags": input.Body.Tags},
				true,
			)
			activitylib.WriteStartedLine(rawWriter, activityID)
			if f, ok := rawWriter.(http.Flusher); ok {
				f.Flush()
			}
			activitylib.AwaitHandlerActivitySlot(runtimeCtx, h.activityService, activityID, input.EnvironmentID)

			writer := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, rawWriter, "Building image")
			if _, buildImageErr := h.buildService.BuildImage(runtimeCtx, input.EnvironmentID, input.Body, writer, "", user); buildImageErr != nil {
				activitylib.FlushWriter(writer)
				activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image build failed", buildImageErr)
				activitylib.WriteErrorLine(rawWriter, buildImageErr)
				return
			}
			activitylib.FlushWriter(writer)
			activitylib.WriteDoneLine(rawWriter)
			activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image build completed", nil)
		},
	}, nil
}

// ListImageBuilds returns a paginated list of image build history entries.
func (h *ImageHandler) ListImageBuilds(ctx context.Context, input *ListImageBuildsInput) (*handlerutil.Page[types.ImageBuildRecord], error) {
	if input.EnvironmentID == "" {
		return nil, huma.Error400BadRequest("Environment ID is required")
	}

	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	params.SkipCount = !input.IncludeTotal
	if input.Status != "" {
		params.Filters["status"] = input.Status
	}
	if input.Provider != "" {
		params.Filters["provider"] = input.Provider
	}

	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.buildService.AggregateImageBuilds(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list build history: " + err.Error())
		}
		return &handlerutil.Page[types.ImageBuildRecord]{Body: body}, nil
	}

	builds, paginationResp, err := h.buildService.ListImageBuildsByEnvironmentPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list build history: " + err.Error())
	}

	if builds == nil {
		builds = []types.ImageBuildRecord{}
	}

	return &handlerutil.Page[types.ImageBuildRecord]{
		Body: types.Paginated[types.ImageBuildRecord]{
			Success:    true,
			Data:       builds,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetImageBuild returns a single build history entry.
func (h *ImageHandler) GetImageBuild(ctx context.Context, input *GetImageBuildInput) (*handlerutil.Out[types.ImageBuildRecord], error) {
	if input.EnvironmentID == "" {
		return nil, huma.Error400BadRequest("Environment ID is required")
	}

	if input.BuildID == "" {
		return nil, huma.Error400BadRequest("buildId is required")
	}

	buildRecord, err := h.buildService.GetImageBuildByID(ctx, input.EnvironmentID, input.BuildID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("build not found")
		}
		return nil, huma.Error500InternalServerError("Failed to retrieve build history: " + err.Error())
	}

	return &handlerutil.Out[types.ImageBuildRecord]{
		Body: types.ApiResponse[types.ImageBuildRecord]{
			Success: true,
			Data:    *buildRecord,
		},
	}, nil
}

// PruneImages removes unused Docker images.
func (h *ImageHandler) PruneImages(ctx context.Context, input *PruneImagesInput) (*handlerutil.Out[types.ImagePruneReport], error) {
	report, err := h.imageService.PruneImages(ctx, types.PruneImagesOptions{
		Mode: types.PruneImageMode(func() string {
			if mode := strings.TrimSpace(kit.FromPtr(kit.FromPtr(input.Body).Mode)); mode != "" {
				return mode
			}
			if dangling, ok := mo.PointerToOption(kit.FromPtr(input.Body).Dangling).Get(); ok {
				return kit.Ternary(dangling, "dangling", "all")
			}
			for _, value := range kit.FromPtr(input.Body).Filters["dangling"] {
				if dangling, valid := kit.ParseBool(value); valid {
					return kit.Ternary(dangling, "dangling", "all")
				}
			}
			return kit.Ternary(input.Dangling, "dangling", "all")
		}()),
		Until: func() string {
			if until, ok := mo.PointerToOption(kit.FromPtr(input.Body).Until).Get(); ok {
				return strings.TrimSpace(until)
			}
			if vals := kit.FromPtr(input.Body).Filters["until"]; len(vals) > 0 {
				return strings.TrimSpace(vals[0])
			}
			return ""
		}(),
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to prune images: " + err.Error())
	}

	return &handlerutil.Out[types.ImagePruneReport]{
		Body: types.ApiResponse[types.ImagePruneReport]{
			Success: true,
			Data:    types.NewImagePruneReport(*report),
		},
	}, nil
}

// GetImageUsageCounts returns counts of images by usage status.
func (h *ImageHandler) GetImageUsageCounts(ctx context.Context, input *GetImageUsageCountsInput) (*handlerutil.Out[types.ImageUsageCounts], error) {
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.imageService.AggregateImageCounts(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get image usage counts: " + err.Error())
		}
		return &handlerutil.Out[types.ImageUsageCounts]{Body: body}, nil
	}

	_, counts, err := h.dockerService.GetAllImages(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get image usage counts: " + err.Error())
	}

	return &handlerutil.Out[types.ImageUsageCounts]{
		Body: types.ApiResponse[types.ImageUsageCounts]{
			Success: true,
			Data:    counts,
		},
	}, nil
}

// UploadImage uploads a Docker image from a tar archive.
func (h *ImageHandler) UploadImage(ctx context.Context, input *UploadImageInput) (*handlerutil.Out[types.ImageLoadResult], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	file, session, cleanup, err := h.uploadService.Consume(ctx, types.UploadKindImage, input.Body.UploadID)
	if err != nil {
		if httpErr := upload.SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError("Failed to open upload: " + err.Error())
	}
	defer cleanup()

	result, err := h.imageService.LoadImageFromReader(ctx, file, session.Filename, *user, int64(h.settingsService.GetIntSetting(ctx, "maxImageUploadSize"))*1024*1024)
	if err != nil {
		if errors.Is(err, common.ErrBadRequest) {
			return nil, huma.Error413RequestEntityTooLarge("Failed to load image: " + err.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to load image: " + err.Error())
	}

	return &handlerutil.Out[types.ImageLoadResult]{
		Body: types.ApiResponse[types.ImageLoadResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}
