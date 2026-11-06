package gitops

import (
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service *GitOpsSyncService
	}
)

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

func RegisterGitOpsSyncs(api huma.API, syncService *GitOpsSyncService) {
	h := &GitOpsSyncHandler{syncService: syncService}

	const basePath = "/environments/{id}/gitops/syncs"
	const syncPath = basePath + "/{syncId}"

	handlerutil.RegisterSecured(
		api,
		huma.Operation{
			OperationID: "listGitOpsSyncs",
			Method:      "GET",
			Path:        basePath,
			Summary:     "List GitOps syncs",
			Description: "Get a paginated list of GitOps syncs for an environment",
			Tags:        []string{"GitOps Syncs"},
			Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
		},
		authz.PermGitOpsList,
		h.ListSyncs,
	)
	handlerutil.RegisterSecured(
		api,
		huma.Operation{
			OperationID: "createGitOpsSync",
			Method:      http.MethodPost,
			Path:        basePath,
			Summary:     "Create a GitOps sync",
			Description: "Create a new GitOps sync configuration for an environment",
			Tags:        []string{"GitOps Syncs"},
			Metadata:    map[string]any{authz.MetaPermissionConditions: slices.Concat(authz.GitOpsPreDeployConditions, authz.GitOpsModeConditions)},
		},
		authz.PermGitOpsCreate,
		h.CreateSync,
	)
	handlerutil.RegisterSecured(
		api,
		huma.Operation{
			OperationID: "importGitOpsSyncs",
			Method:      http.MethodPost,
			Path:        basePath + "/import",
			Summary:     "Import GitOps syncs",
			Description: "Import multiple GitOps sync configurations from JSON",
			Tags:        []string{"GitOps Syncs"},
			Metadata:    map[string]any{authz.MetaPermissionConditions: authz.GitOpsPreDeployConditions},
		},
		authz.PermGitOpsCreate,
		h.ImportSyncs,
	)
	handlerutil.RegisterSecured(api, handlerutil.Operation("getGitOpsSync", "GET", syncPath, "Get a GitOps sync", "Get a GitOps sync by ID", "GitOps Syncs"), authz.PermGitOpsRead, h.GetSync)
	handlerutil.RegisterSecured(
		api,
		huma.Operation{
			OperationID: "updateGitOpsSync",
			Method:      http.MethodPatch,
			Path:        syncPath,
			Summary:     "Update a GitOps sync",
			Description: "Update an existing GitOps sync configuration",
			Tags:        []string{"GitOps Syncs"},
			Metadata:    map[string]any{authz.MetaPermissionConditions: slices.Concat(authz.GitOpsPreDeployConditions, authz.GitOpsSyncModeConditions)},
		},
		authz.PermGitOpsUpdate,
		h.UpdateSync,
	)
	handlerutil.RegisterSecured(
		api,
		huma.Operation{
			OperationID: "deleteGitOpsSync",
			Method:      http.MethodDelete,
			Path:        syncPath,
			Summary:     "Delete a GitOps sync",
			Description: "Delete a GitOps sync configuration by ID",
			Tags:        []string{"GitOps Syncs"},
			Metadata:    map[string]any{authz.MetaPermissionConditions: authz.GitOpsSyncModeConditions},
		},
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
			Metadata:    map[string]any{authz.MetaPermissionConditions: authz.GitOpsSyncModeConditions},
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
