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
	"getarcane.dev/arcane/v3/internal/telemetry"
	"getarcane.dev/arcane/v3/internal/template"
	"getarcane.dev/arcane/v3/internal/updater"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/internal/variable"
	"getarcane.dev/arcane/v3/internal/version"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/arcane/v3/internal/vulnerability"
	"getarcane.dev/arcane/v3/internal/webhook"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
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

	SetupParams struct {
		Echo       *echo.Echo
		Group      *echo.Group
		AppContext handlerutil.ActivityAppContext
		Config     *config.Config
		Deps       HandlerDeps
		Tunnel     *agent.TunnelServer
	}
)

const (
	openAPIPath    = "/openapi"
	schemasPath    = "/schemas"
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

var featureRoutes []func(huma.API, SetupParams)

// SetupAPI creates and configures the Huma API attached to the Echo router.
func SetupAPI(p SetupParams) *authz.OperationRecorder {
	p.Echo.JSONSerializer = jsonx.Serializer{}

	humaConfig := huma.DefaultConfig("Arcane API", config.Version)
	humaConfig.Formats = maps.Clone(humaConfig.Formats)
	humaConfig.Formats["application/json"] = jsonx.HumaFormat
	humaConfig.Formats["json"] = jsonx.HumaFormat
	humaConfig.Info.Description = "Modern Docker Management, Designed for Everyone"
	humaConfig.OpenAPIPath = openAPIPath
	humaConfig.SchemasPath = schemasPath
	// Disable default docs path - we'll use Scalar instead
	humaConfig.DocsPath = ""
	humaConfig.Servers = []*huma.Server{{URL: p.Config.GetAppURL() + "/api"}}
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

	api := &authz.OperationRecorder{API: humaecho.NewWithGroup(p.Echo, p.Group, humaConfig)}

	api.UseMiddleware(auth.NewHumaMiddleware(auth.HumaMiddlewareParams{
		API:              api,
		AuthService:      p.Deps.Auth.Service(),
		APIKeyService:    p.Deps.ApiKey.Service(),
		PermResolver:     p.Deps.Role.Service(),
		EnvTokenResolver: p.Deps.Environment.Service(),
		Config:           p.Config,
	}))
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		if actor, ok := handlerutil.CurrentUserFromContext(ctx.Context()); ok {
			middleware.RecordAuthenticatedRequest(ctx.Context(), humaecho.Unwrap(ctx).Request().Header, actor)
		}
		next(ctx)
	})
	api.UseMiddleware(middleware.NewActivityBatchID())
	registerNormalization(api)

	p.Deps.Health.RegisterRoutes(api)
	p.Deps.Auth.RegisterRoutes(api)
	passkey.RegisterPasskeys(api, p.Deps.Passkey, p.Deps.Auth.Service(), p.Deps.User.Service())
	p.Deps.ApiKey.RegisterRoutes(api)
	federated.RegisterFederatedCredentials(api, p.Deps.Federated)
	p.Deps.Role.RegisterRoutes(api)
	appimages.RegisterAppImages(api, p.Deps.AppImages)
	p.Deps.User.RegisterRoutes(api)
	p.Deps.Project.RegisterRoutes(api, p.AppContext)
	version.RegisterVersion(api, p.Deps.Version)
	p.Deps.Event.RegisterRoutes(api, p.Deps.Environment.Service().ResolveEventEnvironmentByAccessToken)
	p.Deps.Activity.RegisterRoutes(api)
	oidc.RegisterOidc(api, p.Deps.Auth.Service(), p.Deps.Passkey, p.Deps.Oidc, p.Deps.Role.Service(), p.Deps.User.Service(), p.Config)
	p.Deps.Environment.RegisterRoutes(api, p.AppContext, p.Deps.Version.GetAppVersionInfo)
	p.Deps.ContainerRegistry.RegisterRoutes(api)
	p.Deps.Template.RegisterRoutes(api)
	p.Deps.Variable.RegisterRoutes(api, p.Config)
	p.Deps.Image.RegisterRoutes(api, p.AppContext)
	p.Deps.Upload.RegisterRoutes(api)
	build.RegisterBuildWorkspaces(api, p.Deps.Build, p.Deps.Upload.Service())
	p.Deps.ImageUpdate.RegisterRoutes(api, p.AppContext)
	p.Deps.Settings.RegisterRoutes(api)
	p.Deps.S3Destination.RegisterRoutes(api)
	p.Deps.JobSchedule.RegisterRoutes(api)
	p.Deps.Volume.RegisterRoutes(api, p.AppContext)
	p.Deps.Container.RegisterRoutes(api, p.AppContext)
	port.RegisterPorts(api, p.Deps.Port)
	network.RegisterNetworks(api, p.Deps.Network, p.Deps.Docker, p.Deps.Activity.Service(), p.AppContext)
	p.Deps.Swarm.RegisterRoutes(api)
	p.Deps.Podman.RegisterRoutes(api)
	p.Deps.Notification.RegisterRoutes(api)
	p.Deps.Apns.RegisterRoutes(api)
	p.Deps.Updater.RegisterRoutes(api, p.AppContext)
	p.Deps.Search.RegisterRoutes(api)
	p.Deps.System.RegisterRoutes(api, p.AppContext)
	handlers.RegisterDiagnostics(api, p.Deps.Diagnostics)
	p.Deps.GitRepository.RegisterRoutes(api)
	p.Deps.GitOpsSync.RegisterRoutes(api)
	p.Deps.Webhook.RegisterRoutes(api, p.AppContext)
	p.Deps.Vulnerability.RegisterRoutes(api, p.AppContext)
	handlers.RegisterStream(api, p.Deps.Activity.Handler(), p.Deps.Environment.Handler(), p.Deps.Event.Service(), p.Deps.Version, p.Deps.System.Service())

	if !p.Config.AgentEnabled {
		telemetry.RegisterRoutes(api, telemetry.NewService())
		huma.Register(api, huma.Operation{
			OperationID: "enroll-agent-mtls",
			Method:      http.MethodPost,
			Path:        "/tunnel/mtls/enroll",
			Summary:     "Enroll an agent mTLS certificate",
			Tags:        []string{"Environments"},
			Hidden:      true,
			Security:    []map[string][]string{},
			Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessAgentToken},
			Middlewares: huma.Middlewares{
				middleware.EchoMiddleware(middleware.PerIPRateLimit(10, 3)),
				middleware.EchoMiddleware(middleware.PerAgentTokenRateLimit(10, 3)),
			},
		}, middleware.EchoHandler(p.Tunnel.HandleMTLSEnroll))
	}
	for _, register := range featureRoutes {
		register(api, p)
	}
	huma.Register(api, huma.Operation{
		OperationID: "get-api-docs",
		Method:      http.MethodGet,
		Path:        "/docs",
		Tags:        []string{"Docs"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessPublic},
	}, middleware.EchoHandler(func(c *echo.Context) error {
		return c.HTML(http.StatusOK, scalarDocsHTML)
	}))
	huma.Register(api, huma.Operation{
		OperationID: "get-api-docs-script",
		Method:      http.MethodGet,
		Path:        "/docs/scalar.js",
		Tags:        []string{"Docs"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessPublic},
	}, middleware.EchoHandler(func(c *echo.Context) error {
		return c.FileFS("scalar/standalone.js", resources.FS)
	}))

	return api
}

