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

// gitRepositoryHandler handles git repository management endpoints.
type gitRepositoryHandler struct {
	repoService *services.GitRepositoryService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// gitRepositoryPaginatedResponse is the paginated response for git repositories.
type gitRepositoryPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []gitops.GitRepository  `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type listGitRepositoriesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type listGitRepositoriesOutput struct {
	Body gitRepositoryPaginatedResponse
}

type createGitRepositoryInput struct {
	Body models.CreateGitRepositoryRequest
}

type createGitRepositoryOutput struct {
	Body base.ApiResponse[gitops.GitRepository]
}

type getGitRepositoryInput struct {
	ID string `path:"id" doc:"Repository ID"`
}

type getGitRepositoryOutput struct {
	Body base.ApiResponse[gitops.GitRepository]
}

type updateGitRepositoryInput struct {
	ID   string `path:"id" doc:"Repository ID"`
	Body models.UpdateGitRepositoryRequest
}

type updateGitRepositoryOutput struct {
	Body base.ApiResponse[gitops.GitRepository]
}

type deleteGitRepositoryInput struct {
	ID string `path:"id" doc:"Repository ID"`
}

type deleteGitRepositoryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type testGitRepositoryInput struct {
	ID     string `path:"id" doc:"Repository ID"`
	Branch string `query:"branch" doc:"Branch to test (optional, uses repository default branch when omitted)"`
}

type testGitRepositoryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type listBranchesInput struct {
	ID string `path:"id" doc:"Repository ID"`
}

type listBranchesOutput struct {
	Body base.ApiResponse[gitops.BranchesResponse]
}

type browseFilesInput struct {
	ID     string `path:"id" doc:"Repository ID"`
	Branch string `query:"branch" doc:"Branch to browse"`
	Path   string `query:"path" doc:"Path within repository (optional)"`
}

type browseFilesOutput struct {
	Body base.ApiResponse[gitops.BrowseResponse]
}

type syncGitRepositoriesInput struct {
	Body gitops.RepositorySyncRequest
}

type syncGitRepositoriesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterGitRepositories registers all git repository endpoints.
func RegisterGitRepositories(api huma.API, repoService *services.GitRepositoryService) {
	h := &gitRepositoryHandler{repoService: repoService}

	registerCustomizeSecuredInternal(api, "listGitRepositories", "GET", "/customize/git-repositories", "List git repositories", "Get a paginated list of git repositories", authz.PermGitReposList, h.listRepositoriesInternal)
	registerCustomizeSecuredInternal(api, "createGitRepository", "POST", "/customize/git-repositories", "Create a git repository", "Create a new git repository configuration", authz.PermGitReposCreate, h.createRepositoryInternal)
	registerCustomizeSecuredInternal(api, "getGitRepository", "GET", "/customize/git-repositories/{id}", "Get a git repository", "Get a git repository by ID", authz.PermGitReposRead, h.getRepositoryInternal)
	registerCustomizeSecuredInternal(api, "updateGitRepository", "PUT", "/customize/git-repositories/{id}", "Update a git repository", "Update an existing git repository configuration", authz.PermGitReposUpdate, h.updateRepositoryInternal)
	registerCustomizeSecuredInternal(api, "deleteGitRepository", "DELETE", "/customize/git-repositories/{id}", "Delete a git repository", "Delete a git repository configuration by ID", authz.PermGitReposDelete, h.deleteRepositoryInternal)
	registerCustomizeSecuredInternal(api, "testGitRepository", "POST", "/customize/git-repositories/{id}/test", "Test a git repository", "Test connectivity and authentication to a git repository", authz.PermGitReposTest, h.testRepositoryInternal)
	registerCustomizeSecuredInternal(api, "listGitRepositoryBranches", "GET", "/customize/git-repositories/{id}/branches", "List repository branches", "Get all branches from a git repository with default branch detection", authz.PermGitReposRead, h.listBranchesInternal)
	registerCustomizeSecuredInternal(api, "browseGitRepositoryFiles", "GET", "/customize/git-repositories/{id}/files", "Browse repository files", "Browse files and directories in a git repository", authz.PermGitReposRead, h.browseFilesInternal)
	registerTaggedSecuredInternal(api, "syncGitRepositories", "POST", "/git-repositories/sync", "Sync git repositories", "Sync git repositories from a manager to this agent instance", "Git Repositories", authz.PermGitReposSync, h.syncRepositoriesInternal)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListRepositories returns a paginated list of git repositories.
func (h *gitRepositoryHandler) listRepositoriesInternal(ctx context.Context, input *listGitRepositoriesInput) (*listGitRepositoriesOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	repositories, paginationResp, err := h.repoService.GetRepositoriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.GitRepositoryListError{Err: err}).Error())
	}

	return &listGitRepositoriesOutput{
		Body: gitRepositoryPaginatedResponse{
			Success:    true,
			Data:       repositories,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// CreateRepository creates a new git repository.
func (h *gitRepositoryHandler) createRepositoryInternal(ctx context.Context, input *createGitRepositoryInput) (*createGitRepositoryOutput, error) {
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

	return &createGitRepositoryOutput{
		Body: body,
	}, nil
}

// GetRepository returns a git repository by ID.
func (h *gitRepositoryHandler) getRepositoryInternal(ctx context.Context, input *getGitRepositoryInput) (*getGitRepositoryOutput, error) {
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

	return &getGitRepositoryOutput{
		Body: body,
	}, nil
}

// UpdateRepository updates an existing git repository.
func (h *gitRepositoryHandler) updateRepositoryInternal(ctx context.Context, input *updateGitRepositoryInput) (*updateGitRepositoryOutput, error) {
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

	return &updateGitRepositoryOutput{
		Body: body,
	}, nil
}

// DeleteRepository deletes a git repository by ID.
func (h *gitRepositoryHandler) deleteRepositoryInternal(ctx context.Context, input *deleteGitRepositoryInput) (*deleteGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	if err := h.repoService.DeleteRepository(ctx, input.ID, actor); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitRepositoryDeletionError{Err: err}).Error())
	}

	return &deleteGitRepositoryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Repository deleted successfully",
			},
		},
	}, nil
}

// TestRepository tests connectivity and authentication to a git repository.
func (h *gitRepositoryHandler) testRepositoryInternal(ctx context.Context, input *testGitRepositoryInput) (*testGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	if err := h.repoService.TestConnection(ctx, input.ID, input.Branch, actor); err != nil {
		return nil, huma.Error400BadRequest((&common.GitRepositoryTestError{Err: err}).Error())
	}

	return &testGitRepositoryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Connection successful",
			},
		},
	}, nil
}

// ListBranches returns all branches from a git repository.
func (h *gitRepositoryHandler) listBranchesInternal(ctx context.Context, input *listBranchesInput) (*listBranchesOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	branches, err := h.repoService.ListBranches(ctx, input.ID)
	if err != nil {
		return nil, huma.Error400BadRequest((&common.GitRepositoryTestError{Err: err}).Error())
	}

	return &listBranchesOutput{
		Body: base.ApiResponse[gitops.BranchesResponse]{
			Success: true,
			Data: gitops.BranchesResponse{
				Branches: branches,
			},
		},
	}, nil
}

// BrowseFiles returns files and directories from a git repository.
func (h *gitRepositoryHandler) browseFilesInternal(ctx context.Context, input *browseFilesInput) (*browseFilesOutput, error) {
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

	return &browseFilesOutput{
		Body: base.ApiResponse[gitops.BrowseResponse]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// SyncRepositories syncs git repositories from a manager to this agent instance.
func (h *gitRepositoryHandler) syncRepositoriesInternal(ctx context.Context, input *syncGitRepositoriesInput) (*syncGitRepositoriesOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.repoService.SyncRepositories(ctx, input.Body.Repositories); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitRepositorySyncError{Err: err}).Error())
	}

	return &syncGitRepositoriesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Repositories synced successfully",
			},
		},
	}, nil
}
