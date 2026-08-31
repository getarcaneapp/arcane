package gitops

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
)

// GitOpsSyncHandler handles GitOps sync management endpoints.
type GitOpsSyncHandler struct {
	syncService *GitOpsSyncService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type ListGitOpsSyncsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListGitOpsSyncsOutput struct {
	Body base.PaginatedWithCounts[gitops.GitOpsSync, gitops.SyncCounts]
}

type CreateGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          gitops.CreateSyncRequest
}

type GetGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type UpdateGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
	Body          gitops.UpdateSyncRequest
}

type DeleteGitOpsSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type PerformSyncInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type GetSyncStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type BrowseSyncFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
	Path          string `query:"path" doc:"Path to browse (optional)"`
}

type ImportGitOpsSyncsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          []gitops.ImportGitOpsSyncRequest
}

// ============================================================================
// Registration
// ============================================================================

// RegisterGitOpsSyncs registers all GitOps sync endpoints.
func RegisterGitOpsSyncs(api huma.API, syncService *GitOpsSyncService) {
	h := &GitOpsSyncHandler{syncService: syncService}

	const basePath = "/environments/{id}/gitops-syncs"
	const syncPath = basePath + "/{syncId}"

	handlerutil.RegisterSecured(api, handlerutil.Operation("listGitOpsSyncs", "GET", basePath, "List GitOps syncs", "Get a paginated list of GitOps syncs for an environment", "GitOps Syncs"), authz.PermGitOpsList, h.ListSyncs)
	handlerutil.RegisterSecured(api, handlerutil.Operation("createGitOpsSync", "POST", basePath, "Create a GitOps sync", "Create a new GitOps sync configuration for an environment", "GitOps Syncs"), authz.PermGitOpsCreate, h.CreateSync)
	handlerutil.RegisterSecured(api, handlerutil.Operation("importGitOpsSyncs", "POST", basePath+"/import", "Import GitOps syncs", "Import multiple GitOps sync configurations from JSON", "GitOps Syncs"), authz.PermGitOpsCreate, h.ImportSyncs)
	handlerutil.RegisterSecured(api, handlerutil.Operation("getGitOpsSync", "GET", syncPath, "Get a GitOps sync", "Get a GitOps sync by ID", "GitOps Syncs"), authz.PermGitOpsRead, h.GetSync)
	handlerutil.RegisterSecured(api, handlerutil.Operation("updateGitOpsSync", "PUT", syncPath, "Update a GitOps sync", "Update an existing GitOps sync configuration", "GitOps Syncs"), authz.PermGitOpsUpdate, h.UpdateSync)
	handlerutil.RegisterSecured(api, handlerutil.Operation("deleteGitOpsSync", "DELETE", syncPath, "Delete a GitOps sync", "Delete a GitOps sync configuration by ID", "GitOps Syncs"), authz.PermGitOpsDelete, h.DeleteSync)
	handlerutil.RegisterSecured(api, handlerutil.Operation("performGitOpsSync", "POST", syncPath+"/sync", "Perform a GitOps sync", "Manually trigger a sync operation", "GitOps Syncs"), authz.PermGitOpsSync, h.PerformSync)
	handlerutil.RegisterSecured(api, handlerutil.Operation("getGitOpsSyncStatus", "GET", syncPath+"/status", "Get GitOps sync status", "Get the current status of a GitOps sync", "GitOps Syncs"), authz.PermGitOpsRead, h.GetStatus)
	handlerutil.RegisterSecured(api, handlerutil.Operation("browseGitOpsSyncFiles", "GET", syncPath+"/files", "Browse GitOps sync files", "Browse files in the synced repository", "GitOps Syncs"), authz.PermGitOpsRead, h.BrowseFiles)
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
	if ps, _ := middleware.PermissionsFromContext(ctx); ps.Allows(authz.PermGitOpsLifecycle, environmentID) {
		return nil
	}
	return huma.Error403Forbidden("configuring a pre-deploy lifecycle hook requires the " + authz.PermGitOpsLifecycle + " permission")
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListSyncs returns a paginated list of GitOps syncs.
func (h *GitOpsSyncHandler) ListSyncs(ctx context.Context, input *ListGitOpsSyncsInput) (*ListGitOpsSyncsOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	syncs, paginationResp, counts, err := h.syncService.GetSyncsPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list GitOps syncs").Error())
	}

	return &ListGitOpsSyncsOutput{
		Body: base.PaginatedWithCounts[gitops.GitOpsSync, gitops.SyncCounts]{
			Success:    true,
			Data:       syncs,
			Counts:     counts,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// CreateSync creates a new GitOps sync.
func (h *GitOpsSyncHandler) CreateSync(ctx context.Context, input *CreateGitOpsSyncInput) (*handlerutil.Out[gitops.GitOpsSync], error) {
	if err := requireLifecyclePermissionInternal(ctx, input.EnvironmentID, input.Body.HasPreDeployConfig()); err != nil {
		return nil, err
	}

	actor := handlerutil.CurrentActor(ctx)

	sync, err := h.syncService.CreateSync(ctx, input.EnvironmentID, input.Body, actor)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to create GitOps sync").Error())
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*project.GitOpsSync, gitops.GitOpsSync](sync, func(err error) string {
		return "Failed to map GitOps sync"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[gitops.GitOpsSync]{
		Body: body,
	}, nil
}

// ImportSyncs imports multiple GitOps syncs.
func (h *GitOpsSyncHandler) ImportSyncs(ctx context.Context, input *ImportGitOpsSyncsInput) (*handlerutil.Out[gitops.ImportGitOpsSyncResponse], error) {
	actor := handlerutil.CurrentActor(ctx)

	response, err := h.syncService.ImportSyncs(ctx, input.EnvironmentID, input.Body, actor)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[gitops.ImportGitOpsSyncResponse]{
		Body: base.ApiResponse[gitops.ImportGitOpsSyncResponse]{
			Success: true,
			Data:    *response,
		},
	}, nil
}

// GetSync returns a GitOps sync by ID.
func (h *GitOpsSyncHandler) GetSync(ctx context.Context, input *GetGitOpsSyncInput) (*handlerutil.Out[gitops.GitOpsSync], error) {
	sync, err := h.syncService.GetSyncByID(ctx, input.EnvironmentID, input.SyncID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve GitOps sync")
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*project.GitOpsSync, gitops.GitOpsSync](sync, func(err error) string {
		return "Failed to map GitOps sync"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[gitops.GitOpsSync]{
		Body: body,
	}, nil
}

// UpdateSync updates an existing GitOps sync.
func (h *GitOpsSyncHandler) UpdateSync(ctx context.Context, input *UpdateGitOpsSyncInput) (*handlerutil.Out[gitops.GitOpsSync], error) {
	if err := requireLifecyclePermissionInternal(ctx, input.EnvironmentID, input.Body.HasPreDeployConfig()); err != nil {
		return nil, err
	}

	actor := handlerutil.CurrentActor(ctx)

	sync, err := h.syncService.UpdateSync(ctx, input.EnvironmentID, input.SyncID, input.Body, actor)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to update GitOps sync").Error())
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*project.GitOpsSync, gitops.GitOpsSync](sync, func(err error) string {
		return "Failed to map GitOps sync"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[gitops.GitOpsSync]{
		Body: body,
	}, nil
}

// DeleteSync deletes a GitOps sync by ID.
func (h *GitOpsSyncHandler) DeleteSync(ctx context.Context, input *DeleteGitOpsSyncInput) (*handlerutil.Out[base.MessageResponse], error) {
	actor := handlerutil.CurrentActor(ctx)

	if err := h.syncService.DeleteSync(ctx, input.EnvironmentID, input.SyncID, actor); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete GitOps sync")
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Sync deleted successfully",
			},
		},
	}, nil
}

// PerformSync manually triggers a sync operation.
func (h *GitOpsSyncHandler) PerformSync(ctx context.Context, input *PerformSyncInput) (*handlerutil.Out[gitops.SyncResult], error) {
	actor := handlerutil.CurrentActor(ctx)

	result, err := h.syncService.PerformSync(ctx, input.EnvironmentID, input.SyncID, actor)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to perform GitOps sync")
	}

	return &handlerutil.Out[gitops.SyncResult]{
		Body: base.ApiResponse[gitops.SyncResult]{
			Success: result.Success,
			Data:    *result,
		},
	}, nil
}

// GetStatus returns the current status of a GitOps sync.
func (h *GitOpsSyncHandler) GetStatus(ctx context.Context, input *GetSyncStatusInput) (*handlerutil.Out[gitops.SyncStatus], error) {
	status, err := h.syncService.GetSyncStatus(ctx, input.EnvironmentID, input.SyncID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to get GitOps sync status")
	}

	return &handlerutil.Out[gitops.SyncStatus]{
		Body: base.ApiResponse[gitops.SyncStatus]{
			Success: true,
			Data:    *status,
		},
	}, nil
}

// BrowseFiles returns the file tree at the specified path in the repository.
func (h *GitOpsSyncHandler) BrowseFiles(ctx context.Context, input *BrowseSyncFilesInput) (*handlerutil.Out[gitops.BrowseResponse], error) {
	response, err := h.syncService.BrowseFiles(ctx, input.EnvironmentID, input.SyncID, input.Path)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to browse GitOps sync files")
	}

	return &handlerutil.Out[gitops.BrowseResponse]{
		Body: base.ApiResponse[gitops.BrowseResponse]{
			Success: true,
			Data:    *response,
		},
	}, nil
}
