package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
)

// GitRepositoryHandler handles git repository management endpoints.
type GitRepositoryHandler struct {
	repoService *services.GitRepositoryService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// GitRepositoryPaginatedResponse is the paginated response for git repositories.
type GitRepositoryPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []gitops.GitRepository  `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListGitRepositoriesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListGitRepositoriesOutput struct {
	Body GitRepositoryPaginatedResponse
}

type CreateGitRepositoryInput struct {
	Body models.CreateGitRepositoryRequest
}

type CreateGitRepositoryOutput struct {
	Body base.ApiResponse[gitops.GitRepository]
}

type GetGitRepositoryInput struct {
	ID string `path:"id" doc:"Repository ID"`
}

type GetGitRepositoryOutput struct {
	Body base.ApiResponse[gitops.GitRepository]
}

type UpdateGitRepositoryInput struct {
	ID   string `path:"id" doc:"Repository ID"`
	Body models.UpdateGitRepositoryRequest
}

type UpdateGitRepositoryOutput struct {
	Body base.ApiResponse[gitops.GitRepository]
}

type DeleteGitRepositoryInput struct {
	ID string `path:"id" doc:"Repository ID"`
}

type DeleteGitRepositoryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type TestGitRepositoryInput struct {
	ID     string `path:"id" doc:"Repository ID"`
	Branch string `query:"branch" doc:"Branch to test (optional, uses repository default branch when omitted)"`
}

type TestGitRepositoryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type ListBranchesInput struct {
	ID string `path:"id" doc:"Repository ID"`
}

type ListBranchesOutput struct {
	Body base.ApiResponse[gitops.BranchesResponse]
}

type BrowseFilesInput struct {
	ID     string `path:"id" doc:"Repository ID"`
	Branch string `query:"branch" doc:"Branch to browse"`
	Path   string `query:"path" doc:"Path within repository (optional)"`
}

type BrowseFilesOutput struct {
	Body base.ApiResponse[gitops.BrowseResponse]
}

type SyncGitRepositoriesInput struct {
	Body gitops.RepositorySyncRequest
}

type SyncGitRepositoriesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterGitRepositories registers all git repository endpoints.
func RegisterGitRepositories(api huma.API, repoService *services.GitRepositoryService) {
	h := &GitRepositoryHandler{repoService: repoService}

	registerCustomizeSecuredInternal(api, "listGitRepositories", "GET", "/customize/git-repositories", "List git repositories", "Get a paginated list of git repositories", authz.PermGitReposList, h.ListRepositories)
	registerCustomizeSecuredInternal(api, "createGitRepository", "POST", "/customize/git-repositories", "Create a git repository", "Create a new git repository configuration", authz.PermGitReposCreate, h.CreateRepository)
	registerCustomizeSecuredInternal(api, "getGitRepository", "GET", "/customize/git-repositories/{id}", "Get a git repository", "Get a git repository by ID", authz.PermGitReposRead, h.GetRepository)
	registerCustomizeSecuredInternal(api, "updateGitRepository", "PUT", "/customize/git-repositories/{id}", "Update a git repository", "Update an existing git repository configuration", authz.PermGitReposUpdate, h.UpdateRepository)
	registerCustomizeSecuredInternal(api, "deleteGitRepository", "DELETE", "/customize/git-repositories/{id}", "Delete a git repository", "Delete a git repository configuration by ID", authz.PermGitReposDelete, h.DeleteRepository)
	registerCustomizeSecuredInternal(api, "testGitRepository", "POST", "/customize/git-repositories/{id}/test", "Test a git repository", "Test connectivity and authentication to a git repository", authz.PermGitReposTest, h.TestRepository)
	registerCustomizeSecuredInternal(api, "listGitRepositoryBranches", "GET", "/customize/git-repositories/{id}/branches", "List repository branches", "Get all branches from a git repository with default branch detection", authz.PermGitReposRead, h.ListBranches)
	registerCustomizeSecuredInternal(api, "browseGitRepositoryFiles", "GET", "/customize/git-repositories/{id}/files", "Browse repository files", "Browse files and directories in a git repository", authz.PermGitReposRead, h.BrowseFiles)
	registerTaggedSecuredInternal(api, "syncGitRepositories", "POST", "/git-repositories/sync", "Sync git repositories", "Sync git repositories from a manager to this agent instance", "Git Repositories", authz.PermGitReposSync, h.SyncRepositories)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListRepositories returns a paginated list of git repositories.
func (h *GitRepositoryHandler) ListRepositories(ctx context.Context, input *ListGitRepositoriesInput) (*ListGitRepositoriesOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	repositories, paginationResp, err := h.repoService.GetRepositoriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.GitRepositoryListError{Err: err}).Error())
	}

	return &ListGitRepositoriesOutput{
		Body: GitRepositoryPaginatedResponse{
			Success:    true,
			Data:       repositories,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// CreateRepository creates a new git repository.
func (h *GitRepositoryHandler) CreateRepository(ctx context.Context, input *CreateGitRepositoryInput) (*CreateGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	repo, err := h.repoService.CreateRepository(ctx, input.Body, actor)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitRepositoryCreationError{Err: err}).Error())
	}

	body, mapErr := mapOneAPIResponseInternal[*models.GitRepository, gitops.GitRepository](repo, func(err error) string {
		return (&common.GitRepositoryMappingError{Err: err}).Error()
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &CreateGitRepositoryOutput{
		Body: body,
	}, nil
}

// GetRepository returns a git repository by ID.
func (h *GitRepositoryHandler) GetRepository(ctx context.Context, input *GetGitRepositoryInput) (*GetGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	repo, err := h.repoService.GetRepositoryByID(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitRepositoryRetrievalError{Err: err}).Error())
	}

	body, mapErr := mapOneAPIResponseInternal[*models.GitRepository, gitops.GitRepository](repo, func(err error) string {
		return (&common.GitRepositoryMappingError{Err: err}).Error()
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &GetGitRepositoryOutput{
		Body: body,
	}, nil
}

// UpdateRepository updates an existing git repository.
func (h *GitRepositoryHandler) UpdateRepository(ctx context.Context, input *UpdateGitRepositoryInput) (*UpdateGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	repo, err := h.repoService.UpdateRepository(ctx, input.ID, input.Body, actor)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitRepositoryUpdateError{Err: err}).Error())
	}

	body, mapErr := mapOneAPIResponseInternal[*models.GitRepository, gitops.GitRepository](repo, func(err error) string {
		return (&common.GitRepositoryMappingError{Err: err}).Error()
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &UpdateGitRepositoryOutput{
		Body: body,
	}, nil
}

// DeleteRepository deletes a git repository by ID.
func (h *GitRepositoryHandler) DeleteRepository(ctx context.Context, input *DeleteGitRepositoryInput) (*DeleteGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	if err := h.repoService.DeleteRepository(ctx, input.ID, actor); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitRepositoryDeletionError{Err: err}).Error())
	}

	return &DeleteGitRepositoryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Repository deleted successfully",
			},
		},
	}, nil
}

// TestRepository tests connectivity and authentication to a git repository.
func (h *GitRepositoryHandler) TestRepository(ctx context.Context, input *TestGitRepositoryInput) (*TestGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	if err := h.repoService.TestConnection(ctx, input.ID, input.Branch, actor); err != nil {
		return nil, huma.Error400BadRequest((&common.GitRepositoryTestError{Err: err}).Error())
	}

	return &TestGitRepositoryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Connection successful",
			},
		},
	}, nil
}

// ListBranches returns all branches from a git repository.
func (h *GitRepositoryHandler) ListBranches(ctx context.Context, input *ListBranchesInput) (*ListBranchesOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	branches, err := h.repoService.ListBranches(ctx, input.ID)
	if err != nil {
		return nil, huma.Error400BadRequest((&common.GitRepositoryTestError{Err: err}).Error())
	}

	return &ListBranchesOutput{
		Body: base.ApiResponse[gitops.BranchesResponse]{
			Success: true,
			Data: gitops.BranchesResponse{
				Branches: branches,
			},
		},
	}, nil
}

// BrowseFiles returns files and directories from a git repository.
func (h *GitRepositoryHandler) BrowseFiles(ctx context.Context, input *BrowseFilesInput) (*BrowseFilesOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.Branch == "" {
		return nil, huma.Error400BadRequest("branch parameter is required")
	}

	result, err := h.repoService.BrowseFiles(ctx, input.ID, input.Branch, input.Path)
	if err != nil {
		return nil, huma.Error400BadRequest((&common.GitRepositoryTestError{Err: err}).Error())
	}

	return &BrowseFilesOutput{
		Body: base.ApiResponse[gitops.BrowseResponse]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// SyncRepositories syncs git repositories from a manager to this agent instance.
func (h *GitRepositoryHandler) SyncRepositories(ctx context.Context, input *SyncGitRepositoriesInput) (*SyncGitRepositoriesOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.repoService.SyncRepositories(ctx, input.Body.Repositories); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitRepositorySyncError{Err: err}).Error())
	}

	return &SyncGitRepositoriesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Repositories synced successfully",
			},
		},
	}, nil
}
