package api

import (
	"context"
	json "encoding/json/v2"
	"io"
	"maps"
	"net/http"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/backend/v2/api/handlers"
	"github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"
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

var jsonV2Format = huma.Format{
	Marshal: func(writer io.Writer, value any) error {
		return json.MarshalWrite(writer, value, jsonV2APIOptions)
	},
	Unmarshal: func(data []byte, value any) error {
		return json.Unmarshal(data, value, jsonV2APIOptions)
	},
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
	return qualifyGenericArcaneArgumentsInternal(pkgPath, typeStr, name)
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

	return strings.ToUpper(pkg[:1]) + pkg[1:], true
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

func qualifyGenericArcaneArgumentsInternal(pkgPath, typeName, schemaName string) string {
	openBracket := strings.IndexByte(typeName, '[')
	if !strings.HasPrefix(pkgPath, arcaneTypesPrefix) || openBracket < 0 {
		return schemaName
	}

	outerPackage := strings.TrimPrefix(pkgPath, arcaneTypesPrefix)
	if separator := strings.LastIndexByte(outerPackage, '/'); separator >= 0 {
		outerPackage = outerPackage[separator+1:]
	}

	plainSchemaName := huma.DefaultSchemaNamer(reflect.TypeFor[struct{}](), typeName)
	schemaPrefix, ok := strings.CutSuffix(schemaName, plainSchemaName)
	if !ok {
		return schemaName
	}

	rewrittenTypeName := typeName
	searchOffset := openBracket + 1
	for {
		prefixIndex := strings.Index(rewrittenTypeName[searchOffset:], arcaneTypesPrefix)
		if prefixIndex == -1 {
			break
		}
		prefixIndex += searchOffset

		afterPrefix := rewrittenTypeName[prefixIndex+len(arcaneTypesPrefix):]
		separator := strings.IndexByte(afterPrefix, '.')
		if separator < 0 {
			break
		}

		innerPackage := afterPrefix[:separator]
		if nestedSeparator := strings.LastIndexByte(innerPackage, '/'); nestedSeparator >= 0 {
			innerPackage = innerPackage[nestedSeparator+1:]
		}

		replacement := ""
		if innerPackage != outerPackage {
			replacement = strings.ToUpper(innerPackage[:1]) + innerPackage[1:]
		}

		argumentTypeIndex := prefixIndex + len(arcaneTypesPrefix) + separator + 1
		rewrittenTypeName = rewrittenTypeName[:prefixIndex] + replacement + rewrittenTypeName[argumentTypeIndex:]
		searchOffset = prefixIndex + len(replacement)
	}

	return schemaPrefix + huma.DefaultSchemaNamer(reflect.TypeFor[struct{}](), rewrittenTypeName)
}

// HandlerDeps contains the services required to register HTTP API handlers.
//
// It intentionally contains only dependencies consumed by SetupAPI,
// registerHandlersInternal, and the authentication bridge.
type HandlerDeps struct {
	fx.In

	AppImages         *services.ApplicationImagesService
	User              *services.UserService
	Project           *services.ProjectService
	Environment       *services.EnvironmentService
	Settings          *services.SettingsService
	JobSchedule       *services.JobService
	SettingsSearch    *services.SettingsSearchService
	CustomizeSearch   *services.CustomizeSearchService
	Container         *services.ContainerService
	Image             *services.ImageService
	Build             *services.BuildService
	BuildWorkspace    *services.BuildWorkspaceService
	Volume            *services.VolumeService
	Network           *services.NetworkService
	Port              *services.PortService
	Swarm             *services.SwarmService
	ImageUpdate       *services.ImageUpdateService
	Auth              *services.AuthService
	Oidc              *services.OidcService
	Docker            *services.DockerClientService
	Template          *services.TemplateService
	ContainerRegistry *services.ContainerRegistryService
	System            *services.SystemService
	SystemUpgrade     *services.SystemUpgradeService
	Diagnostics       *services.DiagnosticsService
	Updater           *services.UpdaterService
	Event             *services.EventService
	Activity          *services.ActivityService
	Version           *services.VersionService
	Notification      *services.NotificationService
	ApiKey            *services.ApiKeyService
	Federated         *services.FederatedCredentialService
	GitRepository     *services.GitRepositoryService
	GitOpsSync        *services.GitOpsSyncService
	Webhook           *services.WebhookService
	Vulnerability     *services.VulnerabilityService
	Dashboard         *services.DashboardService
	Role              *services.RoleService
	Variable          *services.VariableService
}

// SetupAPI creates and configures the Huma API attached to the Echo router.
func SetupAPI(e *echo.Echo, apiGroup *echo.Group, appCtx handlers.ActivityAppContext, cfg *config.Config, deps HandlerDeps) huma.API {
	e.JSONSerializer = jsonV2Serializer{}

	humaConfig := huma.DefaultConfig("Arcane API", config.Version)
	humaConfig.Formats = maps.Clone(humaConfig.Formats)
	humaConfig.Formats["application/json"] = jsonV2Format
	humaConfig.Formats["json"] = jsonV2Format
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
	api.UseMiddleware(middleware.NewAuthBridge(api, deps.Auth, deps.ApiKey, deps.Role, deps.Environment, cfg))
	api.UseMiddleware(middleware.NewActivityBatchID())

	// Register all Huma handlers
	registerHandlersInternal(api, deps, appCtx, cfg)

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
	apiGroup.GET("/docs", func(c *echo.Context) error {
		return c.HTML(http.StatusOK, scalarDocsHTML)
	})
}

// SetupAPIForSpec creates a Huma API instance for OpenAPI spec generation only.
// No services are required - this is purely for schema generation.
func SetupAPIForSpec() huma.API {
	e := echo.New()
	apiGroup := e.Group("/api")

	humaConfig := huma.DefaultConfig("Arcane API", config.Version)
	humaConfig.Formats = maps.Clone(humaConfig.Formats)
	humaConfig.Formats["application/json"] = jsonV2Format
	humaConfig.Formats["json"] = jsonV2Format
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

	// Register handlers with zero-value dependencies for schema discovery only.
	registerHandlersInternal(api, HandlerDeps{}, handlers.NewActivityAppContext(context.Background()), nil)

	return api
}

// registerHandlers registers all Huma-based API handlers.
// Add new handlers here as they are migrated from Gin.
func registerHandlersInternal(api huma.API, deps HandlerDeps, handlerAppCtx handlers.ActivityAppContext, cfg *config.Config) {
	handlers.RegisterHealth(api)
	handlers.RegisterAuth(api, deps.User, deps.Auth, deps.Oidc, deps.Settings)
	handlers.RegisterApiKeys(api, deps.ApiKey)
	handlers.RegisterFederatedCredentials(api, deps.Federated)
	handlers.RegisterRoles(api, deps.Role)
	handlers.RegisterAppImages(api, deps.AppImages)
	handlers.RegisterUsers(api, deps.User, deps.Auth)
	handlers.RegisterProjects(api, deps.Project, deps.Activity, handlerAppCtx)
	handlers.RegisterVersion(api, deps.Version)
	handlers.RegisterEvents(api, deps.Event)
	handlers.RegisterActivities(api, deps.Activity, deps.Environment)
	handlers.RegisterOidc(api, deps.Auth, deps.Oidc, deps.Role, deps.User, cfg)
	handlers.RegisterEnvironments(api, deps.Environment, deps.Settings, deps.ApiKey, deps.Event, cfg)
	handlers.RegisterContainerRegistries(api, deps.ContainerRegistry, deps.Environment)
	handlers.RegisterTemplates(api, deps.Template)
	if cfg != nil && cfg.AgentMode {
		handlers.RegisterMaterializedVariables(api, deps.Variable, deps.Environment)
	} else {
		handlers.RegisterVariables(api, deps.Variable, deps.Environment)
	}
	handlers.RegisterImages(api, deps.Docker, deps.Image, deps.ImageUpdate, deps.Settings, deps.Build, deps.Activity, handlerAppCtx)
	handlers.RegisterBuildWorkspaces(api, deps.BuildWorkspace)
	handlers.RegisterImageUpdates(api, deps.ImageUpdate, deps.Image, handlerAppCtx)
	handlers.RegisterSettings(api, deps.Settings, deps.SettingsSearch, deps.Environment, cfg)
	handlers.RegisterJobSchedules(api, deps.JobSchedule, deps.Environment)
	handlers.RegisterVolumes(api, deps.Docker, deps.Volume, deps.Activity, handlerAppCtx)
	handlers.RegisterContainers(api, deps.Container, deps.Docker, deps.Settings, deps.Activity, handlerAppCtx)
	handlers.RegisterPorts(api, deps.Port)
	handlers.RegisterNetworks(api, deps.Network, deps.Docker, deps.Activity, handlerAppCtx)
	handlers.RegisterSwarm(api, deps.Swarm, deps.Environment, deps.Event, cfg)
	handlers.RegisterNotifications(api, deps.Notification, cfg)
	handlers.RegisterUpdater(api, deps.Updater, handlerAppCtx)
	handlers.RegisterCustomize(api, deps.CustomizeSearch)
	handlers.RegisterSystem(api, deps.Docker, deps.System, deps.SystemUpgrade, deps.Environment, cfg, deps.Activity, handlerAppCtx)
	handlers.RegisterDiagnostics(api, deps.Diagnostics)
	handlers.RegisterGitRepositories(api, deps.GitRepository)
	handlers.RegisterGitOpsSyncs(api, deps.GitOpsSync)
	handlers.RegisterWebhooks(api, deps.Webhook)
	handlers.RegisterVulnerability(api, deps.Vulnerability, handlerAppCtx)
	handlers.RegisterDashboard(api, deps.Dashboard, deps.Environment)
}
