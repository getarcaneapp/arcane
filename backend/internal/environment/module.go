package environment

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	Module struct {
		service *EnvironmentService
		handler *EnvironmentHandler
	}

	Params struct {
		fx.In

		Service         *EnvironmentService
		SettingsService *settings.SettingsService
		APIKey          *apikey.ApiKeyService
		EventService    *event.EventService
		Config          *config.Config
		MTLS            *agent.MTLS
		ActivityService activity.Service
	}
)

func New(p Params) *Module {
	return &Module{
		service: p.Service,
		handler: NewHandler(HandlerParams{
			EnvironmentService: p.Service,
			SettingsService:    p.SettingsService,
			APIKeyService:      p.APIKey,
			EventService:       p.EventService,
			Config:             p.Config,
			MTLS:               p.MTLS,
			ActivityService:    p.ActivityService,
		}),
	}
}

func (m *Module) Service() *EnvironmentService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) Handler() *EnvironmentHandler {
	if m == nil {
		return nil
	}
	return m.handler
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext, localVersion func(context.Context) *types.VersionInfo) {
	if m == nil {
		RegisterEnvironments(api, NewHandler(HandlerParams{}))
		return
	}
	*m.handler = EnvironmentHandler{ //exhaustruct:enforce
		environmentService: m.handler.environmentService,
		settingsService:    m.handler.settingsService,
		apiKeyService:      m.handler.apiKeyService,
		eventService:       m.handler.eventService,
		cfg:                m.handler.cfg,
		mtls:               m.handler.mtls,
		streamHub:          m.handler.streamHub,
		changeHub:          m.handler.changeHub,
		changeCursors:      m.handler.changeCursors,
		activityService:    m.handler.activityService,
		appCtx:             appCtx.Context(),
		localVersion:       localVersion,
	}
	RegisterEnvironments(api, m.handler)
}

func RegisterEnvironments(api huma.API, h *EnvironmentHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "listEnvironments",
		Method:      "GET",
		Path:        "/agents",
		Summary:     "List environments",
		Description: "Get a paginated list of Docker environments",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.ListEnvironments)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "createEnvironment",
		Method:      "POST",
		Path:        "/agents",
		Summary:     "Create an environment",
		Description: "Create a new Docker environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsCreate, h.CreateEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getEnvironment",
		Method:      "GET",
		Path:        "/agents/{id}",
		Summary:     "Get an environment",
		Description: "Get a Docker environment by ID",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsRead, h.GetEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateEnvironment",
		Method:      "PATCH",
		Path:        "/agents/{id}",
		Summary:     "Update an environment",
		Description: "Update a Docker environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsUpdate, h.UpdateEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteEnvironment",
		Method:      "DELETE",
		Path:        "/agents/{id}",
		Summary:     "Delete an environment",
		Description: "Delete a Arcane environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsDelete, h.DeleteEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "testConnection",
		Method:      "POST",
		Path:        "/agents/{id}/test",
		Summary:     "Test environment connection",
		Description: "Test connectivity to a Arcane environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsRead, h.TestConnection)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "restartEnvironmentTunnel",
		Method:      "POST",
		Path:        "/agents/{id}/tunnel/restart",
		Summary:     "Restart environment agent tunnel",
		Description: "Close the active agent tunnel and wait for the agent to reconnect",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsRestart, h.RestartTunnel)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "syncEnvironment",
		Method:      "POST",
		Path:        "/agents/{id}/sync",
		Summary:     "Sync environment",
		Description: "Sync container registries, S3 destinations, and git repositories to a remote environment. Returns an error if any resource group fails; other groups may still sync successfully.",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsSync, h.SyncEnvironment)

	huma.Register(api, huma.Operation{
		OperationID:  "pairEnvironment",
		Method:       "POST",
		Path:         "/agents/pair",
		Summary:      "Pair agent with manager",
		Description:  "Agent sends API key to complete environment pairing",
		Tags:         []string{"Environments"},
		MaxBodyBytes: 1024,
		Security:     []map[string][]string{},
	}, h.PairEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getDeploymentSnippets",
		Method:      "POST",
		Path:        "/agents/{id}/deployment",
		Summary:     "Create deployment snippets",
		Description: "Mint a single-use enrollment token for the environment and return Docker run and compose snippets that use it",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.CreateDeploymentSnippets)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-enrollment-tokens",
		Method:      "GET",
		Path:        "/enrollment-tokens",
		Summary:     "List enrollment tokens",
		Tags:        []string{"Enrollment"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsCreate, h.ListEnrollmentTokens)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-enrollment-token",
		Method:      "POST",
		Path:        "/enrollment-tokens",
		Summary:     "Create an enrollment token",
		Description: "Create a token agents use to register themselves.",
		Tags:        []string{"Enrollment"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsCreate, h.CreateEnrollmentToken)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-enrollment-token-secret",
		Method:      "GET",
		Path:        "/enrollment-tokens/{id}/token",
		Summary:     "Get an enrollment token secret",
		Tags:        []string{"Enrollment"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsCreate, h.GetEnrollmentTokenSecret)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-enrollment-token",
		Method:      "DELETE",
		Path:        "/enrollment-tokens/{id}",
		Summary:     "Revoke an enrollment token",
		Tags:        []string{"Enrollment"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsCreate, h.DeleteEnrollmentToken)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "downloadEnvironmentMTLSBundle",
		Method:      "GET",
		Path:        "/agents/{id}/deployment/mtls/bundle",
		Summary:     "Download environment mTLS bundle",
		Description: "Download the generated mTLS client certificate bundle for an agent environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.DownloadEnvironmentMTLSBundle)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "downloadEnvironmentMTLSFile",
		Method:      "GET",
		Path:        "/agents/{id}/deployment/mtls/{fileName}",
		Summary:     "Download environment mTLS asset",
		Description: "Download an individual generated mTLS client certificate asset for an agent environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.DownloadEnvironmentMTLSFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getEnvironmentVersion",
		Method:      "GET",
		Path:        "/agents/{id}/version",
		Summary:     "Get environment version",
		Description: "Get the version of a remote environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermEnvironmentsRead, h.GetEnvironmentVersion)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "downloadAgentMTLSCA",
		Method:      "GET",
		Path:        "/agent/mtls/ca",
		Summary:     "Download Arcane-generated agent mTLS CA",
		Description: "Download the Arcane-managed certificate authority used for generated agent mTLS client certificates",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.DownloadAgentMTLSCA)
}
