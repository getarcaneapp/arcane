// Package environment owns environment persistence, remote runtime state, pairing,
// synchronization, and its HTTP and stream surfaces.
package environment

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/version"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type Module struct {
	service *EnvironmentService
	handler *EnvironmentHandler
}

func New(
	service *EnvironmentService,
	settingsService *settings.SettingsService,
	apiKey *apikey.ApiKeyService,
	eventService *event.EventService,
	cfg *config.Config,
	activityService activity.Service,
) *Module {
	return &Module{
		service: service,
		handler: NewHandler(service, settingsService, apiKey, eventService, cfg, activityService),
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

// RegisterRoutes registers the environment endpoints; localVersion answers version requests for the local environment.
func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext, localVersion func(context.Context) *version.Info) {
	if m == nil {
		RegisterEnvironments(api, NewHandler(nil, nil, nil, nil, nil, nil))
		return
	}
	m.handler.appCtx = appCtx.Context()
	m.handler.localVersion = localVersion
	RegisterEnvironments(api, m.handler)
}

// RegisterEnvironments registers all environment management endpoints.
func RegisterEnvironments(api huma.API, h *EnvironmentHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "listEnvironments",
		Method:      "GET",
		Path:        "/environments",
		Summary:     "List environments",
		Description: "Get a paginated list of Docker environments",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		// No global PermEnvironmentsList gate: this endpoint also backs the
		// environment switcher, so any authenticated caller may list. The handler
		// filters the result to the environments the caller can actually access.
		// Management mutations (create/update/delete) remain global-gated below.
	}, h.ListEnvironments)

	huma.Register(api, huma.Operation{
		OperationID: "createEnvironment",
		Method:      "POST",
		Path:        "/environments",
		Summary:     "Create an environment",
		Description: "Create a new Docker environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsCreate),
	}, h.CreateEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "getEnvironment",
		Method:      "GET",
		Path:        "/environments/{id}",
		Summary:     "Get an environment",
		Description: "Get a Docker environment by ID",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsRead),
	}, h.GetEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "updateEnvironment",
		Method:      "PUT",
		Path:        "/environments/{id}",
		Summary:     "Update an environment",
		Description: "Update a Docker environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsUpdate),
	}, h.UpdateEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "deleteEnvironment",
		Method:      "DELETE",
		Path:        "/environments/{id}",
		Summary:     "Delete an environment",
		Description: "Delete a Arcane environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsDelete),
	}, h.DeleteEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "testConnection",
		Method:      "POST",
		Path:        "/environments/{id}/test",
		Summary:     "Test environment connection",
		Description: "Test connectivity to a Arcane environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsRead, h.TestConnection)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateHeartbeat",
		Method:      "POST",
		Path:        "/environments/{id}/heartbeat",
		Summary:     "Update environment heartbeat",
		Description: "Update the heartbeat timestamp for an environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsSync, h.UpdateHeartbeat)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pairAgent",
		Method:      "POST",
		Path:        "/environments/{id}/agent/pair",
		Summary:     "Pair with local agent",
		Description: "Generate or rotate the local agent pairing token",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.PairAgent)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "syncEnvironment",
		Method:      "POST",
		Path:        "/environments/{id}/sync",
		Summary:     "Sync environment",
		Description: "Sync container registries, S3 destinations, and git repositories to a remote environment. Returns an error if any resource group fails; other groups may still sync successfully.",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsSync, h.SyncEnvironment)

	huma.Register(api, huma.Operation{
		OperationID:  "pairEnvironment",
		Method:       "POST",
		Path:         "/environments/pair",
		Summary:      "Pair agent with manager",
		Description:  "Agent sends API key to complete environment pairing",
		Tags:         []string{"Environments"},
		MaxBodyBytes: 1024,
		Security:     []map[string][]string{},
	}, h.PairEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getDeploymentSnippets",
		Method:      "GET",
		Path:        "/environments/{id}/deployment",
		Summary:     "Get deployment snippets",
		Description: "Get Docker run and compose snippets for environment deployment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.GetDeploymentSnippets)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "downloadEnvironmentMTLSBundle",
		Method:      "GET",
		Path:        "/environments/{id}/deployment/mtls/bundle",
		Summary:     "Download environment mTLS bundle",
		Description: "Download the generated mTLS client certificate bundle for an edge environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.DownloadEnvironmentMTLSBundle)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "downloadEnvironmentMTLSFile",
		Method:      "GET",
		Path:        "/environments/{id}/deployment/mtls/{fileName}",
		Summary:     "Download environment mTLS asset",
		Description: "Download an individual generated mTLS client certificate asset for an edge environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.DownloadEnvironmentMTLSFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getEnvironmentVersion",
		Method:      "GET",
		Path:        "/environments/{id}/version",
		Summary:     "Get environment version",
		Description: "Get the version of a remote environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsRead, h.GetEnvironmentVersion)

	huma.Register(api, huma.Operation{
		OperationID: "downloadEdgeMTLSCA",
		Method:      "GET",
		Path:        "/edge-mtls/ca",
		Summary:     "Download Arcane-generated edge mTLS CA",
		Description: "Download the Arcane-managed certificate authority used for generated edge mTLS client certificates",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsPair),
	}, h.DownloadEdgeMTLSCA)
}
