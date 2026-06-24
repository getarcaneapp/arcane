package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
)

// gitOpsSyncHandler handles GitOps sync management endpoints.
type gitOpsSyncHandler struct {
	syncService *services.GitOpsSyncService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// gitOpsSyncPaginatedResponse is the paginated response for GitOps syncs.
type gitOpsSyncPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []gitops.GitOpsSync     `json:"data"`
	Counts     gitops.SyncCounts       `json:"counts"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type listGitOpsSyncsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Items per page"`
}

type listGitOpsSyncsOutput struct {
	Body gitOpsSyncPaginatedResponse
}

type createGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          gitops.CreateSyncRequest
}

type createGitOpsSyncOutput struct {
	Body base.ApiResponse[gitops.GitOpsSync]
}

type getGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type getGitOpsSyncOutput struct {
	Body base.ApiResponse[gitops.GitOpsSync]
}

type updateGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
	Body          gitops.UpdateSyncRequest
}

type updateGitOpsSyncOutput struct {
	Body base.ApiResponse[gitops.GitOpsSync]
}

type deleteGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type deleteGitOpsSyncOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type performSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type performSyncOutput struct {
	Body base.ApiResponse[gitops.SyncResult]
}

type getSyncStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type getSyncStatusOutput struct {
	Body base.ApiResponse[gitops.SyncStatus]
}

type browseSyncFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
	Path          string `query:"path" doc:"Path to browse (optional)"`
}

type browseSyncFilesOutput struct {
	Body base.ApiResponse[gitops.BrowseResponse]
}

type importGitOpsSyncsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          []gitops.ImportGitOpsSyncRequest
}

type importGitOpsSyncsOutput struct {
	Body base.ApiResponse[gitops.ImportGitOpsSyncResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterGitOpsSyncs registers all GitOps sync endpoints.
func RegisterGitOpsSyncs(api huma.API, syncService *services.GitOpsSyncService) {
	h := &gitOpsSyncHandler{syncService: syncService}

	registerGitOpsSecuredInternal(api, "listGitOpsSyncs", "GET", "/environments/{id}/gitops-syncs", "List GitOps syncs", "Get a paginated list of GitOps syncs for an environment", authz.PermGitOpsList, h.listSyncsInternal)
	registerGitOpsSecuredInternal(api, "createGitOpsSync", "POST", "/environments/{id}/gitops-syncs", "Create a GitOps sync", "Create a new GitOps sync configuration for an environment", authz.PermGitOpsCreate, h.createSyncInternal)
	registerGitOpsSecuredInternal(api, "importGitOpsSyncs", "POST", "/environments/{id}/gitops-syncs/import", "Import GitOps syncs", "Import multiple GitOps sync configurations from JSON", authz.PermGitOpsCreate, h.importSyncsInternal)
	registerGitOpsSecuredInternal(api, "getGitOpsSync", "GET", "/environments/{id}/gitops-syncs/{syncId}", "Get a GitOps sync", "Get a GitOps sync by ID", authz.PermGitOpsRead, h.getSyncInternal)
	registerGitOpsSecuredInternal(api, "updateGitOpsSync", "PUT", "/environments/{id}/gitops-syncs/{syncId}", "Update a GitOps sync", "Update an existing GitOps sync configuration", authz.PermGitOpsUpdate, h.updateSyncInternal)
	registerGitOpsSecuredInternal(api, "deleteGitOpsSync", "DELETE", "/environments/{id}/gitops-syncs/{syncId}", "Delete a GitOps sync", "Delete a GitOps sync configuration by ID", authz.PermGitOpsDelete, h.deleteSyncInternal)
	registerGitOpsSecuredInternal(api, "performGitOpsSync", "POST", "/environments/{id}/gitops-syncs/{syncId}/sync", "Perform a GitOps sync", "Manually trigger a sync operation", authz.PermGitOpsSync, h.performSyncInternal)
	registerGitOpsSecuredInternal(api, "getGitOpsSyncStatus", "GET", "/environments/{id}/gitops-syncs/{syncId}/status", "Get GitOps sync status", "Get the current status of a GitOps sync", authz.PermGitOpsRead, h.getStatusInternal)
	registerGitOpsSecuredInternal(api, "browseGitOpsSyncFiles", "GET", "/environments/{id}/gitops-syncs/{syncId}/files", "Browse GitOps sync files", "Browse files in the synced repository", authz.PermGitOpsRead, h.browseFilesInternal)
}

// requireLifecyclePermissionInternal rejects callers lacking gitops:lifecycle
// for the target environment when a create/update request configures the
// pre-deploy lifecycle hook. Configuring the hook lets the caller run an
// arbitrary container — with host bind mounts, env, and network access — on
// every sync, so it is gated behind its own permission (seeded only into the
// Admin built-in role) rather than the broader gitops:create / gitops:update
// permissions that non-admin roles such as Editor hold. Whether a request
// touches the hook is decided by the request type itself
// (gitops.*SyncRequest.HasPreDeployConfig) so the field set has a single owner.
func requireLifecyclePermissionInternal(ctx context.Context, environmentID string, lifecycleRequested bool) error {
	if !lifecycleRequested {
		return nil
	}
	if ps, _ := humamw.PermissionsFromContext(ctx); ps.Allows(authz.PermGitOpsLifecycle, environmentID) {
		return nil
	}
	return huma.Error403Forbidden("configuring a pre-deploy lifecycle hook requires the " + authz.PermGitOpsLifecycle + " permission")
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListSyncs returns a paginated list of GitOps syncs.
func (h *gitOpsSyncHandler) listSyncsInternal(ctx context.Context, input *listGitOpsSyncsInput) (*listGitOpsSyncsOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	syncs, paginationResp, counts, err := h.syncService.GetSyncsPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.GitOpsSyncListError{Err: err}).Error())
	}

	return &listGitOpsSyncsOutput{
		Body: gitOpsSyncPaginatedResponse{
			Success:    true,
			Data:       syncs,
			Counts:     counts,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// CreateSync creates a new GitOps sync.
func (h *gitOpsSyncHandler) createSyncInternal(ctx context.Context, input *createGitOpsSyncInput) (*createGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := requireLifecyclePermissionInternal(ctx, input.EnvironmentID, input.Body.HasPreDeployConfig()); err != nil {
		return nil, err
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

	return &createGitOpsSyncOutput{
		Body: body,
	}, nil
}

// ImportSyncs imports multiple GitOps syncs.
func (h *gitOpsSyncHandler) importSyncsInternal(ctx context.Context, input *importGitOpsSyncsInput) (*importGitOpsSyncsOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	response, err := h.syncService.ImportSyncs(ctx, input.EnvironmentID, input.Body, actor)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &importGitOpsSyncsOutput{
		Body: base.ApiResponse[gitops.ImportGitOpsSyncResponse]{
			Success: true,
			Data:    *response,
		},
	}, nil
}

// GetSync returns a GitOps sync by ID.
func (h *gitOpsSyncHandler) getSyncInternal(ctx context.Context, input *getGitOpsSyncInput) (*getGitOpsSyncOutput, error) {
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

	return &getGitOpsSyncOutput{
		Body: body,
	}, nil
}

// UpdateSync updates an existing GitOps sync.
func (h *gitOpsSyncHandler) updateSyncInternal(ctx context.Context, input *updateGitOpsSyncInput) (*updateGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := requireLifecyclePermissionInternal(ctx, input.EnvironmentID, input.Body.HasPreDeployConfig()); err != nil {
		return nil, err
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

	return &updateGitOpsSyncOutput{
		Body: body,
	}, nil
}

// DeleteSync deletes a GitOps sync by ID.
func (h *gitOpsSyncHandler) deleteSyncInternal(ctx context.Context, input *deleteGitOpsSyncInput) (*deleteGitOpsSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	if err := h.syncService.DeleteSync(ctx, input.EnvironmentID, input.SyncID, actor); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncDeletionError{Err: err}).Error())
	}

	return &deleteGitOpsSyncOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Sync deleted successfully",
			},
		},
	}, nil
}

