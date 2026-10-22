// Package gitops owns GitOps synchronization, scheduling, persistence, and routes.
package gitops

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service *GitOpsSyncService
}

func New(service *GitOpsSyncService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *GitOpsSyncService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterGitOpsSyncs(api, nil)
		return
	}
	RegisterGitOpsSyncs(api, m.service)
}

// RegisterGitOpsSyncs registers all GitOps sync endpoints.
func RegisterGitOpsSyncs(api huma.API, syncService *GitOpsSyncService) {
	h := &GitOpsSyncHandler{syncService: syncService}

	const basePath = "/environments/{id}/gitops/syncs"
	const syncPath = basePath + "/{syncId}"

	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"listGitOpsSyncs",
			"GET",
			basePath,
			"List GitOps syncs",
			"Get a paginated list of GitOps syncs for an environment",
			"GitOps Syncs",
		),
		authz.PermGitOpsList,
		h.ListSyncs,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"createGitOpsSync",
			"POST",
			basePath,
			"Create a GitOps sync",
			"Create a new GitOps sync configuration for an environment",
			"GitOps Syncs",
		),
		authz.PermGitOpsCreate,
		h.CreateSync,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"importGitOpsSyncs",
			"POST",
			basePath+"/import",
			"Import GitOps syncs",
			"Import multiple GitOps sync configurations from JSON",
			"GitOps Syncs",
		),
		authz.PermGitOpsCreate,
		h.ImportSyncs,
	)
	handlerutil.RegisterSecured(api, handlerutil.Operation("getGitOpsSync", "GET", syncPath, "Get a GitOps sync", "Get a GitOps sync by ID", "GitOps Syncs"), authz.PermGitOpsRead, h.GetSync)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"updateGitOpsSync",
			"PATCH",
			syncPath,
			"Update a GitOps sync",
			"Update an existing GitOps sync configuration",
			"GitOps Syncs",
		),
		authz.PermGitOpsUpdate,
		h.UpdateSync,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"deleteGitOpsSync",
			"DELETE",
			syncPath,
			"Delete a GitOps sync",
			"Delete a GitOps sync configuration by ID",
			"GitOps Syncs",
		),
		authz.PermGitOpsDelete,
		h.DeleteSync,
	)
	middleware.RegisterWithActionPermissions(
		api,
		huma.Operation{
			OperationID: "gitOpsSyncAction",
			Method:      http.MethodPost,
			Path:        syncPath,
			Summary:     "Perform a GitOps sync",
			Description: "Manually trigger a sync operation",
			Tags:        []string{"GitOps Syncs"},
			Security:    handlerutil.DefaultOperationSecurity(),
		},
		"action",
		map[string]string{
			"sync": authz.PermGitOpsSync,
		},
		h.PerformSync,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"getGitOpsSyncStatus",
			"GET",
			syncPath+"/status",
			"Get GitOps sync status",
			"Get the current status of a GitOps sync",
			"GitOps Syncs",
		),
		authz.PermGitOpsRead,
		h.GetStatus,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"browseGitOpsSyncFiles",
			"GET",
			syncPath+"/files",
			"Browse GitOps sync files",
			"Browse files in the synced repository",
			"GitOps Syncs",
		),
		authz.PermGitOpsRead,
		h.BrowseFiles,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"previewGitOpsBackup",
			"GET",
			syncPath+"/backup/preview",
			"Preview a Git backup",
			"Show the files the next backup would commit and any conflicts",
			"GitOps Syncs",
		),
		authz.PermGitOpsBackup,
		h.PreviewBackup,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"listGitOpsBackupHistory",
			"GET",
			syncPath+"/backup/history",
			"List Git backup history",
			"List repository revisions that changed the backup",
			"GitOps Syncs",
		),
		authz.PermGitOpsBackup,
		h.BackupHistory,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"getGitOpsBackupRevision",
			"GET",
			syncPath+"/backup/history/{commit}",
			"Get a Git backup revision",
			"Get one revision with per-file diffs",
			"GitOps Syncs",
		),
		authz.PermGitOpsBackup,
		h.BackupRevision,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"resolveGitOpsBackupConflict",
			"POST",
			syncPath+"/backup/resolve",
			"Resolve a Git backup conflict",
			"Resolve a backup that needs attention",
			"GitOps Syncs",
		),
		authz.PermGitOpsBackup,
		h.ResolveBackupConflict,
	)
}
