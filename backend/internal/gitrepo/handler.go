package gitrepo

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	GitRepositoryHandler struct {
		repoService *GitRepositoryService
	}

	ListGitRepositoriesInput struct {
		Search string `query:"search" doc:"Search query"`
		Sort   string `query:"sort" doc:"Column to sort by"`
		Order  string `query:"order" default:"asc" doc:"Sort direction"`
		Start  int    `query:"start" default:"0" doc:"Start index"`
		Limit  int    `query:"limit" default:"20" doc:"Items per page"`
	}

	CreateGitRepositoryInput struct {
		types.Create[types.GitRepository]
	}

	GetGitRepositoryInput struct {
		ID string `path:"id" doc:"Repository ID"`
	}

	UpdateGitRepositoryInput struct {
		ID string `path:"id" doc:"Repository ID"`
		types.Update[types.GitRepository]
	}

	DeleteGitRepositoryInput struct {
		ID string `path:"id" doc:"Repository ID"`
	}

	GitRepositoryActionInput struct {
		ID     string `path:"id" doc:"Repository ID"`
		Action string `query:"action" required:"true" enum:"test" doc:"Repository action"`
		Branch string `query:"branch" doc:"Branch to test (optional, uses repository default branch when omitted)"`
	}

	ListBranchesInput struct {
		ID string `path:"id" doc:"Repository ID"`
	}

	BrowseFilesInput struct {
		ID     string `path:"id" doc:"Repository ID"`
		Branch string `query:"branch" doc:"Branch to browse"`
		Path   string `query:"path" doc:"Path within repository (optional)"`
	}

	SyncGitRepositoriesInput struct {
		Body types.GitRepositorySyncRequest
	}
)

func (h *GitRepositoryHandler) ListRepositories(ctx context.Context, input *ListGitRepositoriesInput) (*handlerutil.Page[types.GitRepository], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	repositories, paginationResp, err := h.repoService.GetRepositoriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list git repositories: " + err.Error())
	}

	return &handlerutil.Page[types.GitRepository]{
		Body: types.Paginated[types.GitRepository]{
			Success:    true,
			Data:       repositories,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *GitRepositoryHandler) CreateRepository(ctx context.Context, input *CreateGitRepositoryInput) (*handlerutil.Out[types.GitRepository], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body", err)
	}

	repo, err := h.repoService.CreateRepository(ctx, input.Body, submitted, handlerutil.CurrentActor(ctx))
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to create git repository")
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*GitRepository, types.GitRepository](repo, func(err error) string {
		return "Failed to map git repository"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[types.GitRepository]{
		Body: body,
	}, nil
}

func (h *GitRepositoryHandler) GetRepository(ctx context.Context, input *GetGitRepositoryInput) (*handlerutil.Out[types.GitRepository], error) {
	repo, err := h.repoService.GetRepositoryByID(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve git repository")
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*GitRepository, types.GitRepository](repo, func(err error) string {
		return "Failed to map git repository"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[types.GitRepository]{
		Body: body,
	}, nil
}

func (h *GitRepositoryHandler) UpdateRepository(ctx context.Context, input *UpdateGitRepositoryInput) (*handlerutil.Out[types.GitRepository], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body", err)
	}

	repo, err := h.repoService.UpdateRepository(ctx, input.ID, RepositoryUpdate{
		Request:   input.Body,
		Submitted: submitted,
		Actor:     handlerutil.CurrentActor(ctx),
	})
	if err != nil {
		apiErr := common.ToAPIError(err)
		if apiErr.Code == common.APIErrorCodeValidationError {
			return nil, handlerutil.APIError(err)
		}
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to update git repository")
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*GitRepository, types.GitRepository](repo, func(err error) string {
		return "Failed to map git repository"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[types.GitRepository]{
		Body: body,
	}, nil
}

func (h *GitRepositoryHandler) DeleteRepository(ctx context.Context, input *DeleteGitRepositoryInput) (*handlerutil.Out[types.MessageResponse], error) {
	actor := handlerutil.CurrentActor(ctx)

	if err := h.repoService.DeleteRepository(ctx, input.ID, actor); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete git repository")
	}

	return handlerutil.MessageOutput("Repository deleted successfully", ""), nil
}

func (h *GitRepositoryHandler) TestRepository(ctx context.Context, input *GitRepositoryActionInput) (*handlerutil.Out[types.MessageResponse], error) {
	actor := handlerutil.CurrentActor(ctx)

	if err := h.repoService.TestConnection(ctx, input.ID, input.Branch, actor); err != nil {
		return nil, huma.Error400BadRequest("Failed to test git repository connection: " + err.Error())
	}

	return handlerutil.MessageOutput("Connection successful", ""), nil
}

func (h *GitRepositoryHandler) ListBranches(ctx context.Context, input *ListBranchesInput) (*handlerutil.Out[types.GitBranchesResponse], error) {
	branches, err := h.repoService.ListBranches(ctx, input.ID)
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to test git repository connection: " + err.Error())
	}

	return &handlerutil.Out[types.GitBranchesResponse]{
		Body: types.ApiResponse[types.GitBranchesResponse]{
			Success: true,
			Data: types.GitBranchesResponse{
				Branches: branches,
			},
		},
	}, nil
}

func (h *GitRepositoryHandler) BrowseFiles(ctx context.Context, input *BrowseFilesInput) (*handlerutil.Out[types.GitOpsBrowseResponse], error) {
	if input.Branch == "" {
		return nil, huma.Error400BadRequest("branch parameter is required")
	}

	result, err := h.repoService.BrowseFiles(ctx, input.ID, input.Branch, input.Path)
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to test git repository connection: " + err.Error())
	}

	return &handlerutil.Out[types.GitOpsBrowseResponse]{
		Body: types.ApiResponse[types.GitOpsBrowseResponse]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

func (h *GitRepositoryHandler) SyncRepositories(ctx context.Context, input *SyncGitRepositoriesInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.repoService.SyncRepositories(ctx, input.Body.Repositories); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to sync git repositories: "+err.Error())
	}

	return handlerutil.MessageOutput("Repositories synced successfully", ""), nil
}
