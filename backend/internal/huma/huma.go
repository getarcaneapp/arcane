package huma

import (
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/huma/handlers"
	"github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
)

// customSchemaNamer creates unique schema names using package prefix for types
// from go.getarcane.app/types to avoid conflicts between packages that have
// types with the same name (e.g., image.Summary vs env.Summary).
func customSchemaNamer(t reflect.Type, hint string) string {
	name := huma.DefaultSchemaNamer(t, hint)

	// Get the package path - for non-pointer types this gives the full import path
	pkgPath := t.PkgPath()

	// For pointer types, get the element's package path
	if pkgPath == "" && t.Kind() == reflect.Ptr {
		pkgPath = t.Elem().PkgPath()
	}

	// Use type string representation for package identification
	// Format: "pkgname.TypeName" - we extract the pkgname part
	typeStr := t.String()
	var shortPkg string
	if dotIdx := strings.Index(typeStr, "."); dotIdx != -1 {
		shortPkg = typeStr[:dotIdx]
	}

	// For types from our types package, prefix with the package name
	if strings.HasPrefix(pkgPath, "go.getarcane.app/types/") {
		// Extract package name (e.g., "image" from "go.getarcane.app/types/image")
		parts := strings.Split(pkgPath, "/")
		if len(parts) > 0 {
			pkgName := parts[len(parts)-1]
			// Capitalize the package name and prefix it
			pkgName = strings.ToUpper(pkgName[:1]) + pkgName[1:]
			return pkgName + name
		}
	}

	// Handle Docker SDK types that have name conflicts
	// Docker has many packages with overlapping type names:
	// types.ServiceConfig vs registry.ServiceConfig
	// types.GenericResource vs swarm.GenericResource
	// etc.
	// We prefix ALL Docker types with their package name
	dockerPackages := map[string]string{
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

	// Check if this is a Docker type based on pkgPath
	if strings.Contains(pkgPath, "github.com/docker/docker") {
		// Extract the last part of the package path
		parts := strings.Split(pkgPath, "/")
		lastPart := parts[len(parts)-1]
		if prefix, ok := dockerPackages[lastPart]; ok {
			return prefix + name
		}
	}

	// Also check short package name from type string for nested/embedded types
	if prefix, ok := dockerPackages[shortPkg]; ok {
		return prefix + name
	}

	// Handle generic types like base.ApiResponse[T] where T is from go.getarcane.app/types
	// The name will be something like "BaseApiResponseUsageCounts" and we need to
	// differentiate based on the inner type's package
	if strings.HasPrefix(pkgPath, "go.getarcane.app/types/base") {
		// Check if this is a generic type by looking at string representation
		typeName := t.String()
		// For generics, Go's String() returns something like:
		// "base.ApiResponse[go.getarcane.app/types/volume.UsageCounts]"
		if strings.Contains(typeName, "[") && strings.Contains(typeName, "go.getarcane.app/types/") {
			// Extract the inner package name
			start := strings.Index(typeName, "go.getarcane.app/types/")
			if start != -1 {
				rest := typeName[start+len("go.getarcane.app/types/"):]
				end := strings.Index(rest, ".")
				if end != -1 {
					innerPkg := rest[:end]
					innerPkg = strings.ToUpper(innerPkg[:1]) + innerPkg[1:]
					// Insert the package name into the schema name
					// BaseApiResponseUsageCounts -> BaseApiResponseVolumeUsageCounts
					return strings.Replace(name, "UsageCounts", innerPkg+"UsageCounts", 1)
				}
			}
		}
	}

	return name
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
	Version           *services.VersionService
	Environment       *services.EnvironmentService
	Settings          *services.SettingsService
	SettingsSearch    *services.SettingsSearchService
	ContainerRegistry *services.ContainerRegistryService
	Template          *services.TemplateService
	Docker            *services.DockerClientService
	Image             *services.ImageService
	ImageUpdate       *services.ImageUpdateService
	Volume            *services.VolumeService
	Container         *services.ContainerService
	Network           *services.NetworkService
	Notification      *services.NotificationService
	Apprise           *services.AppriseService
	Updater           *services.UpdaterService
	CustomizeSearch   *services.CustomizeSearchService
	System            *services.SystemService
	SystemUpgrade     *services.SystemUpgradeService
	Config            *config.Config
}

// SetupAPI creates and configures the Huma API alongside the existing Gin router.
func SetupAPI(router *gin.Engine, apiGroup *gin.RouterGroup, cfg *config.Config, svc *Services) huma.API {
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

	// Use custom schema namer to avoid conflicts between types with same name
	// from different packages (e.g., image.Summary vs env.Summary)
	humaConfig.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", customSchemaNamer)

	// Create Huma API wrapping the Gin router group
	api := humagin.NewWithGroup(router, apiGroup, humaConfig)

	// Add authentication middleware
	api.UseMiddleware(middleware.NewAuthBridge(svc.Auth, cfg))

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
func registerScalarDocs(apiGroup *gin.RouterGroup) {
	apiGroup.GET("/docs", func(c *gin.Context) {
		c.Header("Content-Type", "text/html")
		c.String(200, scalarDocsHTML)
	})
}

// SetupAPIForSpec creates a Huma API instance for OpenAPI spec generation only.
// No services are required - this is purely for schema generation.
func SetupAPIForSpec() huma.API {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	apiGroup := router.Group("/api")

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

	// Use custom schema namer to avoid conflicts between types with same name
	humaConfig.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", customSchemaNamer)

	api := humagin.NewWithGroup(router, apiGroup, humaConfig)

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
	var versionSvc *services.VersionService
	var environmentSvc *services.EnvironmentService
	var settingsSvc *services.SettingsService
	var settingsSearchSvc *services.SettingsSearchService
	var containerRegistrySvc *services.ContainerRegistryService
	var templateSvc *services.TemplateService
	var dockerSvc *services.DockerClientService
	var imageSvc *services.ImageService
	var imageUpdateSvc *services.ImageUpdateService
	var volumeSvc *services.VolumeService
	var containerSvc *services.ContainerService
	var networkSvc *services.NetworkService
	var notificationSvc *services.NotificationService
	var appriseSvc *services.AppriseService
	var updaterSvc *services.UpdaterService
	var customizeSearchSvc *services.CustomizeSearchService
	var systemSvc *services.SystemService
	var systemUpgradeSvc *services.SystemUpgradeService
	var cfg *config.Config

	if svc != nil {
		userSvc = svc.User
		authSvc = svc.Auth
		oidcSvc = svc.Oidc
		apiKeySvc = svc.ApiKey
		appImagesSvc = svc.AppImages
		projectSvc = svc.Project
		eventSvc = svc.Event
		versionSvc = svc.Version
		environmentSvc = svc.Environment
		settingsSvc = svc.Settings
		settingsSearchSvc = svc.SettingsSearch
		containerRegistrySvc = svc.ContainerRegistry
		templateSvc = svc.Template
		dockerSvc = svc.Docker
		imageSvc = svc.Image
		imageUpdateSvc = svc.ImageUpdate
		volumeSvc = svc.Volume
		containerSvc = svc.Container
		networkSvc = svc.Network
		notificationSvc = svc.Notification
		appriseSvc = svc.Apprise
		updaterSvc = svc.Updater
		customizeSearchSvc = svc.CustomizeSearch
		systemSvc = svc.System
		systemUpgradeSvc = svc.SystemUpgrade
		cfg = svc.Config
	}
	handlers.RegisterHealth(api)
	handlers.RegisterAuth(api, userSvc, authSvc, oidcSvc)
	handlers.RegisterApiKeys(api, apiKeySvc)
	handlers.RegisterAppImages(api, appImagesSvc)
	handlers.RegisterProjects(api, projectSvc)
	handlers.RegisterUsers(api, userSvc)
	handlers.RegisterVersion(api, versionSvc)
	handlers.RegisterEvents(api, eventSvc)
	handlers.RegisterOidc(api, authSvc, oidcSvc, cfg)
	handlers.RegisterEnvironments(api, environmentSvc, settingsSvc, cfg)
	handlers.RegisterContainerRegistries(api, containerRegistrySvc)
	handlers.RegisterTemplates(api, templateSvc)
	handlers.RegisterImages(api, dockerSvc, imageSvc, imageUpdateSvc, settingsSvc)
	handlers.RegisterImageUpdates(api, imageUpdateSvc)
	handlers.RegisterSettings(api, settingsSvc, settingsSearchSvc, cfg)
	handlers.RegisterVolumes(api, dockerSvc, volumeSvc)
	handlers.RegisterContainers(api, containerSvc, dockerSvc)
	handlers.RegisterNetworks(api, networkSvc, dockerSvc)
	handlers.RegisterNotifications(api, notificationSvc, appriseSvc)
	handlers.RegisterUpdater(api, updaterSvc)
	handlers.RegisterCustomize(api, customizeSearchSvc)
	handlers.RegisterSystem(api, dockerSvc, systemSvc, systemUpgradeSvc, cfg)
}
