package image

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/system"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
	"go.getarcane.app/builds/types"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
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
	Body image.ListResponse
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
	Body          image.TagRequest
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
	Body          image.PullOptions
}

type BuildImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          types.BuildRequest
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
	Body          uploadtypes.ConsumeRequest
}

// ListImages returns a paginated list of images.
func (h *ImageHandler) ListImages(ctx context.Context, input *ListImagesInput) (*ListImagesOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.InUse != "" {
		params.Filters["inUse"] = input.InUse
	}
	if input.Updates != "" {
		params.Filters["updates"] = input.Updates
	}

	params.Limit = cmp.Or(params.Limit, 20)

	images, paginationResp, err := h.imageService.ListImagesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list images: " + err.Error())
	}

	if images == nil {
		images = []image.Summary{}
	}

	return &ListImagesOutput{
		Body: image.ListResponse{
			Success:            true,
			Data:               images,
			Pagination:         handlerutil.PaginationResponse(paginationResp),
			MaxImageUploadSize: h.settingsService.GetIntSetting(ctx, "maxImageUploadSize", 500),
		},
	}, nil
}

// GetImage returns an image by ID.
func (h *ImageHandler) GetImage(ctx context.Context, input *GetImageInput) (*handlerutil.Out[image.DetailSummary], error) {
	out, err := h.imageService.GetImageDetail(ctx, input.ImageID)
	if err != nil {
		return nil, huma.Error404NotFound("Image not found: " + err.Error())
	}

	return &handlerutil.Out[image.DetailSummary]{
		Body: base.ApiResponse[image.DetailSummary]{
			Success: true,
			Data:    *out,
		},
	}, nil
}

// TagImage adds a repository tag to an image.
func (h *ImageHandler) TagImage(ctx context.Context, input *TagImageInput) (*handlerutil.Out[base.MessageResponse], error) {
	imageName, err := validateImageNameInternal(input.ImageName)
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
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to tag image: %v", tagImageErr))
	}

	return handlerutil.MessageOutput("Image tagged successfully", ""), nil
}

// GetImageHistory returns Docker image layer history.
func (h *ImageHandler) GetImageHistory(ctx context.Context, input *GetImageHistoryInput) (*handlerutil.Out[[]image.HistoryItem], error) {
	imageName, err := validateImageNameInternal(input.ImageName)
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

	return &handlerutil.Out[[]image.HistoryItem]{
		Body: base.ApiResponse[[]image.HistoryItem]{
			Success: true,
			Data:    history,
		},
	}, nil
}

// SearchImages searches Docker Hub images.
func (h *ImageHandler) SearchImages(ctx context.Context, input *SearchImagesInput) (*handlerutil.Out[[]image.SearchResult], error) {
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

	return &handlerutil.Out[[]image.SearchResult]{
		Body: base.ApiResponse[[]image.SearchResult]{
			Success: true,
			Data:    results,
		},
	}, nil
}

