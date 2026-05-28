package api

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/backend/api/handlers"
	"github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/labstack/echo/v4"
)

const (
	arcaneTypesPrefix = "github.com/getarcaneapp/arcane/types/"
	dockerSDKPrefix   = "github.com/moby/moby"
)

var dockerSchemaPrefixes = map[string]string{
	"types":     "DockerTypes",
	"registry":  "DockerRegistry",
	"system":    "DockerSystem",
	"container": "DockerContainer",
	"network":   "DockerNetwork",
	"volume":    "DockerVolume",
	"swarm":     "DockerSwarm",
	"mount":     "DockerMount",
	"filters":   "DockerFilters",
	"blkiodev":  "DockerBlkiodev",
	"strslice":  "DockerStrslice",
	"events":    "DockerEvents",
	"image":     "DockerImage",
}

// customSchemaNamer creates unique schema names using package prefix for types
// from github.com/getarcaneapp/arcane/types to avoid conflicts between packages that have
// types with the same name (e.g., image.Summary vs env.Summary).
func customSchemaNamer(t reflect.Type, hint string) string {
	name := huma.DefaultSchemaNamer(t, hint)
	typeStr := t.String()
	pkgPath := packagePathForType(t)
	shortPkg := shortPackageFromTypeString(typeStr)

	if pkgName, ok := arcanePackageName(pkgPath); ok {
		name = pkgName + name
	} else if dockerPrefix, ok := dockerSchemaPrefix(pkgPath, shortPkg); ok {
		name = dockerPrefix + name
	}
	if innerPkg, ok := genericInnerPackageName(pkgPath, typeStr); ok {
		return strings.Replace(name, "UsageCounts", innerPkg+"UsageCounts", 1)
	}

	return name
}

func packagePathForType(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t.PkgPath()
}

func shortPackageFromTypeString(typeStr string) string {
	before, _, ok := strings.Cut(typeStr, ".")
	if !ok {
		return ""
	}

	return before
}

func arcanePackageName(pkgPath string) (string, bool) {
	if !strings.HasPrefix(pkgPath, arcaneTypesPrefix) {
		return "", false
	}

	parts := strings.Split(pkgPath, "/")
	if len(parts) == 0 {
		return "", false
	}

	pkg := parts[len(parts)-1]
	if pkg == "" {
		return "", false
	}

	return capitalizeFirst(pkg), true
}

func dockerSchemaPrefix(pkgPath, shortPkg string) (string, bool) {
	if strings.Contains(pkgPath, dockerSDKPrefix) {
		parts := strings.Split(pkgPath, "/")
		last := parts[len(parts)-1]
		if prefix, ok := dockerSchemaPrefixes[last]; ok {
			return prefix, true
		}
	}

	prefix, ok := dockerSchemaPrefixes[shortPkg]
	if !ok {
		return "", false
	}

	return prefix, true
}

func genericInnerPackageName(pkgPath, typeName string) (string, bool) {
	if !strings.HasPrefix(pkgPath, arcaneTypesPrefix+"base") {
		return "", false
	}
	if !strings.Contains(typeName, "[") || !strings.Contains(typeName, arcaneTypesPrefix) {
		return "", false
	}

	_, after, ok := strings.Cut(typeName, arcaneTypesPrefix)
	if !ok {
		return "", false
	}
	before, _, ok := strings.Cut(after, ".")
	if !ok || before == "" {
		return "", false
	}

	return capitalizeFirst(before), true
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}

// Services holds all service dependencies needed by Huma handlers.
type Services struct {
	User              *services.UserService
	Auth              *services.AuthService
	Oidc              *services.OidcService
	ApiKey            *services.ApiKeyService
	AppImages         *services.ApplicationImagesService
	Project           *services.ProjectService
	Event             *services.EventService
	Activity          *services.ActivityService
	Version           *services.VersionService
	Environment       *services.EnvironmentService
	Settings          *services.SettingsService
	JobSchedule       *services.JobService
	SettingsSearch    *services.SettingsSearchService
	ContainerRegistry *services.ContainerRegistryService
	Template          *services.TemplateService
	Docker            *services.DockerClientService
	Image             *services.ImageService
	ImageUpdate       *services.ImageUpdateService
	Build             *services.BuildService
	BuildWorkspace    *services.BuildWorkspaceService
	Volume            *services.VolumeService
	Container         *services.ContainerService
	Network           *services.NetworkService
	Port              *services.PortService
	Swarm             *services.SwarmService
	Notification      *services.NotificationService
	Updater           *services.UpdaterService
	CustomizeSearch   *services.CustomizeSearchService
	System            *services.SystemService
	SystemUpgrade     *services.SystemUpgradeService
	GitRepository     *services.GitRepositoryService
	GitOpsSync        *services.GitOpsSyncService
	Webhook           *services.WebhookService
	Vulnerability     *services.VulnerabilityService
	Dashboard         *services.DashboardService
	Role              *services.RoleService
	Config            *config.Config
}

