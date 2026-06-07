package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/jobschedule"
)

type GetJobSchedulesInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetJobSchedulesOutput struct {
	Body jobschedule.Config
}

type UpdateJobSchedulesInput struct {
	ID   string             `path:"id" doc:"Environment ID"`
	Body jobschedule.Update `doc:"Job schedule update data"`
}

type UpdateJobSchedulesOutput struct {
	Body base.ApiResponse[jobschedule.Config]
}

type ListJobsInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetJobsOutput struct {
	Body jobschedule.JobListResponse
}

type RunJobInput struct {
	ID    string `path:"id" doc:"Environment ID"`
	JobID string `path:"jobId" minLength:"1" doc:"Job ID to run"`
}

type RunJobOutput struct {
	Body jobschedule.JobRunResponse
}

func RegisterJobSchedules(api huma.API, jobSvc *services.JobService, envSvc *services.EnvironmentService) {
	h := &JobSchedulesHandler{
		jobService:         jobSvc,
		environmentService: envSvc,
	}

	huma.Register(api, huma.Operation{
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
	}, h.Get)

	huma.Register(api, huma.Operation{
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
		Middlewares: humamw.RequireAdmin(api),
	}, h.Update)

	huma.Register(api, huma.Operation{
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
	}, h.ListJobs)

	huma.Register(api, huma.Operation{
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
		Middlewares: humamw.RequireAdmin(api),
	}, h.RunJob)
}

type JobSchedulesHandler struct {
	jobService         *services.JobService
	environmentService *services.EnvironmentService
}

func (h *JobSchedulesHandler) ListJobs(ctx context.Context, input *ListJobsInput) (*GetJobsOutput, error) {
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
		return &GetJobsOutput{Body: *jobs}, nil
	}

	jobs, err := h.jobService.ListJobs(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &GetJobsOutput{Body: *jobs}, nil
}

func (h *JobSchedulesHandler) RunJob(ctx context.Context, input *RunJobInput) (*RunJobOutput, error) {
	if err := checkAdminInternal(ctx); err != nil {
		return nil, err
	}
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
		return &RunJobOutput{Body: *runResp}, nil
	}

	err := h.jobService.RunJobNowInline(ctx, input.JobID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	return &RunJobOutput{
		Body: jobschedule.JobRunResponse{
			Success: true,
			Message: "Job completed successfully",
		},
	}, nil
}

func (h *JobSchedulesHandler) Get(ctx context.Context, input *GetJobSchedulesInput) (*GetJobSchedulesOutput, error) {
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
		return &GetJobSchedulesOutput{Body: *cfg}, nil
	}

	cfg := h.jobService.GetJobSchedules(ctx)
	return &GetJobSchedulesOutput{Body: cfg}, nil
}

func (h *JobSchedulesHandler) Update(ctx context.Context, input *UpdateJobSchedulesInput) (*UpdateJobSchedulesOutput, error) {
	if err := checkAdminInternal(ctx); err != nil {
		return nil, err
	}
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

		return &UpdateJobSchedulesOutput{Body: *apiResp}, nil
	}

	cfg, err := h.jobService.UpdateJobSchedules(ctx, input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	return &UpdateJobSchedulesOutput{
		Body: base.ApiResponse[jobschedule.Config]{
			Success: true,
			Data:    cfg,
		},
	}, nil
}
