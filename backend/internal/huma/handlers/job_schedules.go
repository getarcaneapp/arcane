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

func RegisterJobSchedules(api huma.API, jobSvc *services.JobService) {
	h := &JobSchedulesHandler{jobService: jobSvc}

	huma.Register(api, huma.Operation{
		OperationID: "get-job-schedules",
		Method:      http.MethodGet,
		Path:        "/job-schedules",
		Summary:     "Get job schedules",
		Description: "Get configured intervals (in minutes) for background jobs",
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
		Description: "Update background job intervals and reschedule running jobs",
		Tags:        []string{"JobSchedules"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.Update)
}

type JobSchedulesHandler struct {
	jobService *services.JobService
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
