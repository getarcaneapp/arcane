package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/gitops"
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
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListGitOpsSyncsOutput struct {
	Body GitOpsSyncPaginatedResponse
}

type CreateGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          gitops.CreateSyncRequest
}

type CreateGitOpsSyncOutput struct {
	Body base.ApiResponse[gitops.GitOpsSync]
}

type GetGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type GetGitOpsSyncOutput struct {
	Body base.ApiResponse[gitops.GitOpsSync]
}

type UpdateGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
	Body          gitops.UpdateSyncRequest
}

type UpdateGitOpsSyncOutput struct {
	Body base.ApiResponse[gitops.GitOpsSync]
}

type DeleteGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type DeleteGitOpsSyncOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PerformSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type PerformSyncOutput struct {
	Body base.ApiResponse[gitops.SyncResult]
}

type GetSyncStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type GetSyncStatusOutput struct {
	Body base.ApiResponse[gitops.SyncStatus]
}

type BrowseSyncFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
	Path          string `query:"path" doc:"Path to browse (optional)"`
}

type BrowseSyncFilesOutput struct {
	Body base.ApiResponse[gitops.BrowseResponse]
}

type ImportGitOpsSyncsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          []gitops.ImportGitOpsSyncRequest
}

type ImportGitOpsSyncsOutput struct {
	Body base.ApiResponse[gitops.ImportGitOpsSyncResponse]
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
		Path:        "/environments/{id}/gitops-syncs",
		Summary:     "List GitOps syncs",
		Description: "Get a paginated list of GitOps syncs for an environment",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListSyncs)

	huma.Register(api, huma.Operation{
		OperationID: "createGitOpsSync",
		Method:      "POST",
		Path:        "/environments/{id}/gitops-syncs",
		Summary:     "Create a GitOps sync",
		Description: "Create a new GitOps sync configuration for an environment",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateSync)

	huma.Register(api, huma.Operation{
		OperationID: "importGitOpsSyncs",
		Method:      "POST",
		Path:        "/environments/{id}/gitops-syncs/import",
		Summary:     "Import GitOps syncs",
		Description: "Import multiple GitOps sync configurations from JSON",
		Tags:        []string{"GitOps Syncs"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ImportSyncs)

	huma.Register(api, huma.Operation{
		OperationID: "getGitOpsSync",
		Method:      "GET",
		Path:        "/environments/{id}/gitops-syncs/{syncId}",
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
		Path:        "/environments/{id}/gitops-syncs/{syncId}",
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
		Path:        "/environments/{id}/gitops-syncs/{syncId}",
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
		Path:        "/environments/{id}/gitops-syncs/{syncId}/sync",
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
		Path:        "/environments/{id}/gitops-syncs/{syncId}/status",
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
		Path:        "/environments/{id}/gitops-syncs/{syncId}/files",
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

	params := buildPaginationParams(0, input.Start, input.Limit, input.Sort, input.Order, input.Search)

	syncs, paginationResp, err := h.syncService.GetSyncsPaginated(ctx, input.EnvironmentID, params)
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

	sync, err := h.syncService.CreateSync(ctx, input.EnvironmentID, input.Body)
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

// ImportSyncs imports multiple GitOps syncs.
func (h *GitOpsSyncHandler) ImportSyncs(ctx context.Context, input *ImportGitOpsSyncsInput) (*ImportGitOpsSyncsOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	response, err := h.syncService.ImportSyncs(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &ImportGitOpsSyncsOutput{
		Body: base.ApiResponse[gitops.ImportGitOpsSyncResponse]{
			Success: true,
			Data:    *response,
		},
	}, nil
}

// GetSync returns a GitOps sync by ID.
func (h *GitOpsSyncHandler) GetSync(ctx context.Context, input *GetGitOpsSyncInput) (*GetGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	sync, err := h.syncService.GetSyncByID(ctx, input.EnvironmentID, input.SyncID)
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

	sync, err := h.syncService.UpdateSync(ctx, input.EnvironmentID, input.SyncID, input.Body)
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

	if err := h.syncService.DeleteSync(ctx, input.EnvironmentID, input.SyncID); err != nil {
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

	result, err := h.syncService.PerformSync(ctx, input.EnvironmentID, input.SyncID)
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

	status, err := h.syncService.GetSyncStatus(ctx, input.EnvironmentID, input.SyncID)
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
func (h *GitOpsSyncHandler) BrowseFiles(ctx context.Context, input *BrowseSyncFilesInput) (*BrowseSyncFilesOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	response, err := h.syncService.BrowseFiles(ctx, input.EnvironmentID, input.SyncID, input.Path)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncBrowseError{Err: err}).Error())
	}

	return &BrowseSyncFilesOutput{
		Body: base.ApiResponse[gitops.BrowseResponse]{
			Success: true,
			Data:    *response,
		},
	}, nil
}
