package api

import (
	"context"
	"maps"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/danielgtaylor/huma/v2/sse"
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/api/handlers"
	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/build"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/container"
	"getarcane.dev/arcane/v3/internal/diagnostics"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/gitops"
	"getarcane.dev/arcane/v3/internal/gitrepo"
	"getarcane.dev/arcane/v3/internal/health"
	"getarcane.dev/arcane/v3/internal/image"
	"getarcane.dev/arcane/v3/internal/imageupdate"
	"getarcane.dev/arcane/v3/internal/job"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/network"
	"getarcane.dev/arcane/v3/internal/podman"
	"getarcane.dev/arcane/v3/internal/port"
	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/swarm"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/internal/updater"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/internal/variable"
	"getarcane.dev/arcane/v3/internal/version"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/arcane/v3/internal/vulnerability"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/jsonx"
)

type (
	HandlerDeps struct {
		fx.In

		Project            *project.Module
		EnvironmentService *environment.EnvironmentService
		Settings           *settings.Module
		JobSchedule        *job.Module
		Container          *container.Module
		Image              *image.Module
		Build              *build.BuildService
		Volume             *volume.Module
		S3Destination      *s3.Module
		Network            *network.NetworkService
		Port               *port.PortService
		Swarm              *swarm.Module
		Podman             *podman.Module
		ImageUpdate        *imageupdate.Module
		AuthService        *auth.AuthService
		ApiKeyService      *apikey.ApiKeyService
		RoleService        *role.RoleService
		Docker             *docker.DockerClientService
		ContainerRegistry  *registry.Module
		System             *system.Module
		Diagnostics        *diagnostics.DiagnosticsService
		Updater            *updater.Module
		Event              *event.Module
		Activity           *activity.Module
		Version            *version.VersionService
		GitRepository      *gitrepo.Module
		GitOpsSync         *gitops.Module
		Vulnerability      *vulnerability.Module
		Variable           *variable.Module
		Upload             *upload.Module
		Health             *health.Module
	}

	SetupParams struct {
		Echo        *echo.Echo
		Group       *echo.Group
		AppContext  handlerutil.ActivityAppContext
		Config      *config.Config
		Deps        HandlerDeps
		ManagerDeps ManagerHandlerDeps
		Tunnel      *agent.TunnelServer
	}
)

const (
	openAPIPath = "/openapi"
	schemasPath = "/schemas"
)

var featureRoutes []func(huma.API, SetupParams)

func SetupAPI(p SetupParams) *authz.OperationRecorder {
	p.Echo.JSONSerializer = jsonx.Serializer{}
	sse.WriteTimeout = time.Hour

	humaConfig := huma.DefaultConfig("Arcane API", config.Version)
	humaConfig.Formats = maps.Clone(humaConfig.Formats)
	humaConfig.Formats["application/json"] = jsonx.HumaFormat
	humaConfig.Formats["json"] = jsonx.HumaFormat
	humaConfig.Info.Description = "Modern Docker Management, Designed for Everyone"
	if !p.Config.AgentEnabled {
		humaConfig.OpenAPIPath = openAPIPath
		humaConfig.SchemasPath = schemasPath
	} else {
		humaConfig.OpenAPIPath = ""
		humaConfig.SchemasPath = ""
	}
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
		AuthService:      p.Deps.AuthService,
		APIKeyService:    p.Deps.ApiKeyService,
		PermResolver:     p.Deps.RoleService,
		EnvTokenResolver: p.Deps.EnvironmentService,
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
	p.Deps.Project.RegisterRoutes(api, p.AppContext)
	version.RegisterVersion(api, p.Deps.Version)
	p.Deps.Event.RegisterRoutes(api, p.Deps.EnvironmentService.ResolveEventEnvironmentByAccessToken)
	p.Deps.Activity.RegisterRoutes(api)
	p.Deps.ContainerRegistry.RegisterRoutes(api)
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
	p.Deps.Updater.RegisterRoutes(api, p.AppContext)
	p.Deps.System.RegisterRoutes(api, p.AppContext)
	handlers.RegisterDiagnostics(api, p.Deps.Diagnostics)
	p.Deps.GitRepository.RegisterRoutes(api)
	p.Deps.GitOpsSync.RegisterRoutes(api)
	p.Deps.Vulnerability.RegisterRoutes(api, p.AppContext)

	if !p.Config.AgentEnabled {
		registerManagerRoutes(api, p)
	}
	for _, register := range featureRoutes {
		register(api, p)
	}

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