// ExportImage streams a Docker image tar archive.
func (h *ImageHandler) ExportImage(ctx context.Context, input *ExportImageInput) (*huma.StreamResponse, error) {
	imageName, err := validateImageNameInternal(input.ImageName)
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

func validateImageNameInternal(raw string) (string, error) {
	imageName := strings.TrimSpace(raw)
	if imageName == "" {
		return "", huma.Error400BadRequest("image name is required")
	}
	return imageName, nil
}

func imageExportFileNameInternal(imageName string) string {
	name := strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(imageName)
	name = cmp.Or(strings.Trim(name, "._-"), "image")
	return name + ".tar"
}

// RemoveImage removes a Docker image.
func (h *ImageHandler) RemoveImage(ctx context.Context, input *RemoveImageInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	if removeImageErr := h.imageService.RemoveImage(ctx, input.ImageID, input.Force, *user); removeImageErr != nil {
		return nil, huma.Error500InternalServerError("Failed to remove image: " + removeImageErr.Error())
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
				activitytypes.TypeImagePull,
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
			if pullImageErr := h.imageService.PullImage(runtimeCtx, fullImageName, writer, *user, credentials); pullImageErr != nil {
				activitylib.FlushWriter(writer)
				activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image pull failed", pullImageErr)
				_, _ = fmt.Fprintf(writer, "{\"error\":%q}\n", pullImageErr.Error())
				if f, ok := writer.(http.Flusher); ok {
					f.Flush()
				}
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
			resourceName := strings.Join(input.Body.Tags, ", ")
			if strings.TrimSpace(resourceName) == "" {
				resourceName = input.Body.ContextDir
			}
			activityID, runtimeCtx := activitylib.StartHandlerActivity(
				runtimeCtx,
				h.activityService,
				input.EnvironmentID,
				activitytypes.TypeImageBuild,
				"image",
				"",
				resourceName,
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
				_, _ = fmt.Fprintf(writer, "{\"error\":%q}\n", buildImageErr.Error())
				if f, ok := writer.(http.Flusher); ok {
					f.Flush()
				}
				return
			}
			activitylib.FlushWriter(writer)
			activitylib.WriteDoneLine(rawWriter)
			activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Image build completed", nil)
		},
	}, nil
}

// ListImageBuilds returns a paginated list of image build history entries.
func (h *ImageHandler) ListImageBuilds(ctx context.Context, input *ListImageBuildsInput) (*handlerutil.Page[image.BuildRecord], error) {
	if input.EnvironmentID == "" {
		return nil, huma.Error400BadRequest("Environment ID is required")
	}

	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Status != "" {
		params.Filters["status"] = input.Status
	}
	if input.Provider != "" {
		params.Filters["provider"] = input.Provider
	}

	builds, paginationResp, err := h.buildService.ListImageBuildsByEnvironmentPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list build history: " + err.Error())
	}

	if builds == nil {
		builds = []image.BuildRecord{}
	}

	return &handlerutil.Page[image.BuildRecord]{
		Body: base.Paginated[image.BuildRecord]{
			Success:    true,
			Data:       builds,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetImageBuild returns a single build history entry.
func (h *ImageHandler) GetImageBuild(ctx context.Context, input *GetImageBuildInput) (*handlerutil.Out[image.BuildRecord], error) {
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

	return &handlerutil.Out[image.BuildRecord]{
		Body: base.ApiResponse[image.BuildRecord]{
			Success: true,
			Data:    *buildRecord,
		},
	}, nil
}

// PruneImages removes unused Docker images.
func (h *ImageHandler) PruneImages(ctx context.Context, input *PruneImagesInput) (*handlerutil.Out[image.PruneReport], error) {
	mode := resolvePruneImageModeInternal(input)
	until := resolvePruneImageUntilInternal(input)

	report, err := h.imageService.PruneImages(ctx, system.PruneImagesOptions{
		Mode:  system.PruneImageMode(mode),
		Until: until,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to prune images: " + err.Error())
	}

	out := image.NewPruneReport(*report)

	return &handlerutil.Out[image.PruneReport]{
		Body: base.ApiResponse[image.PruneReport]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func resolvePruneImageModeInternal(input *PruneImagesInput) string {
	mode := kit.Ternary(input.Dangling, "dangling", "all")
	if input.Body == nil {
		return mode
	}

	if input.Body.Mode != nil && strings.TrimSpace(*input.Body.Mode) != "" {
		return strings.TrimSpace(*input.Body.Mode)
	}

	if input.Body.Dangling != nil {
		return kit.Ternary(*input.Body.Dangling, "dangling", "all")
	}

	if vals, ok := input.Body.Filters["dangling"]; ok {
		for _, value := range vals {
			if dangling, valid := kit.ParseBool(value); valid {
				return kit.Ternary(dangling, "dangling", "all")
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

// GetImageUsageCounts returns counts of images by usage status.
func (h *ImageHandler) GetImageUsageCounts(ctx context.Context, input *GetImageUsageCountsInput) (*handlerutil.Out[image.UsageCounts], error) {
	_, counts, err := h.dockerService.GetAllImages(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get image usage counts: " + err.Error())
	}

	return &handlerutil.Out[image.UsageCounts]{
		Body: base.ApiResponse[image.UsageCounts]{
			Success: true,
			Data:    counts,
		},
	}, nil
}

// UploadImage uploads a Docker image from a tar archive.
func (h *ImageHandler) UploadImage(ctx context.Context, input *UploadImageInput) (*handlerutil.Out[image.LoadResult], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	file, session, cleanup, err := h.uploadService.Consume(ctx, uploadtypes.KindImage, input.Body.UploadID)
	if err != nil {
		if httpErr := upload.SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError("Failed to open upload: " + err.Error())
	}
	defer cleanup()

	maxSizeMB := h.settingsService.GetIntSetting(ctx, "maxImageUploadSize", 500)
	maxSizeBytes := int64(maxSizeMB) * 1024 * 1024

	result, err := h.imageService.LoadImageFromReader(ctx, file, session.Filename, *user, maxSizeBytes)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to load image: " + err.Error())
	}

	return &handlerutil.Out[image.LoadResult]{
		Body: base.ApiResponse[image.LoadResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}
