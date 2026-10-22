// Package image owns Docker image operations, metadata, attestations, patching, and routes.
package image

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/build"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/image/children/attestations"
	"github.com/getarcaneapp/arcane/backend/v3/internal/image/children/patch"
	"github.com/getarcaneapp/arcane/backend/v3/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service     *ImageService
	docker      *docker.DockerClientService
	imageUpdate *imageupdate.ImageUpdateService
	settings    *settings.SettingsService
	build       *build.BuildService
	activity    *activity.ActivityService
	upload      *upload.UploadService

	attestationsHandler *attestations.Handler
}

func New(
	service *ImageService,
	dockerService *docker.DockerClientService,
	imageUpdate *imageupdate.ImageUpdateService,
	settingsService *settings.SettingsService,
	buildService *build.BuildService,
	activityService *activity.ActivityService,
	uploadService *upload.UploadService,
) *Module {
	return &Module{
		service:             service,
		docker:              dockerService,
		imageUpdate:         imageUpdate,
		settings:            settingsService,
		build:               buildService,
		activity:            activityService,
		upload:              uploadService,
		attestationsHandler: attestations.NewHandler(attestations.NewService(service.dockerService, service.registryService.GetRegistryAuthForImage)),
	}
}

func (m *Module) Service() *ImageService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterImages(api, nil, nil, nil, nil, nil, nil, nil, appCtx)
		attestations.RegisterRoutes(api, attestations.NewHandler(nil))
		patch.RegisterRoutes(api, nil, appCtx)
		return
	}
	RegisterImages(api, m.docker, m.service, m.imageUpdate, m.settings, m.build, m.activity, m.upload, appCtx)
	attestations.RegisterRoutes(api, m.attestationsHandler)
	patch.RegisterRoutes(api, m.service.patch, appCtx)
}

// RegisterImages registers image management routes using Huma.
func RegisterImages(
	api huma.API,
	dockerService *docker.DockerClientService,
	imageService *ImageService,
	imageUpdateService *imageupdate.ImageUpdateService,
	settingsService *settings.SettingsService,
	buildService *build.BuildService,
	activityService *activity.ActivityService,
	uploadService *upload.UploadService,
	appCtx handlerutil.ActivityAppContext,
) {
	h := &ImageHandler{
		dockerService:      dockerService,
		imageService:       imageService,
		imageUpdateService: imageUpdateService,
		settingsService:    settingsService,
		buildService:       buildService,
		activityService:    activityService,
		uploadService:      uploadService,
		appCtx:             appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-images",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images",
		Summary:     "List images",
		Description: "Get a paginated list of Docker images",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermImagesList, h.ListImages)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-image-usage-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/counts",
		Summary:     "Get image usage counts",
		Description: "Get counts of images in use, unused, total, and total size",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermImagesList, h.GetImageUsageCounts)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "search-images",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/search",
		Summary:     "Search images",
		Description: "Search Docker Hub images",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesRead, h.SearchImages)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "tag-image",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/{name}/tag",
		Summary:     "Tag image",
		Description: "Add a repository tag to an image",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesTag, h.TagImage)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-image-history",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{name}/history",
		Summary:     "Get image history",
		Description: "Get Docker image layer history",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesRead, h.GetImageHistory)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "export-image",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{name}/export",
		Summary:     "Export image",
		Description: "Download a Docker image as a tar archive",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesRead, h.ExportImage)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-image",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{imageId}",
		Summary:     "Get image by ID",
		Description: "Get a Docker image by its ID",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesRead, h.GetImage)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "remove-image",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/images/{imageId}",
		Summary:     "Remove an image",
		Description: "Remove a Docker image by ID",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesDelete, h.RemoveImage)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pull-image",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/pull",
		Summary:     "Pull an image",
		Description: "Pull a Docker image from a registry with streaming progress output",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesPull, h.PullImage)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "build-image",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/build",
		Summary:     "Build an image",
		Description: "Build a Docker image using BuildKit with streaming progress output",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesBuild, h.BuildImage)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-image-builds",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/builds",
		Summary:     "List image builds",
		Description: "Get a paginated list of image build history for an environment",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermImagesList, h.ListImageBuilds)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-image-build",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/builds/{buildId}",
		Summary:     "Get image build",
		Description: "Get a single image build history entry with output",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesRead, h.GetImageBuild)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "prune-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/prune",
		Summary:     "Prune unused images",
		Description: "Remove unused Docker images",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesPrune, h.PruneImages)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "upload-image",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/upload",
		Summary:     "Upload an image",
		Description: "Load a Docker image tar archive from a complete chunked upload session. multipart/form-data bodies " +
			"are still accepted for backward compatibility; that form is deprecated and will be removed in a " +
			"future release.",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: upload.LegacyMultipartMiddleware(api, h.uploadService, types.UploadKindImage),
	}, authz.PermImagesUpload, h.UploadImage)
}
