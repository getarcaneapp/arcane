package gitrepo

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
)

// GitRepositoryHandler handles git repository management endpoints.
type GitRepositoryHandler struct {
	repoService *GitRepositoryService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type ListGitRepositoriesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type CreateGitRepositoryInput struct {
	Body CreateGitRepositoryRequest
}

type GetGitRepositoryInput struct {
	ID string `path:"id" doc:"Repository ID"`
}

type UpdateGitRepositoryInput struct {
	ID   string `path:"id" doc:"Repository ID"`
	Body UpdateGitRepositoryRequest
}

type DeleteGitRepositoryInput struct {
	ID string `path:"id" doc:"Repository ID"`
}

type TestGitRepositoryInput struct {
	ID     string `path:"id" doc:"Repository ID"`
	Branch string `query:"branch" doc:"Branch to test (optional, uses repository default branch when omitted)"`
}

type ListBranchesInput struct {
	ID string `path:"id" doc:"Repository ID"`
}

type BrowseFilesInput struct {
	ID     string `path:"id" doc:"Repository ID"`
	Branch string `query:"branch" doc:"Branch to browse"`
	Path   string `query:"path" doc:"Path within repository (optional)"`
}

type SyncGitRepositoriesInput struct {
	Body gitops.RepositorySyncRequest
}

// ============================================================================
// Registration
// ============================================================================

// RegisterGitRepositories registers all git repository endpoints.
func RegisterGitRepositories(api huma.API, repoService *GitRepositoryService) {
	h := &GitRepositoryHandler{repoService: repoService}

	handlerutil.RegisterSecured(api, handlerutil.Operation("listGitRepositories", "GET", "/customize/git-repositories", "List git repositories", "Get a paginated list of git repositories", "Customize"), authz.PermGitReposList, h.ListRepositories)
	handlerutil.RegisterSecured(api, handlerutil.Operation("createGitRepository", "POST", "/customize/git-repositories", "Create a git repository", "Create a new git repository configuration", "Customize"), authz.PermGitReposCreate, h.CreateRepository)
	handlerutil.RegisterSecured(api, handlerutil.Operation("getGitRepository", "GET", "/customize/git-repositories/{id}", "Get a git repository", "Get a git repository by ID", "Customize"), authz.PermGitReposRead, h.GetRepository)
	handlerutil.RegisterSecured(api, handlerutil.Operation("updateGitRepository", "PUT", "/customize/git-repositories/{id}", "Update a git repository", "Update an existing git repository configuration", "Customize"), authz.PermGitReposUpdate, h.UpdateRepository)
	handlerutil.RegisterSecured(api, handlerutil.Operation("deleteGitRepository", "DELETE", "/customize/git-repositories/{id}", "Delete a git repository", "Delete a git repository configuration by ID", "Customize"), authz.PermGitReposDelete, h.DeleteRepository)
	handlerutil.RegisterSecured(api, handlerutil.Operation("testGitRepository", "POST", "/customize/git-repositories/{id}/test", "Test a git repository", "Test connectivity and authentication to a git repository", "Customize"), authz.PermGitReposTest, h.TestRepository)
	handlerutil.RegisterSecured(api, handlerutil.Operation("listGitRepositoryBranches", "GET", "/customize/git-repositories/{id}/branches", "List repository branches", "Get all branches from a git repository with default branch detection", "Customize"), authz.PermGitReposRead, h.ListBranches)
	handlerutil.RegisterSecured(api, handlerutil.Operation("browseGitRepositoryFiles", "GET", "/customize/git-repositories/{id}/files", "Browse repository files", "Browse files and directories in a git repository", "Customize"), authz.PermGitReposRead, h.BrowseFiles)
	handlerutil.RegisterSecured(api, handlerutil.Operation("syncGitRepositories", "POST", "/git-repositories/sync", "Sync git repositories", "Sync git repositories from a manager to this agent instance", "Git Repositories"), authz.PermGitReposSync, h.SyncRepositories)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListRepositories returns a paginated list of git repositories.
func (h *GitRepositoryHandler) ListRepositories(ctx context.Context, input *ListGitRepositoriesInput) (*handlerutil.Page[gitops.GitRepository], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	repositories, paginationResp, err := h.repoService.GetRepositoriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list git repositories").Error())
	}

	return &handlerutil.Page[gitops.GitRepository]{
		Body: base.Paginated[gitops.GitRepository]{
			Success:    true,
			Data:       repositories,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// CreateRepository creates a new git repository.
func (h *GitRepositoryHandler) CreateRepository(ctx context.Context, input *CreateGitRepositoryInput) (*handlerutil.Out[gitops.GitRepository], error) {
	actor := handlerutil.CurrentActor(ctx)

	repo, err := h.repoService.CreateRepository(ctx, input.Body, actor)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to create git repository")
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*GitRepository, gitops.GitRepository](repo, func(err error) string {
		return "Failed to map git repository"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[gitops.GitRepository]{
		Body: body,
	}, nil
}

// GetRepository returns a git repository by ID.
func (h *GitRepositoryHandler) GetRepository(ctx context.Context, input *GetGitRepositoryInput) (*handlerutil.Out[gitops.GitRepository], error) {
	repo, err := h.repoService.GetRepositoryByID(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve git repository")
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*GitRepository, gitops.GitRepository](repo, func(err error) string {
		return "Failed to map git repository"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[gitops.GitRepository]{
		Body: body,
	}, nil
}

// UpdateRepository updates an existing git repository.
func (h *GitRepositoryHandler) UpdateRepository(ctx context.Context, input *UpdateGitRepositoryInput) (*handlerutil.Out[gitops.GitRepository], error) {
	actor := handlerutil.CurrentActor(ctx)

	repo, err := h.repoService.UpdateRepository(ctx, input.ID, input.Body, actor)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to update git repository")
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*GitRepository, gitops.GitRepository](repo, func(err error) string {
		return "Failed to map git repository"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[gitops.GitRepository]{
		Body: body,
	}, nil
}

// DeleteRepository deletes a git repository by ID.
func (h *GitRepositoryHandler) DeleteRepository(ctx context.Context, input *DeleteGitRepositoryInput) (*handlerutil.Out[base.MessageResponse], error) {
	actor := handlerutil.CurrentActor(ctx)

	if err := h.repoService.DeleteRepository(ctx, input.ID, actor); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete git repository")
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Repository deleted successfully",
			},
		},
	}, nil
}

// TestRepository tests connectivity and authentication to a git repository.
func (h *GitRepositoryHandler) TestRepository(ctx context.Context, input *TestGitRepositoryInput) (*handlerutil.Out[base.MessageResponse], error) {
	actor := handlerutil.CurrentActor(ctx)

	if err := h.repoService.TestConnection(ctx, input.ID, input.Branch, actor); err != nil {
		return nil, huma.Error400BadRequest(errors.WithMessage(err, "Failed to test git repository connection").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Connection successful",
			},
		},
	}, nil
}

// ListBranches returns all branches from a git repository.
func (h *GitRepositoryHandler) ListBranches(ctx context.Context, input *ListBranchesInput) (*handlerutil.Out[gitops.BranchesResponse], error) {
	branches, err := h.repoService.ListBranches(ctx, input.ID)
	if err != nil {
		return nil, huma.Error400BadRequest(errors.WithMessage(err, "Failed to test git repository connection").Error())
	}

	return &handlerutil.Out[gitops.BranchesResponse]{
		Body: base.ApiResponse[gitops.BranchesResponse]{
			Success: true,
			Data: gitops.BranchesResponse{
				Branches: branches,
			},
		},
	}, nil
}

// BrowseFiles returns files and directories from a git repository.
func (h *GitRepositoryHandler) BrowseFiles(ctx context.Context, input *BrowseFilesInput) (*handlerutil.Out[gitops.BrowseResponse], error) {
	if input.Branch == "" {
		return nil, huma.Error400BadRequest("branch parameter is required")
	}

	result, err := h.repoService.BrowseFiles(ctx, input.ID, input.Branch, input.Path)
	if err != nil {
		return nil, huma.Error400BadRequest(errors.WithMessage(err, "Failed to test git repository connection").Error())
	}

	return &handlerutil.Out[gitops.BrowseResponse]{
		Body: base.ApiResponse[gitops.BrowseResponse]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// SyncRepositories syncs git repositories from a manager to this agent instance.
func (h *GitRepositoryHandler) SyncRepositories(ctx context.Context, input *SyncGitRepositoriesInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.repoService.SyncRepositories(ctx, input.Body.Repositories); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to sync git repositories").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Repositories synced successfully",
			},
		},
	}, nil
}