func SpecRoutes() []authz.PermissionRoute {
	return []authz.PermissionRoute{
		{
			Method: http.MethodGet,
			Path:   openAPIPath + ".json",
			Access: authz.EndpointAccessPublic,
		},
		{
			Method: http.MethodGet,
			Path:   openAPIPath + "-3.0.json",
			Access: authz.EndpointAccessPublic,
		},
		{
			Method: http.MethodGet,
			Path:   openAPIPath + ".yaml",
			Access: authz.EndpointAccessPublic,
		},
		{
			Method: http.MethodGet,
			Path:   openAPIPath + "-3.0.yaml",
			Access: authz.EndpointAccessPublic,
		},
		{
			Method: http.MethodGet,
			Path:   schemasPath + "/:schema",
			Access: authz.EndpointAccessPublic,
		},
	}
}

// SetupAPIForSpec creates a Huma API instance for OpenAPI spec generation only.
// No services are required - this is purely for schema generation.
func SetupAPIForSpec() *authz.OperationRecorder {
	e := echo.New()
	return SetupAPI(SetupParams{
		Echo:       e,
		Group:      e.Group("/api"),
		AppContext: handlerutil.NewActivityAppContext(context.Background()), //nolint:forbidigo // Schema discovery has no running request or application lifecycle.
		Config:     &config.Config{},
		Deps:       HandlerDeps{Health: health.New(&health.Service{})},
	})
}
