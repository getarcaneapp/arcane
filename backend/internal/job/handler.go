package job

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type GetJobSchedulesInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetJobSchedulesOutput struct {
	Body types.JobScheduleConfig
}

type UpdateJobSchedulesInput struct {
	ID string `path:"id" doc:"Environment ID"`
	types.Update[types.JobScheduleConfig]
}

type ListJobsInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetJobsOutput struct {
	Body types.JobListResponse
}

type JobActionInput struct {
	ID        string                   `path:"id" doc:"Environment ID"`
	JobID     string                   `path:"jobId" minLength:"1" doc:"Job ID"`
	Action    string                   `query:"action" required:"true" enum:"run,restart" doc:"Job action"`
	RequestID string                   `header:"Idempotency-Key"`
	Body      *types.JobSubmitRunInput `required:"false"`
}

type JobRunActionInput struct {
	ID     string `path:"id"`
	JobID  string `path:"jobId" minLength:"1"`
	RunID  string `path:"runId" format:"uuid"`
	Action string `query:"action" required:"true" enum:"retry,cancel,ack" doc:"Job run action"`
}

type RunJobOutput struct {
	Body types.JobRunResponse
}

type JobSchedulesHandler struct {
	jobService      *JobService
	proxyRemoteJSON handlerutil.RemoteJSONProxy
}

func (h *JobSchedulesHandler) ListJobs(ctx context.Context, input *ListJobsInput) (*GetJobsOutput, error) {
	jobs, err := func() (*types.JobListResponse, error) {
		if input.ID != "0" {
			return h.jobService.ListRemoteJobs(ctx, input.ID)
		}
		return h.jobService.ListJobs(ctx)
	}()
	if err != nil {
		return nil, jobHTTPError(err)
	}
	return &GetJobsOutput{Body: *jobs}, nil
}

func (h *JobSchedulesHandler) RunJobAction(ctx context.Context, input *JobActionInput) (*RunJobOutput, error) {
	if input.Action == "restart" {
		return h.restartWorker(ctx, input)
	}
	return h.runJob(ctx, input)
}

func (h *JobSchedulesHandler) runJob(ctx context.Context, input *JobActionInput) (*RunJobOutput, error) {
	if kit.FromPtr(input.Body).RunID != "" && input.RequestID != "" && input.RequestID != kit.FromPtr(input.Body).RunID {
		return nil, huma.Error400BadRequest("Conflicting run IDs")
	}
	userID, _ := middleware.GetUserIDFromContext(ctx)
	keyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	run, err := h.jobService.Submit(ctx, types.SchedulerRequest{
		RunID:            cmp.Or(kit.FromPtr(input.Body).RunID, input.RequestID),
		JobID:            input.JobID,
		EnvironmentID:    input.ID,
		Trigger:          kit.Ternary(h.jobService.cfg.AgentMode && userID == "agent", "remote", "manual"),
		RequestedWithKey: keyID,
		RequestedBy:      kit.Ternary(h.jobService.cfg.AgentMode && userID == "agent", "", userID),
	})
	if err != nil {
		return nil, jobHTTPError(err)
	}
	return &RunJobOutput{
		Body: types.JobRunResponse{
			Success: true,
			Message: "Job accepted",
			RunID:   run.ID,
			Status:  run.Status,
		},
	}, nil
}

func (h *JobSchedulesHandler) Get(ctx context.Context, input *GetJobSchedulesInput) (*GetJobSchedulesOutput, error) {
	if input.ID != "0" {
		cfg, err := h.proxyRemoteJSON.JSON[types.JobScheduleConfig](ctx, input.ID, http.MethodGet, "/api/environments/0/jobs/schedules", nil)
		if err != nil {
			return nil, err
		}
		return &GetJobSchedulesOutput{Body: *cfg}, nil
	}

	cfg := h.jobService.GetJobSchedules(ctx)
	return &GetJobSchedulesOutput{Body: cfg}, nil
}

func (h *JobSchedulesHandler) Update(ctx context.Context, input *UpdateJobSchedulesInput) (*handlerutil.Out[types.JobScheduleConfig], error) {
	if input.ID != "0" {
		apiResp, err := h.proxyRemoteJSON.JSON[types.ApiResponse[types.JobScheduleConfig]](ctx, input.ID, http.MethodPatch, "/api/environments/0/jobs/schedules", input.RawBody)
		if err != nil {
			return nil, err
		}

		return &handlerutil.Out[types.JobScheduleConfig]{Body: *apiResp}, nil
	}

	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid job schedule update body")
	}
	cfg, err := h.jobService.UpdateJobSchedules(ctx, input.Body, submitted)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	return &handlerutil.Out[types.JobScheduleConfig]{
		Body: types.ApiResponse[types.JobScheduleConfig]{
			Success: true,
			Data:    cfg,
		},
	}, nil
}

