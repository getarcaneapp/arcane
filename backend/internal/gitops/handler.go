package gitops

import (
	"cmp"
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/gitops"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// GitOpsSyncHandler handles GitOps sync management endpoints.
type GitOpsSyncHandler struct {
	syncService *GitOpsSyncService
}

type ListGitOpsSyncsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Items per page"`
	Mode          string `query:"mode" doc:"Filter by direction (deploy or backup)"`
	ProjectID     string `query:"projectId" doc:"Filter by linked project ID"`
	RepositoryID  string `query:"repositoryId" doc:"Filter by repository ID"`
	AutoSync      string `query:"autoSync" doc:"Filter by automatic sync (true or false)"`
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

const maxBackupHistoryLimit = 200

type BackupPreviewInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
}

type BackupHistoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
	Limit         int    `query:"limit" default:"20" doc:"Maximum number of revisions"`
}

type BackupRevisionInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
	Commit        string `path:"commit" doc:"Commit hash"`
}

type ResolveBackupConflictInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SyncID        string `path:"syncId" doc:"Sync ID"`
	Body          gitops.ResolveBackupConflictRequest
}

// requireLifecyclePermissionInternal rejects callers lacking gitops:lifecycle
// for the target environment when a create/update request configures the
// pre-deploy lifecycle hook. Configuring the hook lets the caller run an
// arbitrary container — with host bind mounts, env, and network access — on
// every sync, so it is gated behind its own permission (seeded only into the
// Admin built-in role) rather than the broader gitops:create / gitops:update
// permissions that non-admin roles such as Editor hold. Whether a request
// touches the hook is decided by the request type itself
// (gitops.PreDeployConfigRequest.HasPreDeployConfig) so the field set has a single owner.
func requireLifecyclePermissionInternal(ctx context.Context, environmentID string, lifecycleRequested bool) error {
	if !lifecycleRequested {
		return nil
	}
	if ps, _ := middleware.PermissionsFromContext(ctx); ps.Allows(authz.PermGitOpsLifecycle, environmentID) {
		return nil
	}
	return huma.Error403Forbidden("configuring a pre-deploy lifecycle hook requires the " + authz.PermGitOpsLifecycle + " permission")
}

// requireBackupPermissionInternal rejects callers lacking gitops:backup for
// the target environment when the operation touches a backup-mode sync.
func requireBackupPermissionInternal(ctx context.Context, environmentID string, backup bool) error {
	if !backup {
		return nil
	}
	if ps, _ := middleware.PermissionsFromContext(ctx); ps.Allows(authz.PermGitOpsBackup, environmentID) {
		return nil
	}
	return huma.Error403Forbidden("backing a project up to Git requires the " + authz.PermGitOpsBackup + " permission")
}

// requireBackupSyncPermissionInternal loads the sync and applies requireBackupPermissionInternal when it is a backup.
func (h *GitOpsSyncHandler) requireBackupSyncPermissionInternal(ctx context.Context, environmentID, syncID string) error {
	sync, err := h.syncService.GetSyncByID(ctx, environmentID, syncID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve GitOps sync")
	}
	return requireBackupPermissionInternal(ctx, environmentID, sync.Mode == gitops.SyncModeBackup)
}

