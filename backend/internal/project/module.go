package project

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/middleware"
	projectbackup "getarcane.dev/arcane/v3/internal/project/children/backup"
	"getarcane.dev/arcane/v3/internal/project/children/bundle"
	"getarcane.dev/arcane/v3/internal/project/children/transfer"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service  *ProjectService
		activity *activity.ActivityService
		transfer *transfer.Service
		backups  *projectbackup.Service
		bundles  *bundle.Service
	}

	ModuleParams struct {
		Service  *ProjectService
		Activity *activity.ActivityService
		Transfer TransferDependencies
		Backups  BackupDependencies
	}

	// BackupDependencies are the shared backup plumbing project backups run on.
	BackupDependencies struct {
		Engine         *backup.Engine
		Repository     *backup.VolumeRepository
		S3Destinations *s3.S3DestinationService
		LockVolumes    func(ctx context.Context, names []string) (func(), error)
	}
)

func New(p ModuleParams) *Module {
	local := localTransferEnvironment{
		projects: p.Service,
		deps:     p.Transfer,
	}
	return &Module{
		service:  p.Service,
		activity: p.Activity,
		bundles: bundle.New(bundle.Dependencies{
			Environment:   local,
			Docker:        p.Service.dockerService,
			Uploads:       p.Service.uploads,
			ComposeFiles:  p.Service.LoadedComposeFiles,
			BackupStore:   p.Backups.Repository.StorageVolume,
			LockProject:   p.Service.LockProject,
			StopProject:   p.Service.stopForExport,
			StartProject:  p.Service.deployProject,
			RemoveProject: p.Service.destroyProject,
		}),
		transfer: transfer.NewService(transfer.Dependencies{
			Environment: transfer.Environments{
				Local:     local,
				Transport: p.Transfer.Transport,
				Tunnels:   p.Transfer.Tunnels,
			}.Get,
			RetargetWebhooks: p.Transfer.RetargetWebhooks,
			CountWebhooks:    p.Transfer.CountWebhooks,
			Activities:       p.Activity,
			Backups:          p.Transfer.Backups,
		}),
		backups: projectbackup.NewService(projectbackup.Dependencies{
			DB:             p.Service.db,
			Docker:         p.Service.dockerService,
			Engine:         p.Backups.Engine,
			Repository:     p.Backups.Repository,
			S3Destinations: p.Backups.S3Destinations,
			Settings:       p.Service.settingsService,
			LockVolumes:    p.Backups.LockVolumes,
			Project:        p.Service.GetProjectDetails,
			Volume:         local.Volume,
			BackupStore:    p.Backups.Repository.StorageVolume,
			ComposeProject: p.Service.composeProject,
			ComposeFiles:   p.Service.LoadedComposeFiles,
			LockProject:    p.Service.LockProject,
		}),
	}
}

func (m *Module) Service() *ProjectService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterWorkflows(engine *flow.Engine) error {
	if err := m.transfer.RegisterWorkflows(engine); err != nil {
		return err
	}
	return m.backups.RegisterWorkflows(engine)
}

// ReconcileInterruptedBackups fails project backups the previous process left running, except protected ones that resume.
func (m *Module) ReconcileInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	return m.backups.ReconcileInterruptedBackups(ctx, protectedIDs...)
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterProjects(api, &ProjectHandler{appCtx: appCtx.Context()})
		transfer.RegisterRoutes(api, transfer.NewHandler(nil))
		projectbackup.RegisterRoutes(api, projectbackup.NewHandler(nil, appCtx.Context()))
		return
	}
	RegisterProjects(api, &ProjectHandler{
		projectService:  m.service,
		activityService: m.activity,
		appCtx:          appCtx.Context(),
		bundles:         m.bundles,
	})
	transfer.RegisterRoutes(api, transfer.NewHandler(m.transfer))
	projectbackup.RegisterRoutes(api, projectbackup.NewHandler(m.backups, appCtx.Context()))
}