// SetupAPI creates and configures the Huma API attached to the Echo router.
func SetupAPI(e *echo.Echo, apiGroup *echo.Group, cfg *config.Config, svc *Services) huma.API {
	humaConfig := huma.DefaultConfig("Arcane API", config.Version)
	humaConfig.Info.Description = "Modern Docker Management, Designed for Everyone"

	// Disable default docs path - we'll use Scalar instead
	humaConfig.DocsPath = ""

	// Configure servers for OpenAPI spec
	if cfg.AppUrl != "" {
		humaConfig.Servers = []*huma.Server{
			{URL: cfg.AppUrl + "/api"},
		}
	} else {
		humaConfig.Servers = []*huma.Server{
			{URL: "/api"},
		}
	}

	// Configure security schemes
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

	// Use custom schema namer to avoid conflicts between types with same name
	// from different packages (e.g., image.Summary vs env.Summary)
	humaConfig.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", customSchemaNamer)

	// Create Huma API wrapping the Echo router group
	api := humaecho.NewWithGroup(e, apiGroup, humaConfig)

	// Add authentication middleware
	api.UseMiddleware(middleware.NewAuthBridge(api, svc.Auth, svc.ApiKey, svc.Role, svc.Environment, cfg))

	// Register all Huma handlers
	registerHandlers(api, svc)

	// Register Scalar API docs endpoint with dark mode
	registerScalarDocs(apiGroup)

	return api
}

// scalarDocsHTML returns the HTML template for Scalar API documentation.
const scalarDocsHTML = `<!doctype html>
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
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`

// registerScalarDocs adds the Scalar API documentation endpoint.
func registerScalarDocs(apiGroup *echo.Group) {
	apiGroup.GET("/docs", func(c echo.Context) error {
		return c.HTML(http.StatusOK, scalarDocsHTML)
	})
}

// SetupAPIForSpec creates a Huma API instance for OpenAPI spec generation only.
// No services are required - this is purely for schema generation.
func SetupAPIForSpec() huma.API {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	apiGroup := e.Group("/api")

	humaConfig := huma.DefaultConfig("Arcane API", config.Version)
	humaConfig.Info.Description = "Modern Docker Management, Designed for Everyone"
	humaConfig.Servers = []*huma.Server{
		{URL: "/api"},
	}
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

	// Use custom schema namer to avoid conflicts between types with same name
	humaConfig.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", customSchemaNamer)

	api := humaecho.NewWithGroup(e, apiGroup, humaConfig)

	// Register handlers with nil services (just for schema)
	registerHandlers(api, nil)

	return api
}