// ListRuns returns paginated history for the requested job and environment.
// Remote history combines agent runs with the manager's delivery records.
func (h *JobSchedulesHandler) ListRuns(ctx context.Context, input *types.JobListRunsInput) (*types.JobListRunsOutput, error) {
	result, err := h.jobService.ListRuns(ctx, input.ID, input.JobID, input.Page, input.Limit)
	if err != nil {
		return nil, jobHTTPError(err)
	}
	return &types.JobListRunsOutput{Body: result}, nil
}

// GetRun returns a persisted run or receipt. For remote work, it queries the
// agent when the manager has no delivery record for the requested run.
func (h *JobSchedulesHandler) GetRun(ctx context.Context, input *types.JobRunInput) (*types.JobRunOutput, error) {
	result, err := h.jobService.GetRun(ctx, input.ID, input.JobID, input.RunID)
	if err != nil {
		return nil, jobHTTPError(err)
	}
	return &types.JobRunOutput{Body: result}, nil
}

func (h *JobSchedulesHandler) RunJobRunAction(ctx context.Context, input *JobRunActionInput) (*types.JobRunOutput, error) {
	run := map[string]func(context.Context, string, string, string) (types.SchedulerRun, error){
		"retry":  h.jobService.RetryRun,
		"cancel": h.jobService.CancelRun,
		"ack":    h.acknowledgeRun,
	}[input.Action]
	result, err := run(ctx, input.ID, input.JobID, input.RunID)
	if err != nil {
		return nil, jobHTTPError(err)
	}
	return &types.JobRunOutput{Body: result}, nil
}

func (h *JobSchedulesHandler) acknowledgeRun(ctx context.Context, environmentID, jobID, runID string) (types.SchedulerRun, error) {
	result, err := h.jobService.GetRun(ctx, environmentID, jobID, runID)
	if err != nil {
		return types.SchedulerRun{}, err
	}
	err = h.jobService.runs.UpdateRun(ctx, result, func(current *types.SchedulerRun) error {
		if !current.Status.Terminal() {
			return errors.New("run is not terminal")
		}
		*current = types.SchedulerRun{ //exhaustruct:enforce
			ActivityEnvironmentID:   current.ActivityEnvironmentID,
			ActivityID:              current.ActivityID,
			RequestedWithKey:        current.RequestedWithKey,
			ID:                      current.ID,
			JobID:                   current.JobID,
			EnvironmentID:           current.EnvironmentID,
			Trigger:                 current.Trigger,
			RequestedBy:             current.RequestedBy,
			Status:                  current.Status,
			CreatedAt:               current.CreatedAt,
			UpdatedAt:               time.Now().UTC(),
			StartedAt:               current.StartedAt,
			FinishedAt:              current.FinishedAt,
			NextAttempt:             current.NextAttempt,
			AttemptCount:            current.AttemptCount,
			Owner:                   current.Owner,
			Outcome:                 current.Outcome,
			Attempts:                current.Attempts,
			RemoteDeliveryAttempted: current.RemoteDeliveryAttempted,
			RemoteOutcome:           current.RemoteOutcome,
			RemoteRetryRequested:    current.RemoteRetryRequested,
			RemoteRetryAttempted:    current.RemoteRetryAttempted,
			RemoteAttemptCount:      current.RemoteAttemptCount,
			RemoteAccepted:          current.RemoteAccepted,
			RemoteSettled:           true,
			LastConfirmedAt:         current.LastConfirmedAt,
		}
		result = *current
		return nil
	})
	return result, err
}

func (h *JobSchedulesHandler) restartWorker(ctx context.Context, input *JobActionInput) (*RunJobOutput, error) {
	if input.ID != "0" {
		result, err := h.proxyRemoteJSON.JSON[types.JobRunResponse](ctx, input.ID, http.MethodPost, "/api/environments/0/jobs/"+input.JobID+"?action=restart", nil)
		if err != nil {
			return nil, err
		}
		return &RunJobOutput{Body: *result}, nil
	}
	workers, ok := h.jobService.scheduler.(types.WorkerController)
	if !ok {
		return nil, huma.Error503ServiceUnavailable("Scheduler unavailable")
	}
	if err := workers.RestartWatcher(ctx, input.JobID); err != nil {
		return nil, jobHTTPError(err)
	}
	return &RunJobOutput{
		Body: types.JobRunResponse{
			Success: true,
			Message: "Worker restart requested",
		},
	}, nil
}

func jobHTTPError(err error) error {
	if _, ok := errors.AsType[huma.StatusError](err); ok {
		return err
	}
	if errors.Is(err, runs.ErrRunNotFound) {
		return huma.Error404NotFound("Job run not found")
	}
	if errors.Is(err, runs.ErrRunConflict) {
		return huma.Error409Conflict("Job run state changed")
	}
	return huma.Error400BadRequest(err.Error())
}