func RegisterProjects(api huma.API, h *ProjectHandler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-workspace",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/workspace",
		Summary:     "Get project workspace",
		Tags:        []string{"Project Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectWorkspace)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-workspace-file",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/workspace/file",
		Summary:     "Get project workspace file",
		Tags:        []string{"Project Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectWorkspaceFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "download-project-workspace-file",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/workspace/file/download",
		Summary:     "Download project workspace file",
		Tags:        []string{"Project Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.DownloadProjectWorkspaceFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project-workspace",
		Method:      http.MethodPatch,
		Path:        "/agents/{id}/projects/{projectId}/workspace",
		Summary:     "Update project workspace",
		Tags:        []string{"Project Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
		RequestBody: handlerutil.WorkspaceMultipartRequestBody("JSON encoded project workspace manifest"),
	}, authz.PermProjectsUpdate, h.UpdateProjectWorkspace)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-projects",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects",
		Summary:     "List projects",
		Description: "Get a paginated list of Docker Compose projects",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermProjectsList, h.ListProjects)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-status-counts",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/counts",
		Summary:     "Get project status counts",
		Description: "Get counts of running, stopped, and total projects",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermProjectsList, h.GetProjectStatusCounts)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "list-project-directories",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/directories",
		Summary:     "List projects directories",
		Description: "Get the configured projects directories and whether each one is available",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "view", map[string]string{
		"projects": authz.PermProjectsList,
		"settings": authz.PermEnvironmentSettingsRead,
	}, h.ListProjectsDirectories)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-project-tags",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/tags",
		Summary:     "List project tags",
		Description: "Get sorted, distinct project tag names",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermProjectsList, h.ListProjectTags)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-project-references",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/references",
		Summary:     "List project references",
		Description: "Get the ID and name of every project, including archived projects, without runtime state",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsList, h.ListProjectReferences)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project-tag",
		Method:      http.MethodPatch,
		Path:        "/agents/{id}/projects/{projectId}/tags",
		Summary:     "Update a project tag",
		Description: "Attach or detach a UI-managed project tag",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsUpdate, h.UpdateProjectTag)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "deploy-project",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/projects/{projectId}/deploy",
		Summary:     "Deploy, redeploy, or pull a project",
		Description: "Stream a Docker Compose deploy action: up (docker compose up), redeploy (pull + down + up), or pull (pull all project images). " +
			"The optional deploy options body applies to up and redeploy; pull ignores it.",
		Tags:     []string{"Projects"},
		Security: handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"up":       authz.PermProjectsDeploy,
		"redeploy": authz.PermProjectsDeploy,
		"pull":     authz.PermProjectsDeploy,
	}, h.DeployProject)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "project-action",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/projects/{projectId}",
		Summary:     "Run project action",
		Description: "Bring down, restart, archive, unarchive, or update services of a Docker Compose project. Restart and updateservices accept an optional services list (empty targets all services).",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"down":           authz.PermProjectsDown,
		"restart":        authz.PermProjectsRestart,
		"archive":        authz.PermProjectsArchive,
		"unarchive":      authz.PermProjectsArchive,
		"updateservices": authz.PermProjectsUpdate,
	}, h.RunProjectAction)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-project",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/projects",
		Summary:     "Create a project",
		Description: "Create a new Docker Compose project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
		RequestBody: &huma.RequestBody{
			Content: map[string]*huma.MediaType{
				"multipart/form-data": {
					Schema: &huma.Schema{
						Type: "object",
						Properties: map[string]*huma.Schema{
							"project":  {Type: "string", Description: "JSON encoded project configuration"},
							"manifest": {Type: "string", Description: "JSON encoded initial project workspace manifest"},
							"files":    {Type: "array", Items: &huma.Schema{Type: "string", Format: "binary"}},
						},
						Required: []string{"project", "manifest"},
					},
				},
			},
		},
	}, authz.PermProjectsCreate, h.CreateProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "import-project-archive",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/projects/import",
		Summary:     "Import project archive",
		Description: "Create a project from a complete project-archive upload session holding a project archive or a project bundle. " +
			"A bundle's volumes are restored too, which requires volumes:create and volumes:upload; start deploys the project and requires projects:deploy.",
		Tags:     []string{"Projects"},
		Security: handlerutil.DefaultOperationSecurity(),
		Metadata: map[string]any{authz.MetaPermissionConditions: authz.ProjectImportConditions},
	}, authz.PermProjectsCreate, h.ImportProjectArchive)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-export-options",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/export",
		Summary:     "Get project export options",
		Description: "List the project's volumes, which of them an export includes by default, and whether the project is running",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsExport, h.GetProjectExportOptions)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "export-project-archive",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/projects/{projectId}/export",
		Summary:     "Export project archive",
		Description: "Package the project's directory as a tar.gz into a complete project-archive upload session. " +
			"With volumes the session holds a project bundle with their data, which requires volumes:backup; stopProject also requires projects:down and projects:deploy.",
		Tags:     []string{"Projects"},
		Security: handlerutil.DefaultOperationSecurity(),
		Metadata: map[string]any{authz.MetaPermissionConditions: authz.ProjectExportConditions},
	}, authz.PermProjectsExport, h.ExportProjectArchive)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}",
		Summary:     "Get a project",
		Description: "Get a Docker Compose project by ID",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-compose",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/compose",
		Summary:     "Get project compose details",
		Description: "Get compose content, includes, and service configs for a project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectCompose)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-runtime",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/runtime",
		Summary:     "Get project runtime",
		Description: "Get runtime service state for a project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectRuntime)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-updates",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/projects/{projectId}/updates",
		Summary:     "Get project updates",
		Description: "Get image update summary for a project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectUpdates)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "destroy-project",
		Method:      http.MethodDelete,
		Path:        "/agents/{id}/projects/{projectId}",
		Summary:     "Destroy a project",
		Description: "Destroy a Docker Compose project and optionally remove files/volumes",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsDelete, h.DestroyProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project",
		Method:      http.MethodPatch,
		Path:        "/agents/{id}/projects/{projectId}",
		Summary:     "Update a project",
		Description: "Update a Docker Compose project configuration",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsUpdate, h.UpdateProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "run-project-job",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/projects/{projectId}/jobs/{jobName}/run",
		Summary:     "Run a project job",
		Description: "Stream a manual run of a Compose job (docker compose run <job>). Requires an engine with the jobs extension enabled.",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsJobsRun, h.RunProjectJob)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "build-project-images",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/projects/{projectId}/build",
		Summary:     "Build project images",
		Description: "Build Docker Compose services with build directives using BuildKit",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaPermissionConditions: authz.BuildPushConditions},
	}, authz.PermProjectsDeploy, h.BuildProjectImages)
}
