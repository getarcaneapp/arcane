package backup

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-project-backups",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/backups",
		Summary:     "List project backups",
		Description: "List the project's backups, newest first",
		Tags:        []string{"Project Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.List)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "create-project-backup",
		DefaultStatus: http.StatusAccepted,
		Method:        http.MethodPost,
		Path:          "/agents/{id}/projects/{projectId}/backups",
		Summary:       "Create project backup",
		Description:   "Back up the project's files and the data of its selected volumes as one snapshot; progress is reported through the returned activity",
		Tags:          []string{"Project Backup"},
		Security:      handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsBackup, h.Create)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-backup",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/backups/{backupId}",
		Summary:     "Get project backup",
		Tags:        []string{"Project Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.Get)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-project-backup",
		Method:      http.MethodDelete,
		Path:        "/agents/{id}/projects/{projectId}/backups/{backupId}",
		Summary:     "Delete project backup",
		Description: "Forget the backup's snapshots and delete it",
		Tags:        []string{"Project Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsBackup, h.Delete)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "restore-project-backup",
		DefaultStatus: http.StatusAccepted,
		Method:        http.MethodPost,
		Path:          "/agents/{id}/projects/{projectId}/backups/{backupId}/restore",
		Summary:       "Restore project backup",
		Description: "Put the project's files and the data of the selected volumes back to the backup's point in time, after a local safety backup that a " +
			"failed restore is rolled back to. Stopping a running project requires projects:down and projects:deploy; progress is reported through the returned activity",
		Tags:     []string{"Project Backup"},
		Security: handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRestore, h.Restore)
}
