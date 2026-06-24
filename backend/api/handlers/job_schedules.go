package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
)

type getJobSchedulesInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type getJobSchedulesOutput struct {
	Body jobschedule.Config
}

type updateJobSchedulesInput struct {
	ID   string             `path:"id" doc:"Environment ID"`
	Body jobschedule.Update `doc:"Job schedule update data"`
}

type updateJobSchedulesOutput struct {
	Body base.ApiResponse[jobschedule.Config]
}

type listJobsInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type getJobsOutput struct {
	Body jobschedule.JobListResponse
}

type runJobInput struct {
	ID    string `path:"id" doc:"Environment ID"`
	JobID string `path:"jobId" minLength:"1" doc:"Job ID to run"`
}

type runJobOutput struct {
	Body jobschedule.JobRunResponse
}

func RegisterJobSchedules(api huma.API, jobSvc *services.JobService, envSvc *services.EnvironmentService) {
	h := &jobSchedulesHandler{
		jobService:         jobSvc,
		environmentService: envSvc,
	}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-job-schedules",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/job-schedules",
		Summary:     "Get job schedules",
		Description: "Get configured cron schedules for background jobs",
		Tags:        []string{"JobSchedules"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermJobsManage, h.getInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-job-schedules",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/job-schedules",
		Summary:     "Update job schedules",
		Description: "Update background job cron schedules and reschedule running jobs",
		Tags:        []string{"JobSchedules"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermJobsManage, h.updateInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-jobs",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/jobs",
		Summary:     "List all background jobs",
		Description: "Get status, schedule, and metadata for all background jobs",
		Tags:        []string{"JobSchedules"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermJobsManage, h.listJobsInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "run-job",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/jobs/{jobId}/run",
		Summary:     "Run a job now",
		Description: "Manually trigger a background job to run immediately",
		Tags:        []string{"JobSchedules"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermJobsManage, h.runJobInternal)
}

type jobSchedulesHandler struct {
	jobService         *services.JobService
	environmentService *services.EnvironmentService
}

func (h *jobSchedulesHandler) listJobsInternal(ctx context.Context, input *listJobsInput) (*getJobsOutput, error) {
	if h.jobService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		jobs, err := proxyRemoteJSONInternal[jobschedule.JobListResponse](ctx, h.environmentService, input.ID, http.MethodGet, "/api/environments/0/jobs", nil)
		if err != nil {
			return nil, err
		}
		return &getJobsOutput{Body: *jobs}, nil
	}

	jobs, err := h.jobService.ListJobs(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &getJobsOutput{Body: *jobs}, nil
}

func (h *jobSchedulesHandler) runJobInternal(ctx context.Context, input *runJobInput) (*runJobOutput, error) {
	if h.jobService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		runResp, err := proxyRemoteJSONInternal[jobschedule.JobRunResponse](ctx, h.environmentService, input.ID, http.MethodPost, "/api/environments/0/jobs/"+input.JobID+"/run", nil)
		if err != nil {
			return nil, err
		}
		return &runJobOutput{Body: *runResp}, nil
	}

	err := h.jobService.RunJobNowInline(ctx, input.JobID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	return &runJobOutput{
		Body: jobschedule.JobRunResponse{
			Success: true,
			Message: "Job completed successfully",
		},
	}, nil
}

func (h *jobSchedulesHandler) getInternal(ctx context.Context, input *getJobSchedulesInput) (*getJobSchedulesOutput, error) {
	if h.jobService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		cfg, err := proxyRemoteJSONInternal[jobschedule.Config](ctx, h.environmentService, input.ID, http.MethodGet, "/api/environments/0/job-schedules", nil)
		if err != nil {
			return nil, err
		}
		return &getJobSchedulesOutput{Body: *cfg}, nil
	}

	cfg := h.jobService.GetJobSchedules(ctx)
	return &getJobSchedulesOutput{Body: cfg}, nil
}

func (h *jobSchedulesHandler) updateInternal(ctx context.Context, input *updateJobSchedulesInput) (*updateJobSchedulesOutput, error) {
	if h.jobService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}

		apiResp, err := proxyRemoteJSONInternal[base.ApiResponse[jobschedule.Config]](ctx, h.environmentService, input.ID, http.MethodPut, "/api/environments/0/job-schedules", input.Body)
		if err != nil {
			return nil, err
		}

		return &updateJobSchedulesOutput{Body: *apiResp}, nil
	}

	cfg, err := h.jobService.UpdateJobSchedules(ctx, input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	return &updateJobSchedulesOutput{
		Body: base.ApiResponse[jobschedule.Config]{
			Success: true,
			Data:    cfg,
		},
	}, nil
}
