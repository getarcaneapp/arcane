package job

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
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

type ListJobsInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetJobsOutput struct {
	Body jobschedule.JobListResponse
}

type RunJobInput struct {
	ID        string                      `path:"id" doc:"Environment ID"`
	JobID     string                      `path:"jobId" minLength:"1" doc:"Job ID to run"`
	RequestID string                      `header:"Idempotency-Key"`
	Body      *jobschedule.SubmitRunInput `required:"false"`
}

type RunJobOutput struct {
	Body jobschedule.JobRunResponse
}

func RegisterJobSchedules(api huma.API, jobSvc *JobService, envSvc *environment.EnvironmentService) {
	h := &JobSchedulesHandler{
		jobService:      jobSvc,
		proxyRemoteJSON: envSvc.ProxyJSONRequest,
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-job-schedules",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/job-schedules",
		Summary:     "Get job schedules",
		Description: "Get configured cron schedules for background jobs",
		Tags:        []string{"JobSchedules"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermJobsManage, h.Get)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-job-schedules",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/job-schedules",
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

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "run-job",
		DefaultStatus: http.StatusAccepted,
		Method:        http.MethodPost,
		Path:          "/environments/{id}/jobs/{jobId}/run",
		Summary:       "Run a job now",
		Description:   "Manually trigger a background job to run immediately",
		Tags:          []string{"JobSchedules"},
		Security:      handlerutil.DefaultOperationSecurity(),
	}, authz.PermJobsManage, h.RunJob)
	h.registerRunRoutesInternal(api)
}

type JobSchedulesHandler struct {
	jobService      *JobService
	proxyRemoteJSON handlerutil.RemoteJSONProxy
}

func (h *JobSchedulesHandler) ListJobs(ctx context.Context, input *ListJobsInput) (*GetJobsOutput, error) {
	var jobs *jobschedule.JobListResponse
	var err error
	if input.ID != "0" {
		jobs, err = h.jobService.ListRemoteJobs(ctx, input.ID)
	} else {
		jobs, err = h.jobService.ListJobs(ctx)
	}
	if err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	return &GetJobsOutput{Body: *jobs}, nil
}

func (h *JobSchedulesHandler) RunJob(ctx context.Context, input *RunJobInput) (*RunJobOutput, error) {
	runID := input.RequestID
	if input.Body != nil && input.Body.RunID != "" {
		if runID != "" && runID != input.Body.RunID {
			return nil, huma.Error400BadRequest("Conflicting run IDs")
		}
		runID = input.Body.RunID
	}
	userID, _ := middleware.GetUserIDFromContext(ctx)
	keyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	trigger := "manual"
	if h.jobService.cfg.AgentMode && userID == "agent" {
		userID = ""
		trigger = "remote"
	}
	run, err := h.jobService.Submit(ctx, st.Request{RunID: runID, JobID: input.JobID, EnvironmentID: input.ID, Trigger: trigger, RequestedBy: userID, RequestedWithKey: keyID})
	if err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	return &RunJobOutput{Body: jobschedule.JobRunResponse{Success: true, Message: "Job accepted", RunID: run.ID, Status: run.Status}}, nil
}

func (h *JobSchedulesHandler) Get(ctx context.Context, input *GetJobSchedulesInput) (*GetJobSchedulesOutput, error) {
	if input.ID != "0" {
		cfg, err := h.proxyRemoteJSON.JSON[jobschedule.Config](ctx, input.ID, http.MethodGet, "/api/environments/0/job-schedules", nil)
		if err != nil {
			return nil, err
		}
		return &GetJobSchedulesOutput{Body: *cfg}, nil
	}

	cfg := h.jobService.GetJobSchedules(ctx)
	return &GetJobSchedulesOutput{Body: cfg}, nil
}

func (h *JobSchedulesHandler) Update(ctx context.Context, input *UpdateJobSchedulesInput) (*handlerutil.Out[jobschedule.Config], error) {
	if input.ID != "0" {
		apiResp, err := h.proxyRemoteJSON.JSON[base.ApiResponse[jobschedule.Config]](ctx, input.ID, http.MethodPut, "/api/environments/0/job-schedules", input.Body)
		if err != nil {
			return nil, err
		}

		return &handlerutil.Out[jobschedule.Config]{Body: *apiResp}, nil
	}

	cfg, err := h.jobService.UpdateJobSchedules(ctx, input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	return &handlerutil.Out[jobschedule.Config]{
		Body: base.ApiResponse[jobschedule.Config]{
			Success: true,
			Data:    cfg,
		},
	}, nil
}
