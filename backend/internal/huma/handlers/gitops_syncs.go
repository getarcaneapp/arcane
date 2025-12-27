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

// GitOpsSyncHandler handles GitOps sync management endpoints.
type GitOpsSyncHandler struct {
	syncService *services.GitOpsSyncService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// GitOpsSyncPaginatedResponse is the paginated response for GitOps syncs.
type GitOpsSyncPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []gitops.GitOpsSync     `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListGitOpsSyncsInput struct {
	Page    int    `query:"pagination[page]" default:"1" doc:"Page number"`
	Limit   int    `query:"pagination[limit]" default:"20" doc:"Items per page"`
	SortCol string `query:"sort[column]" doc:"Column to sort by"`
	SortDir string `query:"sort[direction]" default:"asc" doc:"Sort direction"`
}

type ListGitOpsSyncsOutput struct {
	Body GitOpsSyncPaginatedResponse
}

type CreateGitOpsSyncInput struct {
	Body models.CreateGitOpsSyncRequest
}

type CreateGitOpsSyncOutput struct {
	Body base.ApiResponse[gitops.GitOpsSync]
}

type GetGitOpsSyncInput struct {
	ID string `path:"id" doc:"Sync ID"`
}

type GetGitOpsSyncOutput struct {
	Body base.ApiResponse[gitops.GitOpsSync]
}

type UpdateGitOpsSyncInput struct {
	ID   string `path:"id" doc:"Sync ID"`
	Body models.UpdateGitOpsSyncRequest
}

type UpdateGitOpsSyncOutput struct {
	Body base.ApiResponse[gitops.GitOpsSync]
}

type DeleteGitOpsSyncInput struct {
	ID string `path:"id" doc:"Sync ID"`
}

type DeleteGitOpsSyncOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PerformSyncInput struct {
	ID string `path:"id" doc:"Sync ID"`
}

type PerformSyncOutput struct {
	Body base.ApiResponse[gitops.SyncResult]
}

type GetSyncStatusInput struct {
	ID string `path:"id" doc:"Sync ID"`
}

type GetSyncStatusOutput struct {
	Body base.ApiResponse[gitops.SyncStatus]
}

type BrowseFilesInput struct {
	ID   string `path:"id" doc:"Sync ID"`
	Path string `query:"path" doc:"Path to browse (optional)"`
}

type BrowseFilesOutput struct {
	Body base.ApiResponse[gitops.BrowseResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterGitOpsSyncs registers all GitOps sync endpoints.
func RegisterGitOpsSyncs(api huma.API, syncService *services.GitOpsSyncService) {
	h := &GitOpsSyncHandler{syncService: syncService}

	huma.Register(api, huma.Operation{
		OperationID: "listGitOpsSyncs",
		Method:      "GET",
		Path:        "/gitops-syncs",
		Summary:     "List GitOps syncs",
		Description: "Get a paginated list of GitOps syncs",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListSyncs)

	huma.Register(api, huma.Operation{
		OperationID: "createGitOpsSync",
		Method:      "POST",
		Path:        "/gitops-syncs",
		Summary:     "Create a GitOps sync",
		Description: "Create a new GitOps sync configuration",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateSync)

	huma.Register(api, huma.Operation{
		OperationID: "getGitOpsSync",
		Method:      "GET",
		Path:        "/gitops-syncs/{id}",
		Summary:     "Get a GitOps sync",
		Description: "Get a GitOps sync by ID",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetSync)

	huma.Register(api, huma.Operation{
		OperationID: "updateGitOpsSync",
		Method:      "PUT",
		Path:        "/gitops-syncs/{id}",
		Summary:     "Update a GitOps sync",
		Description: "Update an existing GitOps sync configuration",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateSync)

	huma.Register(api, huma.Operation{
		OperationID: "deleteGitOpsSync",
		Method:      "DELETE",
		Path:        "/gitops-syncs/{id}",
		Summary:     "Delete a GitOps sync",
		Description: "Delete a GitOps sync configuration by ID",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteSync)

	huma.Register(api, huma.Operation{
		OperationID: "performGitOpsSync",
		Method:      "POST",
		Path:        "/gitops-syncs/{id}/sync",
		Summary:     "Perform a GitOps sync",
		Description: "Manually trigger a sync operation",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.PerformSync)

	huma.Register(api, huma.Operation{
		OperationID: "getGitOpsSyncStatus",
		Method:      "GET",
		Path:        "/gitops-syncs/{id}/status",
		Summary:     "Get GitOps sync status",
		Description: "Get the current status of a GitOps sync",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetStatus)

	huma.Register(api, huma.Operation{
		OperationID: "browseGitOpsSyncFiles",
		Method:      "GET",
		Path:        "/gitops-syncs/{id}/files",
		Summary:     "Browse GitOps sync files",
		Description: "Browse files in the synced repository",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.BrowseFiles)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListSyncs returns a paginated list of GitOps syncs.
func (h *GitOpsSyncHandler) ListSyncs(ctx context.Context, input *ListGitOpsSyncsInput) (*ListGitOpsSyncsOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParams(input.Page, input.Limit, input.SortCol, input.SortDir)

	syncs, paginationResp, err := h.syncService.GetSyncsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.GitOpsSyncListError{Err: err}).Error())
	}

	return &ListGitOpsSyncsOutput{
		Body: GitOpsSyncPaginatedResponse{
			Success: true,
			Data:    syncs,
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

// CreateSync creates a new GitOps sync.
func (h *GitOpsSyncHandler) CreateSync(ctx context.Context, input *CreateGitOpsSyncInput) (*CreateGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	sync, err := h.syncService.CreateSync(ctx, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncCreationError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.GitOpsSync, gitops.GitOpsSync](sync)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.GitOpsSyncMappingError{Err: mapErr}).Error())
	}

	return &CreateGitOpsSyncOutput{
		Body: base.ApiResponse[gitops.GitOpsSync]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetSync returns a GitOps sync by ID.
func (h *GitOpsSyncHandler) GetSync(ctx context.Context, input *GetGitOpsSyncInput) (*GetGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	sync, err := h.syncService.GetSyncByID(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncRetrievalError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.GitOpsSync, gitops.GitOpsSync](sync)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.GitOpsSyncMappingError{Err: mapErr}).Error())
	}

	return &GetGitOpsSyncOutput{
		Body: base.ApiResponse[gitops.GitOpsSync]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateSync updates an existing GitOps sync.
func (h *GitOpsSyncHandler) UpdateSync(ctx context.Context, input *UpdateGitOpsSyncInput) (*UpdateGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	sync, err := h.syncService.UpdateSync(ctx, input.ID, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncUpdateError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.GitOpsSync, gitops.GitOpsSync](sync)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.GitOpsSyncMappingError{Err: mapErr}).Error())
	}

	return &UpdateGitOpsSyncOutput{
		Body: base.ApiResponse[gitops.GitOpsSync]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteSync deletes a GitOps sync by ID.
func (h *GitOpsSyncHandler) DeleteSync(ctx context.Context, input *DeleteGitOpsSyncInput) (*DeleteGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.syncService.DeleteSync(ctx, input.ID); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncDeletionError{Err: err}).Error())
	}

	return &DeleteGitOpsSyncOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Sync deleted successfully",
			},
		},
	}, nil
}

// PerformSync manually triggers a sync operation.
func (h *GitOpsSyncHandler) PerformSync(ctx context.Context, input *PerformSyncInput) (*PerformSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	result, err := h.syncService.PerformSync(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncPerformError{Err: err}).Error())
	}

	return &PerformSyncOutput{
		Body: base.ApiResponse[gitops.SyncResult]{
			Success: result.Success,
			Data:    *result,
		},
	}, nil
}

// GetStatus returns the current status of a GitOps sync.
func (h *GitOpsSyncHandler) GetStatus(ctx context.Context, input *GetSyncStatusInput) (*GetSyncStatusOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	status, err := h.syncService.GetSyncStatus(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncStatusError{Err: err}).Error())
	}

	return &GetSyncStatusOutput{
		Body: base.ApiResponse[gitops.SyncStatus]{
			Success: true,
			Data:    *status,
		},
	}, nil
}

// BrowseFiles returns the file tree at the specified path in the repository.
func (h *GitOpsSyncHandler) BrowseFiles(ctx context.Context, input *BrowseFilesInput) (*BrowseFilesOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	response, err := h.syncService.BrowseFiles(ctx, input.ID, input.Path)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncBrowseError{Err: err}).Error())
	}

	return &BrowseFilesOutput{
		Body: base.ApiResponse[gitops.BrowseResponse]{
			Success: true,
			Data:    *response,
		},
	}, nil
}