// PerformSync manually triggers a sync operation.
func (h *gitOpsSyncHandler) performSyncInternal(ctx context.Context, input *performSyncInput) (*performSyncOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := currentActorInternal(ctx)

	result, err := h.syncService.PerformSync(ctx, input.EnvironmentID, input.SyncID, actor)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncPerformError{Err: err}).Error())
	}

	return &performSyncOutput{
		Body: base.ApiResponse[gitops.SyncResult]{
			Success: result.Success,
			Data:    *result,
		},
	}, nil
}

// GetStatus returns the current status of a GitOps sync.
func (h *gitOpsSyncHandler) getStatusInternal(ctx context.Context, input *getSyncStatusInput) (*getSyncStatusOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	status, err := h.syncService.GetSyncStatus(ctx, input.EnvironmentID, input.SyncID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncStatusError{Err: err}).Error())
	}

	return &getSyncStatusOutput{
		Body: base.ApiResponse[gitops.SyncStatus]{
			Success: true,
			Data:    *status,
		},
	}, nil
}

// BrowseFiles returns the file tree at the specified path in the repository.
func (h *gitOpsSyncHandler) browseFilesInternal(ctx context.Context, input *browseSyncFilesInput) (*browseSyncFilesOutput, error) {
	if h.syncService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	response, err := h.syncService.BrowseFiles(ctx, input.EnvironmentID, input.SyncID, input.Path)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.GitOpsSyncBrowseError{Err: err}).Error())
	}

	return &browseSyncFilesOutput{
		Body: base.ApiResponse[gitops.BrowseResponse]{
			Success: true,
			Data:    *response,
		},
	}, nil
}
