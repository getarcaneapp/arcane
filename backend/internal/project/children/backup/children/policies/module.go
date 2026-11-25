package policies

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-backup-policies",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/backups/policies",
		Summary:     "Get project backup policies",
		Description: "List the project's scheduled backup policies with the latest backup each one took",
		Tags:        []string{"Project Backup"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.Policies)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project-backup-policies",
		Method:      http.MethodPut,
		Path:        "/agents/{id}/projects/{projectId}/backups/policies",
		Summary:     "Update project backup policies",
		Description: "Replace the project's scheduled backup policies; scheduled backups beyond a policy's retention count are deleted, " +
			"and the backups of a removed policy are deleted once they have been unmanaged for the unmanaged backup retention",
		Tags:     []string{"Project Backup"},
		Security: handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsBackup, h.UpdatePolicies)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-global-project-backup-config",
		Method:      http.MethodGet,
		Path:        "/backups/projects/config",
		Summary:     "Get global project backup policies",
		Description: "List the policies that back up all or selected projects on this server, with the latest backup each one took",
		Tags:        []string{"Project Backup"},
	}, authz.PermSystemBackupsRead, h.GlobalPolicies)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-global-project-backup-config",
		Method:      http.MethodPut,
		Path:        "/backups/projects/config",
		Summary:     "Update global project backup policies",
		Description: "Replace the policies that back up all or selected projects on this server. A project's own enabled backup policy replaces " +
			"them for that project; each project keeps its newest retentionCount backups of each policy",
		Tags: []string{"Project Backup"},
	}, authz.PermSystemBackupsUpdate, h.UpdateGlobalPolicies)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-global-project-backup-options",
		Method:      http.MethodGet,
		Path:        "/backups/projects/options",
		Summary:     "List projects available to global project backups",
		Description: "List this server's projects, followed by the ones a policy lists that no longer exist",
		Tags:        []string{"Project Backup"},
	}, authz.PermSystemBackupsRead, h.GlobalOptions)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "run-global-project-backups",
		DefaultStatus: http.StatusAccepted,
		Method:        http.MethodPost,
		Path:          "/backups/projects/run",
		Summary:       "Run global project backups",
		Description:   "Back up the projects an enabled global policy selects now, as its schedule does; progress is reported through the returned activity",
		Tags:          []string{"Project Backup"},
	}, authz.PermSystemBackupsCreate, h.RunGlobal)
}
