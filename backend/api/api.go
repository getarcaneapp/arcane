package api

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"io"
	"maps"
	"net/http"
	"path"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v5"
	kit "go.getarcane.app/kit/pkg"
	"go.uber.org/fx"

	"github.com/getarcaneapp/arcane/backend/v2/api/handlers"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apns"
	"github.com/getarcaneapp/arcane/backend/v2/internal/appimages"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/dashboard"
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
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/network"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/oidc"
	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/port"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/variable"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/internal/webhook"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

const (
	arcaneTypesPrefix = "github.com/getarcaneapp/arcane/types/v2/"
	dockerSDKPrefix   = "github.com/moby/moby"

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
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`
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

// customSchemaNamer prefixes Arcane and Docker type schemas with their package name
// so same-named types (e.g. image.Summary vs env.Summary) do not collide.
func customSchemaNamer(t reflect.Type, hint string) string {
	name := huma.DefaultSchemaNamer(t, hint)
	typeStr := t.String()
	elem := t
	for elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}
	pkgPath := elem.PkgPath()
	pkgName := path.Base(pkgPath)
	shortPkg, _, _ := strings.Cut(typeStr, ".")
	isArcaneType := strings.HasPrefix(pkgPath, arcaneTypesPrefix)

	if isArcaneType {
		name = kit.Capitalize(pkgName) + name
	} else {
		prefix := dockerSchemaPrefixes[shortPkg]
		if strings.Contains(pkgPath, dockerSDKPrefix) {
			prefix = cmp.Or(dockerSchemaPrefixes[pkgName], prefix)
		}
		name = prefix + name
	}
	// Preserve the published name after moving the volume backup response to types.
	if pkgPath == arcaneTypesPrefix+"volume" && strings.TrimLeft(typeStr, "*") == "volume.Backup" {
		name = "DockerVolumeVolumeBackup"
	}

	// Qualify Arcane generic arguments with their package unless it matches the outer type's package.
	openBracket := strings.IndexByte(typeStr, '[')
	if !isArcaneType || openBracket < 0 {
		return name
	}
	anonymous := reflect.TypeFor[struct{}]()
	schemaPrefix, ok := strings.CutSuffix(name, huma.DefaultSchemaNamer(anonymous, typeStr))
	if !ok {
		return name
	}

	rewrittenTypeName := typeStr
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

		innerPackage := path.Base(afterPrefix[:separator])
		replacement := kit.Ternary(innerPackage != pkgName, kit.Capitalize(innerPackage), "")
		argumentTypeIndex := prefixIndex + len(arcaneTypesPrefix) + separator + 1
		rewrittenTypeName = rewrittenTypeName[:prefixIndex] + replacement + rewrittenTypeName[argumentTypeIndex:]
		searchOffset = prefixIndex + len(replacement)
	}

	return schemaPrefix + huma.DefaultSchemaNamer(anonymous, rewrittenTypeName)
}

// HandlerDeps contains the services required to register HTTP API handlers.
// It contains only dependencies consumed by SetupAPI and the authentication middleware.
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
	Volume            *volume.Module
	S3Destination     *s3.Module
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
	humaConfig.Servers = []*huma.Server{{URL: cfg.AppUrl + "/api"}}
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
	humaConfig.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", customSchemaNamer)

	api := humaecho.NewWithGroup(e, apiGroup, humaConfig)

	api.UseMiddleware(auth.NewHumaMiddleware(api, deps.Auth.Service(), deps.ApiKey.Service(), deps.Role.Service(), deps.Environment.Service(), cfg))
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		if actor, ok := userctx.CurrentUserFromContext(ctx.Context()); ok {
			middleware.RecordAuthenticatedRequest(ctx.Context(), humaecho.Unwrap(ctx).Request().Header, actor)
		}
		next(ctx)
	})
	api.UseMiddleware(middleware.NewActivityBatchID())
	registerNormalization(api)

	health.RegisterRoutes(api)
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
	deps.Dashboard.RegisterRoutes(api)
	handlers.RegisterStream(api, deps.Dashboard.Handler(), deps.Activity.Handler(), deps.Environment.Handler(), deps.Event.Service(), deps.Version)

	apiGroup.GET("/docs", func(c *echo.Context) error {
		return c.HTML(http.StatusOK, scalarDocsHTML)
	})

	return api
}

// SetupAPIForSpec creates a Huma API instance for OpenAPI spec generation only.
// No services are required - this is purely for schema generation.
func SetupAPIForSpec() huma.API {
	e := echo.New()
	appCtx := handlerutil.NewActivityAppContext(context.Background()) //nolint:forbidigo // Schema discovery has no running request or application lifecycle.
	return SetupAPI(e, e.Group("/api"), appCtx, &config.Config{}, HandlerDeps{})
}
