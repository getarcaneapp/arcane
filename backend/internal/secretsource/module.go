// Package secretsource owns connections to external secret providers
// (Infisical), project secret bindings, the deploy-time secret fetch, and
// background drift checks.
package secretsource

import (
	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type Module struct {
	service *SecretSourceService
	handler *SecretSourceHandler
}

func New(service *SecretSourceService, resolveProject ProjectResolver) *Module {
	return &Module{
		service: service,
		handler: &SecretSourceHandler{service: service, resolveProject: resolveProject},
	}
}

func (m *Module) Service() *SecretSourceService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes registers manager routes. Agents do not serve them: a remote
// environment's bindings are not supported yet.
func (m *Module) RegisterRoutes(api huma.API, cfg *config.Config) {
	if cfg != nil && cfg.AgentEnabled {
		return
	}
	if m == nil {
		RegisterSecretSources(api, &SecretSourceHandler{})
		return
	}
	RegisterSecretSources(api, m.handler)
}

func RegisterSecretSources(api huma.API, h *SecretSourceHandler) {
	tags := []string{"Secret Sources"}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listSecretSources",
		Method:      "GET",
		Path:        "/secret-sources",
		Summary:     "List secret sources",
		Description: "List connections to external secret managers (client secrets are never returned)",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesList, h.ListSources)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "createSecretSource",
		Method:      "POST",
		Path:        "/secret-sources",
		Summary:     "Create a secret source",
		Description: "Save a connection to a secret provider: Infisical (Universal Auth)",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesCreate, h.CreateSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "testSecretSource",
		Method:      "POST",
		Path:        "/secret-sources/test",
		Summary:     "Test secret source settings",
		Description: "Log in with the given settings without saving them; pass sourceId to reuse a stored client secret",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesTest, h.TestSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getSecretSource",
		Method:      "GET",
		Path:        "/secret-sources/{id}",
		Summary:     "Get a secret source",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesRead, h.GetSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateSecretSource",
		Method:      "PUT",
		Path:        "/secret-sources/{id}",
		Summary:     "Update a secret source",
		Description: "Update a secret source; an empty client secret keeps the stored one",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesUpdate, h.UpdateSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteSecretSource",
		Method:      "DELETE",
		Path:        "/secret-sources/{id}",
		Summary:     "Delete a secret source",
		Description: "Delete a secret source that no project is bound to",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesDelete, h.DeleteSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "browseSecretSource",
		Method:      "GET",
		Path:        "/secret-sources/{id}/browse",
		Summary:     "Browse a secret source",
		Description: "List pickable binding targets, such as Infisical projects and folders",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesRead, h.Browse)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listProjectSecretBindings",
		Method:      "GET",
		Path:        "/environments/{id}/projects/{projectId}/secrets",
		Summary:     "List a project's secret bindings",
		Description: "Bindings apply in order; when two deliver the same variable, the earlier one wins.",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.ListProjectBindings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "createProjectSecretBinding",
		Method:      "POST",
		Path:        "/environments/{id}/projects/{projectId}/secrets",
		Summary:     "Bind a project to one more secret source target",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
		// A binding delivers anything the source's identity can read, so it
		// also needs the right to use secret sources.
		Middlewares: middleware.RequirePermission(api, authz.PermSecretSourcesUse),
	}, authz.PermProjectsUpdate, h.CreateProjectBinding)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateProjectSecretBinding",
		Method:      "PUT",
		Path:        "/environments/{id}/projects/{projectId}/secrets/{bindingId}",
		Summary:     "Change one of a project's secret bindings",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermSecretSourcesUse),
	}, authz.PermProjectsUpdate, h.UpdateProjectBinding)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteProjectSecretBinding",
		Method:      "DELETE",
		Path:        "/environments/{id}/projects/{projectId}/secrets/{bindingId}",
		Summary:     "Remove one of a project's secret bindings",
		Tags:        tags,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsUpdate, h.DeleteProjectBinding)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "checkProjectSecretBindings",
		Method:      "POST",
		Path:        "/environments/{id}/projects/{projectId}/secrets/check",
		Summary:     "Check a project's secrets",
		Description: "Fetch every binding now and report, per binding, key names, keys an earlier binding already delivers, " +
			"overridden .env keys, and whether a redeploy is needed. Values are never returned.",
		Tags:     tags,
		Security: handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsDeploy, h.CheckProjectBindings)
}
