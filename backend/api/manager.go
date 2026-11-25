package api

import (
	"io/fs"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/api/handlers"
	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/apns"
	"getarcane.dev/arcane/v3/internal/appimages"
	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/enrollment"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/federated"
	"getarcane.dev/arcane/v3/internal/ldap"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/notification"
	"getarcane.dev/arcane/v3/internal/oidc"
	"getarcane.dev/arcane/v3/internal/passkey"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/telemetry"
	"getarcane.dev/arcane/v3/internal/template"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/internal/webhook"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/resources"
)

type ManagerHandlerDeps struct {
	fx.In

	AppImages    *appimages.ApplicationImagesService
	User         *user.Module
	Environment  *environment.Module
	Enrollment   *enrollment.Module
	Auth         *auth.Module
	AuthTokens   *auth.TokenCache
	Passkey      *passkey.PasskeyService
	Oidc         *oidc.OidcService
	Template     *template.Module
	Notification *notification.Module
	ApiKey       *apikey.Module
	Apns         *apns.Module
	Federated    *federated.FederatedCredentialService
	Webhook      *webhook.Module
	Role         *role.Module
	Ldap         *ldap.Module
}

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
    <script src="/api/docs/scalar.js"></script>
  </body>
</html>`

func registerManagerRoutes(api huma.API, p SetupParams) {
	p.ManagerDeps.Auth.RegisterRoutes(api)
	passkey.RegisterPasskeys(api, p.ManagerDeps.Passkey, p.Deps.AuthService, p.ManagerDeps.AuthTokens, p.ManagerDeps.User.Service())
	p.ManagerDeps.ApiKey.RegisterRoutes(api)
	federated.RegisterFederatedCredentials(api, p.ManagerDeps.Federated)
	p.ManagerDeps.Role.RegisterRoutes(api)
	appimages.RegisterAppImages(api, p.ManagerDeps.AppImages)
	p.ManagerDeps.User.RegisterRoutes(api)
	oidc.RegisterOidc(api, p.Deps.AuthService, p.ManagerDeps.Passkey, p.ManagerDeps.Oidc, p.Deps.RoleService, p.ManagerDeps.User.Service(), p.Config)
	p.ManagerDeps.Environment.RegisterRoutes(api, p.AppContext, p.Deps.Version.GetAppVersionInfo)
	p.ManagerDeps.Enrollment.RegisterRoutes(api, p.Config)
	p.ManagerDeps.Template.RegisterRoutes(api)
	p.ManagerDeps.Notification.RegisterRoutes(api)
	p.ManagerDeps.Apns.RegisterRoutes(api)
	p.ManagerDeps.Webhook.RegisterRoutes(api, p.AppContext)
	handlers.RegisterStream(handlers.StreamParams{
		API:                api,
		ActivityHandler:    p.Deps.Activity.Handler(),
		EnvironmentHandler: p.ManagerDeps.Environment.Handler(),
		EventService:       p.Deps.Event.Service(),
		VersionService:     p.Deps.Version,
		SystemService:      p.Deps.System.Service(),
	})
	telemetry.RegisterRoutes(api, telemetry.NewService())
	p.ManagerDeps.Ldap.RegisterRoutes(api)
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
		if _, err := fs.Stat(resources.FS, "scalar/standalone.js"); err == nil {
			return c.FileFS("scalar/standalone.js", resources.FS)
		}
		if _, err := os.Stat("resources/scalar/standalone.js"); err == nil {
			return c.File("resources/scalar/standalone.js")
		}
		return c.Redirect(http.StatusTemporaryRedirect, "https://cdn.jsdelivr.net/npm/@scalar/api-reference")
	}))
}
