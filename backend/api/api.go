package api

import (
	"context"
	"maps"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/api/handlers"
	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/apns"
	"getarcane.dev/arcane/v3/internal/appimages"
	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/build"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/container"
	"getarcane.dev/arcane/v3/internal/diagnostics"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/federated"
	"getarcane.dev/arcane/v3/internal/gitops"
	"getarcane.dev/arcane/v3/internal/gitrepo"
	"getarcane.dev/arcane/v3/internal/health"
	"getarcane.dev/arcane/v3/internal/image"
	"getarcane.dev/arcane/v3/internal/imageupdate"
	"getarcane.dev/arcane/v3/internal/job"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/network"
	"getarcane.dev/arcane/v3/internal/notification"
	"getarcane.dev/arcane/v3/internal/oidc"
	"getarcane.dev/arcane/v3/internal/passkey"
	"getarcane.dev/arcane/v3/internal/podman"
	"getarcane.dev/arcane/v3/internal/port"
	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/search"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/swarm"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/internal/template"
	"getarcane.dev/arcane/v3/internal/updater"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/internal/variable"
	"getarcane.dev/arcane/v3/internal/version"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/arcane/v3/internal/vulnerability"
	"getarcane.dev/arcane/v3/internal/webhook"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/jsonx"
	"getarcane.dev/arcane/v3/resources"
)

type (
	// HandlerDeps contains the services required to register HTTP API handlers.
	// It contains only dependencies consumed by SetupAPI and the authentication middleware.
	HandlerDeps struct {
		fx.In

		AppImages         *appimages.ApplicationImagesService
		User              *user.Module
		Project           *project.Module
		Environment       *environment.Module
		Settings          *settings.Module
		JobSchedule       *job.Module
		Search            *search.Module
		Container         *container.Module
		Image             *image.Module
		Build             *build.BuildService
		Volume            *volume.Module
		S3Destination     *s3.Module
		Network           *network.NetworkService
		Port              *port.PortService
		Swarm             *swarm.Module
		Podman            *podman.Module
		ImageUpdate       *imageupdate.Module
		Auth              *auth.Module
		Passkey           *passkey.PasskeyService
		Oidc              *oidc.OidcService
		Docker            *docker.DockerClientService
		Template          *template.Module
		ContainerRegistry *registry.Module
		System            *system.Module
		Diagnostics       *diagnostics.DiagnosticsService
		Updater           *updater.Module
		Event             *event.Module
		Activity          *activity.Module
		Version           *version.VersionService
		Notification      *notification.Module
		ApiKey            *apikey.Module
		Apns              *apns.Module
		Federated         *federated.FederatedCredentialService
		GitRepository     *gitrepo.Module
		GitOpsSync        *gitops.Module
		Webhook           *webhook.Module
		Vulnerability     *vulnerability.Module
		Role              *role.Module
		Variable          *variable.Module
		Upload            *upload.Module
		Health            *health.Module
	}
)

const (
	scalarDocsHTML = `<!doctype html>
<html>
  <head>
    <title>Arcane API Reference</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <script
      id="api-reference"
      data-url="/api/openapi.json"
      data-configuration='{
        "theme": "purple",
        "darkMode": true,
        "layout": "modern",
        "hiddenClients": ["unirest"],
        "defaultHttpClient": { "targetKey": "shell", "clientKey": "curl" }
      }'></script>
    <script src="/api/docs/scalar.js"></script>
  </body>
</html>`
)

