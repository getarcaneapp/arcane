package handlers

import (
	"context"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/snippet"
)

// SnippetHandler handles Snippet management endpoints.
type SnippetHandler struct {
	snippetService *services.SnippetService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type ListSnippetsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListSnippetsOutput struct {
	Body base.Paginated[snippet.Snippet]
}

type CreateSnippetInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          snippet.CreateSnippetRequest
}

type CreateSnippetOutput struct {
	Body base.ApiResponse[snippet.Snippet]
}

type GetSnippetInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SnippetID     string `path:"snippetId" doc:"Snippet ID"`
}

type GetSnippetOutput struct {
	Body base.ApiResponse[snippet.Snippet]
}

type UpdateSnippetInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SnippetID     string `path:"snippetId" doc:"Snippet ID"`
	Body          snippet.UpdateSnippetRequest
}

type UpdateSnippetOutput struct {
	Body base.ApiResponse[snippet.Snippet]
}

type DeleteSnippetInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SnippetID     string `path:"snippetId" doc:"Snippet ID"`
}

type DeleteSnippetOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type RunSnippetInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SnippetID     string `path:"snippetId" doc:"Snippet ID"`
	Body          snippet.RunSnippetRequest
}

type RunSnippetOutput struct {
	Body base.ApiResponse[snippet.SnippetRun]
}

type ListSnippetRunsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SnippetID     string `path:"snippetId" doc:"Snippet ID"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"desc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListSnippetRunsOutput struct {
	Body base.Paginated[snippet.SnippetRun]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterSnippets registers all Snippet endpoints. Registered unconditionally
// (manager and agent alike): a snippet is defined on, stored by, and executed
// by the environment that owns it, and standard env-proxy middleware routes a
// manager-side request for a remote environment's snippets to the owning
// agent — see SnippetService's package doc for the security model.
func RegisterSnippets(api huma.API, snippetService *services.SnippetService) {
	h := &SnippetHandler{snippetService: snippetService}

	registerSnippetsSecuredInternal(api, "listSnippets", "GET", "/environments/{id}/snippets", "List snippets", "Get a paginated list of snippets for an environment", authz.PermSnippetsList, h.ListSnippets)
	registerSnippetsSecuredInternal(api, "createSnippet", "POST", "/environments/{id}/snippets", "Create a snippet", "Create a new snippet for an environment", authz.PermSnippetsCreate, h.CreateSnippet)
	registerSnippetsSecuredInternal(api, "getSnippet", "GET", "/environments/{id}/snippets/{snippetId}", "Get a snippet", "Get a snippet by ID", authz.PermSnippetsRead, h.GetSnippet)
	registerSnippetsSecuredInternal(api, "updateSnippet", "PUT", "/environments/{id}/snippets/{snippetId}", "Update a snippet", "Update an existing snippet", authz.PermSnippetsUpdate, h.UpdateSnippet)
	registerSnippetsSecuredInternal(api, "deleteSnippet", "DELETE", "/environments/{id}/snippets/{snippetId}", "Delete a snippet", "Delete a snippet by ID", authz.PermSnippetsDelete, h.DeleteSnippet)
	registerSnippetsSecuredInternal(api, "runSnippet", "POST", "/environments/{id}/snippets/{snippetId}/run", "Run a snippet", "Manually run a snippet on the environment's host shell", authz.PermSnippetsRun, h.RunSnippet)
	registerSnippetsSecuredInternal(api, "listSnippetRuns", "GET", "/environments/{id}/snippets/{snippetId}/runs", "List snippet runs", "Get a paginated list of past runs for a snippet", authz.PermSnippetsRead, h.ListSnippetRuns)
}

func registerSnippetsSecuredInternal[I, O any](
	api huma.API,
	operationID string,
	method string,
	path string,
	summary string,
	description string,
	permission string,
	handler func(context.Context, *I) (*O, error),
) {
	registerSecuredInternal(api, operationInternal(operationID, method, path, summary, description, "Snippets"), permission, handler)
}

// ============================================================================
// Handler Methods
// ============================================================================

func (h *SnippetHandler) ListSnippets(ctx context.Context, input *ListSnippetsInput) (*ListSnippetsOutput, error) {
	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	snippets, paginationResp, err := h.snippetService.GetSnippetsPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list snippets").Error())
	}

	return &ListSnippetsOutput{
		Body: base.Paginated[snippet.Snippet]{
			Success:    true,
			Data:       snippets,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

func (h *SnippetHandler) CreateSnippet(ctx context.Context, input *CreateSnippetInput) (*CreateSnippetOutput, error) {
	actor := currentActorInternal(ctx)

	snip, err := h.snippetService.CreateSnippet(ctx, input.EnvironmentID, input.Body, actor)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to create snippet").Error())
	}

	body, mapErr := mapOneAPIResponseInternal[*models.Snippet, snippet.Snippet](snip, func(err error) string {
		return "Failed to map snippet"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &CreateSnippetOutput{Body: body}, nil
}

func (h *SnippetHandler) GetSnippet(ctx context.Context, input *GetSnippetInput) (*GetSnippetOutput, error) {
	snip, err := h.snippetService.GetSnippetByID(ctx, input.EnvironmentID, input.SnippetID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve snippet")
	}

	body, mapErr := mapOneAPIResponseInternal[*models.Snippet, snippet.Snippet](snip, func(err error) string {
		return "Failed to map snippet"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &GetSnippetOutput{Body: body}, nil
}

func (h *SnippetHandler) UpdateSnippet(ctx context.Context, input *UpdateSnippetInput) (*UpdateSnippetOutput, error) {
	actor := currentActorInternal(ctx)

	snip, err := h.snippetService.UpdateSnippet(ctx, input.EnvironmentID, input.SnippetID, input.Body, actor)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to update snippet").Error())
	}

	body, mapErr := mapOneAPIResponseInternal[*models.Snippet, snippet.Snippet](snip, func(err error) string {
		return "Failed to map snippet"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &UpdateSnippetOutput{Body: body}, nil
}

func (h *SnippetHandler) DeleteSnippet(ctx context.Context, input *DeleteSnippetInput) (*DeleteSnippetOutput, error) {
	actor := currentActorInternal(ctx)

	if err := h.snippetService.DeleteSnippet(ctx, input.EnvironmentID, input.SnippetID, actor); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete snippet")
	}

	return &DeleteSnippetOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Snippet deleted successfully"},
		},
	}, nil
}

func (h *SnippetHandler) RunSnippet(ctx context.Context, input *RunSnippetInput) (*RunSnippetOutput, error) {
	actor := currentActorInternal(ctx)

	run, err := h.snippetService.RunSnippet(ctx, input.EnvironmentID, input.SnippetID, input.Body.Parameters, "manual", actor)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to run snippet").Error())
	}

	body, mapErr := mapOneAPIResponseInternal[*models.SnippetRun, snippet.SnippetRun](run, func(err error) string {
		return "Failed to map snippet run"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &RunSnippetOutput{Body: body}, nil
}

func (h *SnippetHandler) ListSnippetRuns(ctx context.Context, input *ListSnippetRunsInput) (*ListSnippetRunsOutput, error) {
	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, "")

	runs, paginationResp, err := h.snippetService.GetSnippetRunsPaginated(ctx, input.EnvironmentID, input.SnippetID, params)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to list snippet runs").Error())
	}

	return &ListSnippetRunsOutput{
		Body: base.Paginated[snippet.SnippetRun]{
			Success:    true,
			Data:       runs,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}
