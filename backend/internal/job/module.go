// Package job owns background-job schedule configuration and its HTTP surface.
package job

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service     *JobService
	environment *environment.EnvironmentService
}

func New(service *JobService, localEnvironment *environment.EnvironmentService) *Module {
	return &Module{service: service, environment: localEnvironment}
}

func (m *Module) Service() *JobService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterJobSchedules(api, nil, nil)
		return
	}
	RegisterJobSchedules(api, m.service, m.environment)
}

func RegisterJobSchedules(api huma.API, jobSvc *JobService, envSvc *environment.EnvironmentService) {
	h := &JobSchedulesHandler{
		jobService:      jobSvc,
		proxyRemoteJSON: envSvc.ProxyJSONRequest,
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-job-schedules",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/jobs/schedules",
		Summary:     "Get job schedules",
		Description: "Get configured cron schedules for background jobs",
		Tags:        []string{"JobSchedules"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermJobsManage, h.Get)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-job-schedules",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/jobs/schedules",
		Summary:     "Update job schedules",
		Description: "Update background job cron schedules and reschedule running jobs",
		Tags:        []string{"JobSchedules"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermJobsManage, h.Update)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-jobs",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/jobs",
		Summary:     "List all background jobs",
		Description: "Get status, schedule, and metadata for all background jobs",
		Tags:        []string{"JobSchedules"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermJobsManage, h.ListJobs)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID:   "job-action",
		DefaultStatus: http.StatusAccepted,
		Method:        http.MethodPost,
		Path:          "/environments/{id}/jobs/{jobId}",
		Summary:       "Run a job action",
		Description:   "Trigger a background job to run immediately or restart a continuous job worker",
		Tags:          []string{"JobSchedules"},
		Security:      handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"run":     authz.PermJobsManage,
		"restart": authz.PermJobsManage,
	}, h.RunJobAction)

	basePath := "/environments/{id}/jobs/{jobId}"
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-job-runs",
			Method:      http.MethodGet,
			Path:        basePath + "/runs",
			Summary:     "List job runs",
			Security:    handlerutil.DefaultOperationSecurity(),
		},
		authz.PermJobsManage,
		h.ListRuns,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-job-run",
			Method:      http.MethodGet,
			Path:        basePath + "/runs/{runId}",
			Summary:     "Get job run",
			Security:    handlerutil.DefaultOperationSecurity(),
		},
		authz.PermJobsManage,
		h.GetRun,
	)
	middleware.RegisterWithActionPermissions(
		api,
		huma.Operation{
			OperationID: "job-run-action",
			Method:      http.MethodPost,
			Path:        basePath + "/runs/{runId}",
			Summary:     "Retry, cancel, or acknowledge a job run",
			Security:    handlerutil.DefaultOperationSecurity(),
		},
		"action",
		map[string]string{
			"retry":  authz.PermJobsManage,
			"cancel": authz.PermJobsManage,
			"ack":    authz.PermJobsManage,
		},
		h.RunJobRunAction,
	)
}