// registerHandlers registers all Huma-based API handlers.
// Add new handlers here as they are migrated from Gin.
func registerHandlers(api huma.API, svc *Services) {
	var userSvc *services.UserService
	var authSvc *services.AuthService
	var oidcSvc *services.OidcService
	var apiKeySvc *services.ApiKeyService
	var appImagesSvc *services.ApplicationImagesService
	var projectSvc *services.ProjectService
	var eventSvc *services.EventService
	var activitySvc *services.ActivityService
	var versionSvc *services.VersionService
	var environmentSvc *services.EnvironmentService
	var settingsSvc *services.SettingsService
	var jobScheduleSvc *services.JobService
	var settingsSearchSvc *services.SettingsSearchService
	var containerRegistrySvc *services.ContainerRegistryService
	var templateSvc *services.TemplateService
	var dockerSvc *services.DockerClientService
	var imageSvc *services.ImageService
	var imageUpdateSvc *services.ImageUpdateService
	var buildSvc *services.BuildService
	var buildWorkspaceSvc *services.BuildWorkspaceService
	var volumeSvc *services.VolumeService
	var containerSvc *services.ContainerService
	var networkSvc *services.NetworkService
	var portSvc *services.PortService
	var swarmSvc *services.SwarmService
	var notificationSvc *services.NotificationService
	var updaterSvc *services.UpdaterService
	var customizeSearchSvc *services.CustomizeSearchService
	var systemSvc *services.SystemService
	var systemUpgradeSvc *services.SystemUpgradeService
	var gitRepositorySvc *services.GitRepositoryService
	var gitOpsSyncSvc *services.GitOpsSyncService
	var webhookSvc *services.WebhookService
	var vulnerabilitySvc *services.VulnerabilityService
	var dashboardSvc *services.DashboardService
	var roleSvc *services.RoleService
	var cfg *config.Config

	if svc != nil {
		userSvc = svc.User
		authSvc = svc.Auth
		oidcSvc = svc.Oidc
		apiKeySvc = svc.ApiKey
		appImagesSvc = svc.AppImages
		projectSvc = svc.Project
		eventSvc = svc.Event
		activitySvc = svc.Activity
		versionSvc = svc.Version
		environmentSvc = svc.Environment
		settingsSvc = svc.Settings
		jobScheduleSvc = svc.JobSchedule
		settingsSearchSvc = svc.SettingsSearch
		containerRegistrySvc = svc.ContainerRegistry
		templateSvc = svc.Template
		dockerSvc = svc.Docker
		imageSvc = svc.Image
		imageUpdateSvc = svc.ImageUpdate
		buildSvc = svc.Build
		buildWorkspaceSvc = svc.BuildWorkspace
		volumeSvc = svc.Volume
		containerSvc = svc.Container
		networkSvc = svc.Network
		portSvc = svc.Port
		swarmSvc = svc.Swarm
		notificationSvc = svc.Notification
		updaterSvc = svc.Updater
		customizeSearchSvc = svc.CustomizeSearch
		systemSvc = svc.System
		systemUpgradeSvc = svc.SystemUpgrade
		gitRepositorySvc = svc.GitRepository
		gitOpsSyncSvc = svc.GitOpsSync
		webhookSvc = svc.Webhook
		vulnerabilitySvc = svc.Vulnerability
		dashboardSvc = svc.Dashboard
		roleSvc = svc.Role
		cfg = svc.Config
	}
	handlers.RegisterHealth(api)
	handlers.RegisterAuth(api, userSvc, authSvc, oidcSvc)
	handlers.RegisterApiKeys(api, apiKeySvc)
	handlers.RegisterRoles(api, roleSvc)
	handlers.RegisterAppImages(api, appImagesSvc)
	handlers.RegisterUsers(api, userSvc, authSvc)
	handlers.RegisterProjects(api, projectSvc, activitySvc)
	handlers.RegisterVersion(api, versionSvc)
	handlers.RegisterEvents(api, eventSvc, apiKeySvc)
	handlers.RegisterActivities(api, activitySvc, environmentSvc)
	handlers.RegisterOidc(api, authSvc, oidcSvc, roleSvc, userSvc, cfg)
	handlers.RegisterEnvironments(api, environmentSvc, settingsSvc, apiKeySvc, eventSvc, cfg)
	handlers.RegisterContainerRegistries(api, containerRegistrySvc, environmentSvc)
	handlers.RegisterTemplates(api, templateSvc, environmentSvc)
	handlers.RegisterImages(api, dockerSvc, imageSvc, imageUpdateSvc, settingsSvc, buildSvc, activitySvc)
	handlers.RegisterBuildWorkspaces(api, buildWorkspaceSvc)
	handlers.RegisterImageUpdates(api, imageUpdateSvc, imageSvc)
	handlers.RegisterSettings(api, settingsSvc, settingsSearchSvc, environmentSvc, cfg)
	handlers.RegisterJobSchedules(api, jobScheduleSvc, environmentSvc)
	handlers.RegisterVolumes(api, dockerSvc, volumeSvc, activitySvc)
	handlers.RegisterContainers(api, containerSvc, dockerSvc, settingsSvc, activitySvc)
	handlers.RegisterPorts(api, portSvc)
	handlers.RegisterNetworks(api, networkSvc, dockerSvc, activitySvc)
	handlers.RegisterSwarm(api, swarmSvc, environmentSvc, eventSvc, cfg)
	handlers.RegisterNotifications(api, notificationSvc, cfg)
	handlers.RegisterUpdater(api, updaterSvc)
	handlers.RegisterCustomize(api, customizeSearchSvc)
	handlers.RegisterSystem(api, dockerSvc, systemSvc, systemUpgradeSvc, cfg, activitySvc)
	handlers.RegisterGitRepositories(api, gitRepositorySvc)
	handlers.RegisterGitOpsSyncs(api, gitOpsSyncSvc)
	handlers.RegisterWebhooks(api, webhookSvc)
	handlers.RegisterVulnerability(api, vulnerabilitySvc)
	handlers.RegisterDashboard(api, dashboardSvc)
}
