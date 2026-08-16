package api

import (
	"context"
	"encoding/json/v2"
	"io"
	"maps"
	"net/http"
	"reflect"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/appimages"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/federated"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/health"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job"
	"github.com/getarcaneapp/arcane/backend/v2/internal/network"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/oidc"
	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/port"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/systembackup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/variable"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/backend/v2/api/handlers"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/dashboard"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/webhook"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
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
// registerHandlersInternal, and the authentication middleware.
type HandlerDeps struct {
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
	BuildWorkspace    *build.BuildWorkspaceService
	Volume            *volume.Module
	S3Destination     *s3domain.Module
	SystemBackup      *systembackup.Module
	Network           *network.NetworkService
	Port              *port.PortService
	Swarm             *swarm.Module
	ImageUpdate       *imageupdate.Module
	Auth              *auth.Module
	Passkey           *passkey.PasskeyService
	Oidc              *oidc.OidcService
	Docker            *docker.DockerClientService
	Template          *template.Module
	ContainerRegistry *registry.Module
	System            *system.Module
	SystemUpgrade     *system.SystemUpgradeService
	Diagnostics       *diagnostics.DiagnosticsService
	Updater           *updater.Module
	Event             *event.Module
	Activity          *activity.Module
	Version           *version.VersionService
	Notification      *notification.Module
	ApiKey            *apikey.Module
	Federated         *federated.FederatedCredentialService
	GitRepository     *gitrepo.Module
	GitOpsSync        *gitops.Module
	Webhook           *webhook.Module
	Vulnerability     *vulnerability.Module
	Dashboard         *dashboard.Module
	Role              *role.Module
	Variable          *variable.Module
	Upload            *upload.Module
}

// SetupAPI creates and configures the Huma API attached to the Echo router.
func SetupAPI(e *echo.Echo, apiGroup *echo.Group, appCtx handlerutil.ActivityAppContext, cfg *config.Config, deps HandlerDeps) huma.API {
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
	api.UseMiddleware(auth.NewHumaMiddleware(api, deps.Auth.Service(), deps.ApiKey.Service(), deps.Role.Service(), deps.Environment.Service(), cfg))
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
	registerHandlersInternal(api, HandlerDeps{}, handlerutil.NewActivityAppContext(context.Background()), nil)

	return api
}

// registerHandlersInternal registers all Huma-based API handlers.
func registerHandlersInternal(api huma.API, deps HandlerDeps, handlerAppCtx handlerutil.ActivityAppContext, cfg *config.Config) {
	health.RegisterRoutes(api)
	deps.Auth.RegisterRoutes(api)
	passkey.RegisterPasskeys(api, deps.Passkey, deps.Auth.Service(), deps.User.Service())
	deps.ApiKey.RegisterRoutes(api)
	federated.RegisterFederatedCredentials(api, deps.Federated)
	deps.Role.RegisterRoutes(api)
	appimages.RegisterAppImages(api, deps.AppImages)
	deps.User.RegisterRoutes(api)
	deps.Project.RegisterRoutes(api, handlerAppCtx)
	version.RegisterVersion(api, deps.Version)
	deps.Event.RegisterRoutes(api)
	deps.Activity.RegisterRoutes(api)
	oidc.RegisterOidc(api, deps.Auth.Service(), deps.Passkey, deps.Oidc, deps.Role.Service(), deps.User.Service(), cfg)
	deps.Environment.RegisterRoutes(api)
	deps.ContainerRegistry.RegisterRoutes(api)
	deps.Template.RegisterRoutes(api)
	deps.Variable.RegisterRoutes(api, cfg)
	deps.Image.RegisterRoutes(api, handlerAppCtx)
	deps.Upload.RegisterRoutes(api)
	build.RegisterBuildWorkspaces(api, deps.BuildWorkspace, deps.Upload.Service())
	deps.ImageUpdate.RegisterRoutes(api, handlerAppCtx)
	deps.Settings.RegisterRoutes(api)
	deps.S3Destination.RegisterRoutes(api)
	deps.SystemBackup.RegisterRoutes(api, handlerAppCtx)
	deps.JobSchedule.RegisterRoutes(api)
	deps.Volume.RegisterRoutes(api, handlerAppCtx)
	deps.Container.RegisterRoutes(api, handlerAppCtx)
	port.RegisterPorts(api, deps.Port)
	network.RegisterNetworks(api, deps.Network, deps.Docker, deps.Activity.Service(), handlerAppCtx)
	deps.Swarm.RegisterRoutes(api)
	deps.Notification.RegisterRoutes(api)
	deps.Updater.RegisterRoutes(api, handlerAppCtx)
	deps.Search.RegisterRoutes(api)
	deps.System.RegisterRoutes(api, handlerAppCtx)
	handlers.RegisterDiagnostics(api, deps.Diagnostics)
	deps.GitRepository.RegisterRoutes(api)
	deps.GitOpsSync.RegisterRoutes(api)
	deps.Webhook.RegisterRoutes(api)
	deps.Vulnerability.RegisterRoutes(api, handlerAppCtx)
	deps.Dashboard.RegisterRoutes(api)
	handlers.RegisterStream(api, deps.Dashboard.Handler(), deps.Activity.Handler(), deps.Environment.Handler())
}
