package api

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/backend/v2/api/handlers"
	"github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/di"
	"github.com/labstack/echo/v4"
)

const (
	arcaneTypesPrefix = "github.com/getarcaneapp/arcane/types/v2/"
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
// from github.com/getarcaneapp/arcane/types/v2 to avoid conflicts between packages that have
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

// SetupAPI creates and configures the Huma API attached to the Echo router.
func SetupAPI(e *echo.Echo, apiGroup *echo.Group, appCtx handlers.ActivityAppContext, cfg *config.Config, svc *di.Services) huma.API {
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
	registerHandlersInternal(api, svc, appCtx, cfg)

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
	registerHandlersInternal(api, nil, handlers.NewActivityAppContext(context.Background()), nil)

	return api
}

// registerHandlers registers all Huma-based API handlers.
// Add new handlers here as they are migrated from Gin.
func registerHandlersInternal(api huma.API, svc *di.Services, handlerAppCtx handlers.ActivityAppContext, cfg *config.Config) {
	// svc is nil during OpenAPI spec generation (SetupAPIForSpec); an empty
	// container keeps every field a true-nil pointer so handler nil-guards hold.
	if svc == nil {
		svc = &di.Services{}
	}
	handlers.RegisterHealth(api)
	handlers.RegisterAuth(api, svc.User, svc.Auth, svc.Oidc)
	handlers.RegisterApiKeys(api, svc.ApiKey)
	handlers.RegisterFederatedCredentials(api, svc.Federated)
	handlers.RegisterRoles(api, svc.Role)
	handlers.RegisterAppImages(api, svc.AppImages)
	handlers.RegisterUsers(api, svc.User, svc.Auth)
	handlers.RegisterProjects(api, svc.Project, svc.Activity, handlerAppCtx)
	handlers.RegisterVersion(api, svc.Version)
	handlers.RegisterEvents(api, svc.Event)
	handlers.RegisterActivities(api, svc.Activity, svc.Environment)
	handlers.RegisterOidc(api, svc.Auth, svc.Oidc, svc.Role, svc.User, cfg)
	handlers.RegisterEnvironments(api, svc.Environment, svc.Settings, svc.ApiKey, svc.Event, cfg)
	handlers.RegisterContainerRegistries(api, svc.ContainerRegistry, svc.Environment)
	handlers.RegisterTemplates(api, svc.Template, svc.Environment)
	handlers.RegisterImages(api, svc.Docker, svc.Image, svc.ImageUpdate, svc.Settings, svc.Build, svc.Activity, handlerAppCtx)
	handlers.RegisterBuildWorkspaces(api, svc.BuildWorkspace)
	handlers.RegisterImageUpdates(api, svc.ImageUpdate, svc.Image, handlerAppCtx)
	handlers.RegisterSettings(api, svc.Settings, svc.SettingsSearch, svc.Environment, cfg)
	handlers.RegisterJobSchedules(api, svc.JobSchedule, svc.Environment)
	handlers.RegisterVolumes(api, svc.Docker, svc.Volume, svc.Activity, handlerAppCtx)
	handlers.RegisterContainers(api, svc.Container, svc.Docker, svc.Settings, svc.Activity, handlerAppCtx)
	handlers.RegisterPorts(api, svc.Port)
	handlers.RegisterNetworks(api, svc.Network, svc.Docker, svc.Activity, handlerAppCtx)
	handlers.RegisterSwarm(api, svc.Swarm, svc.Environment, svc.Event, cfg)
	handlers.RegisterNotifications(api, svc.Notification, cfg)
	handlers.RegisterUpdater(api, svc.Updater, handlerAppCtx)
	handlers.RegisterCustomize(api, svc.CustomizeSearch)
	handlers.RegisterSystem(api, svc.Docker, svc.System, svc.SystemUpgrade, svc.Environment, cfg, svc.Activity, handlerAppCtx)
	handlers.RegisterDiagnostics(api, svc.Diagnostics)
	handlers.RegisterGitRepositories(api, svc.GitRepository)
	handlers.RegisterGitOpsSyncs(api, svc.GitOpsSync)
	handlers.RegisterWebhooks(api, svc.Webhook)
	handlers.RegisterVulnerability(api, svc.Vulnerability, handlerAppCtx)
	handlers.RegisterDashboard(api, svc.Dashboard, svc.Environment)
}
