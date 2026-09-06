package job

import (
	"context"
	"net/http"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/queue"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

func (h *JobSchedulesHandler) registerRunRoutesInternal(api huma.API) {
	base := "/environments/{id}/jobs/{jobId}"
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-job-runs", Method: http.MethodGet, Path: base + "/runs", Summary: "List job runs", Security: handlerutil.DefaultOperationSecurity()}, authz.PermJobsManage, h.ListRuns)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-job-run", Method: http.MethodGet, Path: base + "/runs/{runId}", Summary: "Get job run", Security: handlerutil.DefaultOperationSecurity()}, authz.PermJobsManage, h.GetRun)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "retry-job-run", Method: http.MethodPost, Path: base + "/runs/{runId}/retry", Summary: "Retry job run", Security: handlerutil.DefaultOperationSecurity()}, authz.PermJobsManage, h.RetryRun)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "cancel-job-run", Method: http.MethodPost, Path: base + "/runs/{runId}/cancel", Summary: "Cancel pending job run", Security: handlerutil.DefaultOperationSecurity()}, authz.PermJobsManage, h.CancelRun)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "ack-job-run", Method: http.MethodPost, Path: base + "/runs/{runId}/ack", Summary: "Acknowledge remote run completion", Security: handlerutil.DefaultOperationSecurity()}, authz.PermJobsManage, h.AcknowledgeRun)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "restart-job-worker", Method: http.MethodPost, Path: base + "/restart", Summary: "Restart continuous job worker", Security: handlerutil.DefaultOperationSecurity()}, authz.PermJobsManage, h.RestartWorker)
}

// ListRuns returns paginated history for the requested job and environment.
// Remote history combines agent runs with the manager's delivery records.
func (h *JobSchedulesHandler) ListRuns(ctx context.Context, input *jobschedule.ListRunsInput) (*jobschedule.ListRunsOutput, error) {
	result, err := h.jobService.ListRuns(ctx, input.ID, input.JobID, input.Page, input.Limit)
	if err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	return &jobschedule.ListRunsOutput{Body: result}, nil
}

// GetRun returns a persisted run or receipt. For remote work, it queries the
// agent when the manager has no delivery record for the requested run.
func (h *JobSchedulesHandler) GetRun(ctx context.Context, input *jobschedule.RunInput) (*jobschedule.RunOutput, error) {
	result, err := h.jobService.GetRun(ctx, input.ID, input.JobID, input.RunID)
	if err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	return &jobschedule.RunOutput{Body: result}, nil
}

// RetryRun requests an explicit retry while preserving the run ID and target
// progress. Eligibility and remote-outcome checks are enforced by the service.
func (h *JobSchedulesHandler) RetryRun(ctx context.Context, input *jobschedule.RunInput) (*jobschedule.RunOutput, error) {
	result, err := h.jobService.RetryRun(ctx, input.ID, input.JobID, input.RunID)
	if err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	return &jobschedule.RunOutput{Body: result}, nil
}

// CancelRun cancels queued work only when it has never been attempted or sent
// remotely. Agent-owned cancellations are forwarded to the owning environment.
func (h *JobSchedulesHandler) CancelRun(ctx context.Context, input *jobschedule.RunInput) (*jobschedule.RunOutput, error) {
	result, err := h.jobService.CancelRun(ctx, input.ID, input.JobID, input.RunID)
	if err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	return &jobschedule.RunOutput{Body: result}, nil
}

// AcknowledgeRun marks a terminal run's remote delivery settled so retention can
// compact its history. Nonterminal runs cannot be acknowledged.
func (h *JobSchedulesHandler) AcknowledgeRun(ctx context.Context, input *jobschedule.RunInput) (*jobschedule.RunOutput, error) {
	result, err := h.jobService.GetRun(ctx, input.ID, input.JobID, input.RunID)
	if err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	err = h.jobService.Queue.UpdateRun(ctx, result, func(current *st.Run) error {
		if !current.Status.Terminal() {
			return errors.New("run is not terminal")
		}
		current.RemoteSettled = true
		current.UpdatedAt = time.Now().UTC()
		result = *current
		return nil
	})
	if err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	return &jobschedule.RunOutput{Body: result}, nil
}

// RestartWorker requests a continuous watcher's restart on the owning runtime,
// forwarding remote requests to the agent. It does not enqueue a scheduled run.
func (h *JobSchedulesHandler) RestartWorker(ctx context.Context, input *jobschedule.JobInput) (*RunJobOutput, error) {
	if input.ID != "0" {
		result, err := h.proxyRemoteJSON.JSON[jobschedule.JobRunResponse](ctx, input.ID, http.MethodPost, "/api/environments/0/jobs/"+input.JobID+"/restart", nil)
		if err != nil {
			return nil, err
		}
		return &RunJobOutput{Body: *result}, nil
	}
	runtime, ok := h.jobService.scheduler.(st.WorkerController)
	if !ok {
		return nil, huma.Error503ServiceUnavailable("Scheduler unavailable")
	}
	if err := runtime.RestartWatcher(ctx, input.JobID); err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	return &RunJobOutput{Body: jobschedule.JobRunResponse{Success: true, Message: "Worker restart requested"}}, nil
}

func jobHTTPErrorInternal(err error) error {
	if errors.Is(err, queue.ErrRunNotFound) {
		return huma.Error404NotFound("Job run not found")
	}
	if errors.Is(err, queue.ErrRunConflict) {
		return huma.Error409Conflict("Job run state changed")
	}
	return huma.Error400BadRequest(err.Error())
}

// RetryRun revalidates local job eligibility before requeuing the existing run.
// Remote retries follow the delivery protocol and retain confirmed target progress.
func (s *JobService) RetryRun(ctx context.Context, environmentID, jobID, runID string) (st.Run, error) {
	if environmentID != "0" {
		return s.RetryRemoteRun(ctx, environmentID, jobID, runID)
	}
	if environmentID == "0" {
		if err := s.validateLocalJobInternal(ctx, jobID); err != nil {
			return st.Run{}, err
		}
	}
	return s.Queue.Retry(ctx, environmentID, jobID, runID)
}
