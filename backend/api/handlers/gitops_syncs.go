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
	Counts     gitops.SyncCounts       `json:"counts"`
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

	registerGitOpsSecuredInternal(api, "listGitOpsSyncs", "GET", "/environments/{id}/gitops-syncs", "List GitOps syncs", "Get a paginated list of GitOps syncs for an environment", authz.PermGitOpsList, h.ListSyncs)
	registerGitOpsSecuredInternal(api, "createGitOpsSync", "POST", "/environments/{id}/gitops-syncs", "Create a GitOps sync", "Create a new GitOps sync configuration for an environment", authz.PermGitOpsCreate, h.CreateSync)
	registerGitOpsSecuredInternal(api, "importGitOpsSyncs", "POST", "/environments/{id}/gitops-syncs/import", "Import GitOps syncs", "Import multiple GitOps sync configurations from JSON", authz.PermGitOpsCreate, h.ImportSyncs)
	registerGitOpsSecuredInternal(api, "getGitOpsSync", "GET", "/environments/{id}/gitops-syncs/{syncId}", "Get a GitOps sync", "Get a GitOps sync by ID", authz.PermGitOpsRead, h.GetSync)
	registerGitOpsSecuredInternal(api, "updateGitOpsSync", "PUT", "/environments/{id}/gitops-syncs/{syncId}", "Update a GitOps sync", "Update an existing GitOps sync configuration", authz.PermGitOpsUpdate, h.UpdateSync)
	registerGitOpsSecuredInternal(api, "deleteGitOpsSync", "DELETE", "/environments/{id}/gitops-syncs/{syncId}", "Delete a GitOps sync", "Delete a GitOps sync configuration by ID", authz.PermGitOpsDelete, h.DeleteSync)
	registerGitOpsSecuredInternal(api, "performGitOpsSync", "POST", "/environments/{id}/gitops-syncs/{syncId}/sync", "Perform a GitOps sync", "Manually trigger a sync operation", authz.PermGitOpsSync, h.PerformSync)
	registerGitOpsSecuredInternal(api, "getGitOpsSyncStatus", "GET", "/environments/{id}/gitops-syncs/{syncId}/status", "Get GitOps sync status", "Get the current status of a GitOps sync", authz.PermGitOpsRead, h.GetStatus)
	registerGitOpsSecuredInternal(api, "browseGitOpsSyncFiles", "GET", "/environments/{id}/gitops-syncs/{syncId}/files", "Browse GitOps sync files", "Browse files in the synced repository", authz.PermGitOpsRead, h.BrowseFiles)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListSyncs returns a paginated list of GitOps syncs.
func (h *GitOpsSyncHandler) ListSyncs(ctx context.Context, input *ListGitOpsSyncsInput) (*ListGitOpsSyncsOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	syncs, paginationResp, counts, err := h.syncService.GetSyncsPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.GitOpsSyncListError{Err: err}).Error())
	}

	return &ListGitOpsSyncsOutput{
		Body: GitOpsSyncPaginatedResponse{
			Success:    true,
			Data:       syncs,
			Counts:     counts,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// CreateSync creates a new GitOps sync.
func (h *GitOpsSyncHandler) CreateSync(ctx context.Context, input *CreateGitOpsSyncInput) (*CreateGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	sync, err := h.syncService.CreateSync(ctx, input.EnvironmentID, input.Body, actor)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncCreationError{Err: err}).Error())
	}

	body, mapErr := mapOneAPIResponseInternal[*models.GitOpsSync, gitops.GitOpsSync](sync, func(err error) string {
		return (&common.GitOpsSyncMappingError{Err: err}).Error()
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &CreateGitOpsSyncOutput{
		Body: body,
	}, nil
}

// ImportSyncs imports multiple GitOps syncs.
func (h *GitOpsSyncHandler) ImportSyncs(ctx context.Context, input *ImportGitOpsSyncsInput) (*ImportGitOpsSyncsOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	response, err := h.syncService.ImportSyncs(ctx, input.EnvironmentID, input.Body, actor)
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

	body, mapErr := mapOneAPIResponseInternal[*models.GitOpsSync, gitops.GitOpsSync](sync, func(err error) string {
		return (&common.GitOpsSyncMappingError{Err: err}).Error()
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &GetGitOpsSyncOutput{
		Body: body,
	}, nil
}

// UpdateSync updates an existing GitOps sync.
func (h *GitOpsSyncHandler) UpdateSync(ctx context.Context, input *UpdateGitOpsSyncInput) (*UpdateGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	sync, err := h.syncService.UpdateSync(ctx, input.EnvironmentID, input.SyncID, input.Body, actor)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncUpdateError{Err: err}).Error())
	}

	body, mapErr := mapOneAPIResponseInternal[*models.GitOpsSync, gitops.GitOpsSync](sync, func(err error) string {
		return (&common.GitOpsSyncMappingError{Err: err}).Error()
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &UpdateGitOpsSyncOutput{
		Body: body,
	}, nil
}

// DeleteSync deletes a GitOps sync by ID.
func (h *GitOpsSyncHandler) DeleteSync(ctx context.Context, input *DeleteGitOpsSyncInput) (*DeleteGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	if err := h.syncService.DeleteSync(ctx, input.EnvironmentID, input.SyncID, actor); err != nil {
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

	actor := currentActorInternal(ctx)

	result, err := h.syncService.PerformSync(ctx, input.EnvironmentID, input.SyncID, actor)
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
