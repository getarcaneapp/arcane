// Package gitrepo owns configured Git repositories and their HTTP surface.
package gitrepo

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service *GitRepositoryService
}

func New(service *GitRepositoryService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *GitRepositoryService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterGitRepositories(api, nil)
		return
	}
	RegisterGitRepositories(api, m.service)
}

// RegisterGitRepositories registers all git repository endpoints.
func RegisterGitRepositories(api huma.API, repoService *GitRepositoryService) {
	h := &GitRepositoryHandler{repoService: repoService}

	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"listGitRepositories",
			"GET",
			"/customize/git/repositories",
			"List git repositories",
			"Get a paginated list of git repositories",
			"Customize",
		),
		authz.PermGitReposList,
		h.ListRepositories,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"createGitRepository",
			"POST",
			"/customize/git/repositories",
			"Create a git repository",
			"Create a new git repository configuration",
			"Customize",
		),
		authz.PermGitReposCreate,
		h.CreateRepository,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"getGitRepository",
			"GET",
			"/customize/git/repositories/{id}",
			"Get a git repository",
			"Get a git repository by ID",
			"Customize",
		),
		authz.PermGitReposRead,
		h.GetRepository,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"updateGitRepository",
			"PATCH",
			"/customize/git/repositories/{id}",
			"Update a git repository",
			"Update an existing git repository configuration",
			"Customize",
		),
		authz.PermGitReposUpdate,
		h.UpdateRepository,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"deleteGitRepository",
			"DELETE",
			"/customize/git/repositories/{id}",
			"Delete a git repository",
			"Delete a git repository configuration by ID",
			"Customize",
		),
		authz.PermGitReposDelete,
		h.DeleteRepository,
	)
	middleware.RegisterWithActionPermissions(
		api,
		huma.Operation{
			OperationID: "gitRepositoryAction",
			Method:      http.MethodPost,
			Path:        "/customize/git/repositories/{id}",
			Summary:     "Test a git repository",
			Description: "Test connectivity and authentication to a git repository",
			Tags:        []string{"Customize"},
			Security:    handlerutil.DefaultOperationSecurity(),
		},
		"action",
		map[string]string{
			"test": authz.PermGitReposTest,
		},
		h.TestRepository,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"listGitRepositoryBranches",
			"GET",
			"/customize/git/repositories/{id}/branches",
			"List repository branches",
			"Get all branches from a git repository with default branch detection",
			"Customize",
		),
		authz.PermGitReposRead,
		h.ListBranches,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"browseGitRepositoryFiles",
			"GET",
			"/customize/git/repositories/{id}/files",
			"Browse repository files",
			"Browse files and directories in a git repository",
			"Customize",
		),
		authz.PermGitReposRead,
		h.BrowseFiles,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"syncGitRepositories",
			"POST",
			"/git/repositories/sync",
			"Sync git repositories",
			"Sync git repositories from a manager to this agent instance",
			"Git Repositories",
		),
		authz.PermGitReposSync,
		h.SyncRepositories,
	)
}
