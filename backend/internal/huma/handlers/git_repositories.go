package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/gitops"
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
	Page    int    `query:"pagination[page]" default:"1" doc:"Page number"`
	Limit   int    `query:"pagination[limit]" default:"20" doc:"Items per page"`
	SortCol string `query:"sort[column]" doc:"Column to sort by"`
	SortDir string `query:"sort[direction]" default:"asc" doc:"Sort direction"`
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
	Branch string `query:"branch" doc:"Branch to test (optional, defaults to main)"`
}

type TestGitRepositoryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterGitRepositories registers all git repository endpoints.
func RegisterGitRepositories(api huma.API, repoService *services.GitRepositoryService) {
	h := &GitRepositoryHandler{repoService: repoService}

	huma.Register(api, huma.Operation{
		OperationID: "listGitRepositories",
		Method:      "GET",
		Path:        "/git-repositories",
		Summary:     "List git repositories",
		Description: "Get a paginated list of git repositories",
		Tags:        []string{"Git Repositories"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListRepositories)

	huma.Register(api, huma.Operation{
		OperationID: "createGitRepository",
		Method:      "POST",
		Path:        "/git-repositories",
		Summary:     "Create a git repository",
		Description: "Create a new git repository configuration",
		Tags:        []string{"Git Repositories"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateRepository)

	huma.Register(api, huma.Operation{
		OperationID: "getGitRepository",
		Method:      "GET",
		Path:        "/git-repositories/{id}",
		Summary:     "Get a git repository",
		Description: "Get a git repository by ID",
		Tags:        []string{"Git Repositories"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetRepository)

	huma.Register(api, huma.Operation{
		OperationID: "updateGitRepository",
		Method:      "PUT",
		Path:        "/git-repositories/{id}",
		Summary:     "Update a git repository",
		Description: "Update an existing git repository configuration",
		Tags:        []string{"Git Repositories"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateRepository)

	huma.Register(api, huma.Operation{
		OperationID: "deleteGitRepository",
		Method:      "DELETE",
		Path:        "/git-repositories/{id}",
		Summary:     "Delete a git repository",
		Description: "Delete a git repository configuration by ID",
		Tags:        []string{"Git Repositories"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteRepository)

	huma.Register(api, huma.Operation{
		OperationID: "testGitRepository",
		Method:      "POST",
		Path:        "/git-repositories/{id}/test",
		Summary:     "Test a git repository",
		Description: "Test connectivity and authentication to a git repository",
		Tags:        []string{"Git Repositories"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.TestRepository)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListRepositories returns a paginated list of git repositories.
func (h *GitRepositoryHandler) ListRepositories(ctx context.Context, input *ListGitRepositoriesInput) (*ListGitRepositoriesOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParams(input.Page, input.Limit, input.SortCol, input.SortDir)

	repositories, paginationResp, err := h.repoService.GetRepositoriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.GitRepositoryListError{Err: err}).Error())
	}

	return &ListGitRepositoriesOutput{
		Body: GitRepositoryPaginatedResponse{
			Success: true,
			Data:    repositories,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

// CreateRepository creates a new git repository.
func (h *GitRepositoryHandler) CreateRepository(ctx context.Context, input *CreateGitRepositoryInput) (*CreateGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	repo, err := h.repoService.CreateRepository(ctx, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitRepositoryCreationError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.GitRepository, gitops.GitRepository](repo)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.GitRepositoryMappingError{Err: mapErr}).Error())
	}

	return &CreateGitRepositoryOutput{
		Body: base.ApiResponse[gitops.GitRepository]{
			Success: true,
			Data:    out,
		},
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

	out, mapErr := mapper.MapOne[*models.GitRepository, gitops.GitRepository](repo)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.GitRepositoryMappingError{Err: mapErr}).Error())
	}

	return &GetGitRepositoryOutput{
		Body: base.ApiResponse[gitops.GitRepository]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateRepository updates an existing git repository.
func (h *GitRepositoryHandler) UpdateRepository(ctx context.Context, input *UpdateGitRepositoryInput) (*UpdateGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	repo, err := h.repoService.UpdateRepository(ctx, input.ID, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitRepositoryUpdateError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.GitRepository, gitops.GitRepository](repo)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.GitRepositoryMappingError{Err: mapErr}).Error())
	}

	return &UpdateGitRepositoryOutput{
		Body: base.ApiResponse[gitops.GitRepository]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteRepository deletes a git repository by ID.
func (h *GitRepositoryHandler) DeleteRepository(ctx context.Context, input *DeleteGitRepositoryInput) (*DeleteGitRepositoryOutput, error) {
	if h.repoService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.repoService.DeleteRepository(ctx, input.ID); err != nil {
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

	if err := h.repoService.TestConnection(ctx, input.ID, input.Branch); err != nil {
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