// SetupAPI creates and configures the Huma API attached to the Echo router.
func SetupAPI(e *echo.Echo, apiGroup *echo.Group, appCtx handlerutil.ActivityAppContext, cfg *config.Config, deps HandlerDeps) huma.API {
	e.JSONSerializer = jsonx.Serializer{}

	humaConfig := huma.DefaultConfig("Arcane API", config.Version)
	humaConfig.Formats = maps.Clone(humaConfig.Formats)
	humaConfig.Formats["application/json"] = jsonx.HumaFormat
	humaConfig.Formats["json"] = jsonx.HumaFormat
	humaConfig.Info.Description = "Modern Docker Management, Designed for Everyone"
	// Disable default docs path - we'll use Scalar instead
	humaConfig.DocsPath = ""
	humaConfig.Servers = []*huma.Server{{URL: cfg.GetAppURL() + "/api"}}
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "JWT Bearer token authentication",
		},
		"ApiKeyAuth": {
			Type:        "apiKey",
			In:          "header",
			Name:        "X-API-Key",
			Description: "API Key authentication",
		},
	}
	humaConfig.Security = []map[string][]string{
		{"BearerAuth": {}},
		{"ApiKeyAuth": {}},
	}
	for t, alias := range schemaTypeAliases {
		humaConfig.Components.Schemas.RegisterTypeAlias(t, alias)
	}

	api := humaecho.NewWithGroup(e, apiGroup, humaConfig)

	api.UseMiddleware(auth.NewHumaMiddleware(auth.HumaMiddlewareParams{
		API:              api,
		AuthService:      deps.Auth.Service(),
		APIKeyService:    deps.ApiKey.Service(),
		PermResolver:     deps.Role.Service(),
		EnvTokenResolver: deps.Environment.Service(),
		Config:           cfg,
	}))
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		if actor, ok := handlerutil.CurrentUserFromContext(ctx.Context()); ok {
			middleware.RecordAuthenticatedRequest(ctx.Context(), humaecho.Unwrap(ctx).Request().Header, actor)
		}
		next(ctx)
	})
	api.UseMiddleware(middleware.NewActivityBatchID())
	registerNormalization(api)

	deps.Health.RegisterRoutes(api)
	deps.Auth.RegisterRoutes(api)
	passkey.RegisterPasskeys(api, deps.Passkey, deps.Auth.Service(), deps.User.Service())
	deps.ApiKey.RegisterRoutes(api)
	federated.RegisterFederatedCredentials(api, deps.Federated)
	deps.Role.RegisterRoutes(api)
	appimages.RegisterAppImages(api, deps.AppImages)
	deps.User.RegisterRoutes(api)
	deps.Project.RegisterRoutes(api, appCtx)
	version.RegisterVersion(api, deps.Version)
	deps.Event.RegisterRoutes(api)
	deps.Activity.RegisterRoutes(api)
	oidc.RegisterOidc(api, deps.Auth.Service(), deps.Passkey, deps.Oidc, deps.Role.Service(), deps.User.Service(), cfg)
	deps.Environment.RegisterRoutes(api, appCtx, deps.Version.GetAppVersionInfo)
	deps.ContainerRegistry.RegisterRoutes(api)
	deps.Template.RegisterRoutes(api)
	deps.Variable.RegisterRoutes(api, cfg)
	deps.Image.RegisterRoutes(api, appCtx)
	deps.Upload.RegisterRoutes(api)
	build.RegisterBuildWorkspaces(api, deps.Build, deps.Upload.Service())
	deps.ImageUpdate.RegisterRoutes(api, appCtx)
	deps.Settings.RegisterRoutes(api)
	deps.S3Destination.RegisterRoutes(api)
	deps.JobSchedule.RegisterRoutes(api)
	deps.Volume.RegisterRoutes(api, appCtx)
	deps.Container.RegisterRoutes(api, appCtx)
	port.RegisterPorts(api, deps.Port)
	network.RegisterNetworks(api, deps.Network, deps.Docker, deps.Activity.Service(), appCtx)
	deps.Swarm.RegisterRoutes(api)
	deps.Podman.RegisterRoutes(api)
	deps.Notification.RegisterRoutes(api)
	deps.Apns.RegisterRoutes(api)
	deps.Updater.RegisterRoutes(api, appCtx)
	deps.Search.RegisterRoutes(api)
	deps.System.RegisterRoutes(api, appCtx)
	handlers.RegisterDiagnostics(api, deps.Diagnostics)
	deps.GitRepository.RegisterRoutes(api)
	deps.GitOpsSync.RegisterRoutes(api)
	deps.Webhook.RegisterRoutes(api)
	deps.Vulnerability.RegisterRoutes(api, appCtx)
	handlers.RegisterStream(api, deps.Activity.Handler(), deps.Environment.Handler(), deps.Event.Service(), deps.Version, deps.System.Service())

	apiGroup.GET("/docs", func(c *echo.Context) error {
		return c.HTML(http.StatusOK, scalarDocsHTML)
	})
	apiGroup.GET("/docs/scalar.js", func(c *echo.Context) error {
		return c.FileFS("scalar/standalone.js", resources.FS)
	})

	return api
}

// SetupAPIForSpec creates a Huma API instance for OpenAPI spec generation only.
// No services are required - this is purely for schema generation.
func SetupAPIForSpec() huma.API {
	e := echo.New()
	appCtx := handlerutil.NewActivityAppContext(context.Background()) //nolint:forbidigo // Schema discovery has no running request or application lifecycle.
	return SetupAPI(e, e.Group("/api"), appCtx, &config.Config{}, HandlerDeps{Health: health.New(&health.Service{})})
}
