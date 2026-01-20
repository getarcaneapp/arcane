package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/jobschedule"
)

type GetJobSchedulesOutput struct {
	Body jobschedule.Config
}

type UpdateJobSchedulesInput struct {
	Body jobschedule.Update `doc:"Job schedule update data"`
}

type UpdateJobSchedulesOutput struct {
	Body base.ApiResponse[jobschedule.Config]
}

type GetJobsOutput struct {
	Body jobschedule.JobListResponse
}

type RunJobInput struct {
	JobID string `path:"jobId" minLength:"1" doc:"Job ID to run"`
}

type RunJobOutput struct {
	Body jobschedule.JobRunResponse
}

func RegisterJobSchedules(api huma.API, jobSvc *services.JobService) {
	h := &JobSchedulesHandler{jobService: jobSvc}

	huma.Register(api, huma.Operation{
		OperationID: "get-job-schedules",
		Method:      http.MethodGet,
		Path:        "/job-schedules",
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
		Path:        "/job-schedules",
		Summary:     "Update job schedules",
		Description: "Update background job cron schedules and reschedule running jobs",
		Tags:        []string{"JobSchedules"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "list-jobs",
		Method:      http.MethodGet,
		Path:        "/jobs",
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
		Path:        "/jobs/{jobId}/run",
		Summary:     "Run a job now",
		Description: "Manually trigger a background job to run immediately",
		Tags:        []string{"JobSchedules"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.RunJob)
}

type JobSchedulesHandler struct {
	jobService *services.JobService
}

func (h *JobSchedulesHandler) ListJobs(ctx context.Context, _ *struct{}) (*GetJobsOutput, error) {
	if h.jobService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	jobs, err := h.jobService.ListJobs(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &GetJobsOutput{Body: *jobs}, nil
}

func (h *JobSchedulesHandler) RunJob(ctx context.Context, input *RunJobInput) (*RunJobOutput, error) {
	if h.jobService == nil {
		return nil, huma.Error500InternalServerError("service not available")
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

func (h *JobSchedulesHandler) Get(ctx context.Context, _ *struct{}) (*GetJobSchedulesOutput, error) {
	if h.jobService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	cfg := h.jobService.GetJobSchedules(ctx)
	return &GetJobSchedulesOutput{Body: cfg}, nil
}

func (h *JobSchedulesHandler) Update(ctx context.Context, input *UpdateJobSchedulesInput) (*UpdateJobSchedulesOutput, error) {
	if h.jobService == nil {
		return nil, huma.Error500InternalServerError("service not available")
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