// ListSyncs returns a paginated list of GitOps syncs.
func (h *GitOpsSyncHandler) ListSyncs(ctx context.Context, input *ListGitOpsSyncsInput) (*ListGitOpsSyncsOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	for key, value := range map[string]string{"mode": input.Mode, "projectId": input.ProjectID, "repositoryId": input.RepositoryID, "autoSync": input.AutoSync} {
		params.Filters[key] = cmp.Or(value, params.Filters[key])
	}

	syncs, paginationResp, counts, err := h.syncService.GetSyncsPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list GitOps syncs: " + err.Error())
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
	if err := requireBackupPermissionInternal(ctx, input.EnvironmentID, input.Body.Mode == gitops.SyncModeBackup); err != nil {
		return nil, err
	}

	actor := handlerutil.CurrentActor(ctx)

	sync, err := h.syncService.CreateSync(ctx, input.EnvironmentID, input.Body, actor)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to create GitOps sync: "+err.Error())
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
	for _, item := range input.Body {
		if err := requireLifecyclePermissionInternal(ctx, input.EnvironmentID, item.HasPreDeployConfig()); err != nil {
			return nil, err
		}
	}

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
	if err := h.requireBackupSyncPermissionInternal(ctx, input.EnvironmentID, input.SyncID); err != nil {
		return nil, err
	}

	actor := handlerutil.CurrentActor(ctx)

	sync, err := h.syncService.UpdateSync(ctx, input.EnvironmentID, input.SyncID, input.Body, actor)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to update GitOps sync: "+err.Error())
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
	if err := h.requireBackupSyncPermissionInternal(ctx, input.EnvironmentID, input.SyncID); err != nil {
		return nil, err
	}
	actor := handlerutil.CurrentActor(ctx)

	if err := h.syncService.DeleteSync(ctx, input.EnvironmentID, input.SyncID, actor); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete GitOps sync")
	}

	return handlerutil.MessageOutput("Sync deleted successfully", ""), nil
}

// PerformSync manually triggers a sync operation.
func (h *GitOpsSyncHandler) PerformSync(ctx context.Context, input *PerformSyncInput) (*handlerutil.Out[gitops.SyncResult], error) {
	if err := h.requireBackupSyncPermissionInternal(ctx, input.EnvironmentID, input.SyncID); err != nil {
		return nil, err
	}
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

// PreviewBackup shows what the next backup would commit.
func (h *GitOpsSyncHandler) PreviewBackup(ctx context.Context, input *BackupPreviewInput) (*handlerutil.Out[gitops.BackupPreview], error) {
	preview, err := h.syncService.PreviewBackup(ctx, input.EnvironmentID, input.SyncID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to preview Git backup: "+err.Error())
	}

	return &handlerutil.Out[gitops.BackupPreview]{
		Body: base.ApiResponse[gitops.BackupPreview]{
			Success: true,
			Data:    *preview,
		},
	}, nil
}

// BackupHistory lists revisions affecting the backup.
func (h *GitOpsSyncHandler) BackupHistory(ctx context.Context, input *BackupHistoryInput) (*handlerutil.Out[gitops.BackupHistoryResponse], error) {
	history, err := h.syncService.GetBackupHistory(ctx, input.EnvironmentID, input.SyncID, min(input.Limit, maxBackupHistoryLimit))
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to load Git backup history: "+err.Error())
	}

	return &handlerutil.Out[gitops.BackupHistoryResponse]{
		Body: base.ApiResponse[gitops.BackupHistoryResponse]{
			Success: true,
			Data:    *history,
		},
	}, nil
}

// BackupRevision returns one revision with diffs.
func (h *GitOpsSyncHandler) BackupRevision(ctx context.Context, input *BackupRevisionInput) (*handlerutil.Out[gitops.BackupRevision], error) {
	revision, err := h.syncService.GetBackupRevision(ctx, input.EnvironmentID, input.SyncID, input.Commit)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to load Git backup revision: "+err.Error())
	}

	return &handlerutil.Out[gitops.BackupRevision]{
		Body: base.ApiResponse[gitops.BackupRevision]{
			Success: true,
			Data:    *revision,
		},
	}, nil
}

// ResolveBackupConflict resolves a backup that needs attention.
func (h *GitOpsSyncHandler) ResolveBackupConflict(ctx context.Context, input *ResolveBackupConflictInput) (*handlerutil.Out[gitops.SyncResult], error) {
	actor := handlerutil.CurrentActor(ctx)

	result, err := h.syncService.ResolveBackupConflict(ctx, input.EnvironmentID, input.SyncID, input.Body, actor)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to resolve Git backup conflict: "+err.Error())
	}

	return &handlerutil.Out[gitops.SyncResult]{
		Body: base.ApiResponse[gitops.SyncResult]{
			Success: result.Success,
			Data:    *result,
		},
	}, nil
}
