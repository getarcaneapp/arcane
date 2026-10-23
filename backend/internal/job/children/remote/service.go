// Package remote owns manager-side delivery, history and review of job runs
// executed on remote agents.
package remote

import (
	"context"
	"encoding/json/v2"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
)

// Service talks to agents through the environment proxy and persists delivery
// state in the shared run coordinator.
type Service struct {
	runs         *runs.Coordinator
	store        *kv.KVService
	environments *environment.EnvironmentService
}

func New(coordinator *runs.Coordinator, store *kv.KVService, environments *environment.EnvironmentService) *Service {
	return &Service{
		runs:         coordinator,
		store:        store,
		environments: environments,
	}
}

// Deliver drives one manager-owned run through the remote delivery protocol.
func (s *Service) Deliver(ctx context.Context, run types.SchedulerRun) (types.SchedulerOutcome, error) {
	env, err := s.environments.GetEnvironmentByID(ctx, run.EnvironmentID)
	if err != nil {
		return types.SchedulerOutcome{Status: types.RunStatusFailed}, err
	}
	if !env.Enabled {
		return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Environment is disabled"}, nil
	}
	basePath := "/api/environments/0/jobs/" + url.PathEscape(run.JobID)
	runPath := basePath + "/runs/" + url.PathEscape(run.ID)
	// A previously confirmed terminal result needs only acknowledgement, never execution.
	if !run.RemoteRetryRequested && run.RemoteAccepted && kit.FromPtr(run.RemoteOutcome).Status.Terminal() {
		return s.acknowledgeRemote(ctx, run, runPath, *run.RemoteOutcome)
	}
	var catalog types.JobListResponse
	if proxyJSONRequestErr := s.environments.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodGet, "/api/environments/0/jobs", nil, &catalog); proxyJSONRequestErr != nil {
		return remoteFailure(proxyJSONRequestErr)
	}
	if !catalog.DurableRuns {
		return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Upgrade required: agent does not support durable job runs"}, nil
	}
	var remoteRun types.SchedulerRun
	if err = s.environments.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodGet, runPath, nil, &remoteRun); err != nil {
		var status *remenv.StatusError
		if !errors.As(err, &status) || status.StatusCode != http.StatusNotFound {
			return remoteFailure(err)
		}
		if run.RemoteAccepted {
			return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Agent no longer has the delivery receipt; verify the operation before retrying"}, nil
		}
		if !remoteJobEligible(catalog.Jobs, run.JobID) {
			return types.SchedulerOutcome{Status: types.RunStatusCanceled, Message: "Remote job is disabled or no longer eligible"}, nil
		}
		// A confirmed 404 permits another delivery of the same deduplicated ID.
		if updateRunErr := s.runs.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
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
				UpdatedAt:               current.UpdatedAt,
				StartedAt:               current.StartedAt,
				FinishedAt:              current.FinishedAt,
				NextAttempt:             current.NextAttempt,
				AttemptCount:            current.AttemptCount,
				Owner:                   current.Owner,
				Outcome:                 current.Outcome,
				Attempts:                current.Attempts,
				RemoteDeliveryAttempted: true,
				RemoteOutcome:           current.RemoteOutcome,
				RemoteRetryRequested:    current.RemoteRetryRequested,
				RemoteRetryAttempted:    current.RemoteRetryAttempted,
				RemoteAttemptCount:      current.RemoteAttemptCount,
				RemoteAccepted:          current.RemoteAccepted,
				RemoteSettled:           current.RemoteSettled,
				LastConfirmedAt:         current.LastConfirmedAt,
			}
			return nil
		}); updateRunErr != nil {
			return remoteFailure(updateRunErr)
		}
		body, marshalErr := json.Marshal(map[string]string{"runId": run.ID})
		if marshalErr != nil {
			return remoteFailure(marshalErr)
		}
		var accepted types.JobRunResponse
		if proxyJSONRequestErr := s.environments.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodPost, basePath+"?action=run", body, &accepted); proxyJSONRequestErr != nil {
			return remoteFailure(proxyJSONRequestErr)
		}
		if accepted.RunID != run.ID {
			return remoteFailure(errors.New("Agent returned an inconsistent delivery receipt")) //nolint:staticcheck // Preserve the existing error message.
		}
		remoteRun = types.SchedulerRun{
			ID:      run.ID,
			JobID:   run.JobID,
			Status:  accepted.Status,
			Outcome: types.SchedulerOutcome{Status: accepted.Status},
		}
	}
	if remoteRun.ID != run.ID || remoteRun.JobID != run.JobID {
		return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Agent returned an inconsistent run identity"}, nil
	}
	if run.RemoteRetryRequested && (remoteRun.Status.Terminal() || remoteRun.Status == types.RunStatusNeedsAttention) && remoteRun.AttemptCount <= run.RemoteAttemptCount {
		if run.RemoteRetryAttempted {
			return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Remote retry acknowledgement was lost; inspect the agent before retrying again"}, nil
		}
		if updateRunErr := s.runs.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
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
				UpdatedAt:               current.UpdatedAt,
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
				RemoteRetryAttempted:    true,
				RemoteAttemptCount:      current.RemoteAttemptCount,
				RemoteAccepted:          current.RemoteAccepted,
				RemoteSettled:           current.RemoteSettled,
				LastConfirmedAt:         current.LastConfirmedAt,
			}
			return nil
		}); updateRunErr != nil {
			return remoteFailure(updateRunErr)
		}
		var retried types.SchedulerRun
		if proxyJSONRequestErr := s.environments.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodPost, runPath+"?action=retry", nil, &retried); proxyJSONRequestErr != nil {
			return remoteFailure(proxyJSONRequestErr)
		}
		if retried.ID != run.ID || retried.JobID != run.JobID {
			return remoteFailure(errors.New("Agent returned an inconsistent retry receipt")) //nolint:staticcheck // Preserve the existing error message.
		}
		remoteRun = retried
	}
	return s.Confirm(ctx, run, remoteRun, runPath)
}

// Confirm records the agent-reported state of a delivered run.
func (s *Service) Confirm(ctx context.Context, run, remoteRun types.SchedulerRun, runPath string) (types.SchedulerOutcome, error) {
	outcome := types.SchedulerOutcome{
		Status:     remoteRun.Status,
		Message:    remoteRun.Outcome.Message,
		ActivityID: remoteRun.Outcome.ActivityID,
		Targets:    remoteRun.Outcome.Targets,
		Steps:      remoteRun.Outcome.Steps,
	}
	if err := s.runs.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
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
			UpdatedAt:               current.UpdatedAt,
			StartedAt:               remoteRun.StartedAt,
			FinishedAt:              remoteRun.FinishedAt,
			NextAttempt:             current.NextAttempt,
			AttemptCount:            current.AttemptCount,
			Owner:                   current.Owner,
			Outcome:                 outcome,
			Attempts:                current.Attempts,
			RemoteDeliveryAttempted: true,
			RemoteOutcome:           new(outcome),
			RemoteRetryRequested:    false,
			RemoteRetryAttempted:    current.RemoteRetryAttempted,
			RemoteAttemptCount:      remoteRun.AttemptCount,
			RemoteAccepted:          true,
			RemoteSettled:           remoteRun.RemoteSettled,
			LastConfirmedAt:         new(time.Now().UTC()),
		}
		return nil
	}); err != nil {
		return types.SchedulerOutcome{Status: types.RunStatusRetrying}, err
	}
	if outcome.Status.Terminal() {
		if remoteRun.RemoteSettled {
			return outcome, nil
		}
		return s.acknowledgeRemote(ctx, run, runPath, outcome)
	}
	return types.SchedulerOutcome{
		Status:     types.RunStatusWaiting,
		Message:    "Waiting for the agent to complete this run",
		Targets:    outcome.Targets,
		ActivityID: outcome.ActivityID,
	}, nil
}

func (s *Service) acknowledgeRemote(ctx context.Context, run types.SchedulerRun, path string, outcome types.SchedulerOutcome) (types.SchedulerOutcome, error) {
	var response types.SchedulerRun
	if err := s.environments.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodPost, path+"?action=ack", nil, &response); err != nil {
		return types.SchedulerOutcome{
			Status:     types.RunStatusWaiting,
			Message:    "Remote operation completed; waiting for delivery acknowledgement",
			Targets:    outcome.Targets,
			ActivityID: outcome.ActivityID,
		}, err
	}
	if response.ID != run.ID || response.JobID != run.JobID || response.EnvironmentID != "0" || response.Status != outcome.Status || !response.RemoteSettled {
		return types.SchedulerOutcome{Status: types.RunStatusWaiting, Message: "Remote operation completed; acknowledgement not confirmed"}, nil
	}
	if err := s.runs.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
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
			UpdatedAt:               current.UpdatedAt,
			StartedAt:               response.StartedAt,
			FinishedAt:              response.FinishedAt,
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
		return nil
	}); err != nil {
		return types.SchedulerOutcome{Status: types.RunStatusRetrying}, err
	}
	return outcome, nil
}

// Catalog retains the last observed agent catalog while its environment is offline.
func (s *Service) Catalog(ctx context.Context, environmentID string) (*types.JobListResponse, error) {
	if _, err := s.environments.GetEnvironmentByID(ctx, environmentID); err != nil {
		return nil, err
	}
	key := "jobs-catalog/" + kit.SHA256Hex(environmentID)
	var catalog types.JobListResponse
	remoteErr := s.environments.ProxyJSONRequest(ctx, environmentID, http.MethodGet, "/api/environments/0/jobs", nil, &catalog)
	if remoteErr == nil {
		observed := types.JobListResponse{ //exhaustruct:enforce
			DurableRuns: catalog.DurableRuns,
			ObservedAt:  time.Now().UTC(),
			Offline:     false,
			Jobs:        catalog.Jobs,
			IsAgent:     catalog.IsAgent,
		}
		raw, err := json.Marshal(observed)
		if err != nil {
			return nil, err
		}
		if setErr := s.store.Set(ctx, key, string(raw)); setErr != nil {
			return nil, setErr
		}
		return &observed, nil
	}
	var status *remenv.StatusError
	if errors.As(remoteErr, &status) && (status.StatusCode == http.StatusUnauthorized || status.StatusCode == http.StatusForbidden) {
		return nil, remoteErr
	}
	raw, found, err := s.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if found {
		if unmarshalErr := json.Unmarshal([]byte(raw), &catalog); unmarshalErr != nil {
			return nil, unmarshalErr
		}
	}
	jobs := catalog.Jobs
	for _, metadata := range types.GetAllJobMetadata() {
		if !found && !metadata.ManagerOnly {
			jobs = append(jobs, metadata.ToJobStatus("", nil, metadata.CanRunManually, []types.JobPrerequisite{}))
		}
	}
	return &types.JobListResponse{ //exhaustruct:enforce
		DurableRuns: catalog.DurableRuns,
		ObservedAt:  catalog.ObservedAt,
		Offline:     true,
		Jobs:        jobs,
		IsAgent:     catalog.IsAgent || !found,
	}, nil
}

// RetryRun queues one explicit retry of an authorized manager-owned run while
// retaining the agent's target progress.
func (s *Service) RetryRun(ctx context.Context, run types.SchedulerRun) (types.SchedulerRun, error) {
	err := s.runs.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
		if current.Status != types.RunStatusFailed && current.Status != types.RunStatusPartial && current.Status != types.RunStatusNeedsAttention {
			return errors.New("run is not eligible for retry")
		}
		if !current.RemoteAccepted && current.RemoteDeliveryAttempted {
			return errors.New("remote delivery is uncertain; inspect the agent before submitting new work")
		}
		remoteStatus := kit.FromPtr(current.RemoteOutcome).Status
		if current.RemoteAccepted && !remoteStatus.Terminal() && remoteStatus != types.RunStatusNeedsAttention {
			return errors.New("remote outcome must be confirmed before retry")
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
			Status:                  types.RunStatusQueued,
			CreatedAt:               current.CreatedAt,
			UpdatedAt:               time.Now().UTC(),
			StartedAt:               current.StartedAt,
			FinishedAt:              nil,
			NextAttempt:             nil,
			AttemptCount:            current.AttemptCount,
			Owner:                   "",
			Outcome:                 current.Outcome,
			Attempts:                current.Attempts,
			RemoteDeliveryAttempted: current.RemoteDeliveryAttempted,
			RemoteOutcome:           current.RemoteOutcome,
			RemoteRetryRequested:    current.RemoteAccepted || current.RemoteRetryRequested,
			RemoteRetryAttempted:    !current.RemoteAccepted && current.RemoteRetryAttempted,
			RemoteAttemptCount:      current.RemoteAttemptCount,
			RemoteAccepted:          current.RemoteAccepted,
			RemoteSettled:           !current.RemoteAccepted && current.RemoteSettled,
			LastConfirmedAt:         current.LastConfirmedAt,
		}
		run = *current
		return nil
	})
	return run, err
}

// Runs merges agent history with requests accepted by this manager.
func (s *Service) Runs(ctx context.Context, environmentID, jobID string) ([]types.SchedulerRun, error) {
	if _, err := s.environments.GetEnvironmentByID(ctx, environmentID); err != nil {
		return nil, err
	}
	records, err := s.runs.Records(ctx)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]types.SchedulerRun)
	for page := 1; ; page++ {
		path := "/api/environments/0/jobs/" + url.PathEscape(jobID) + "/runs?page=" + strconv.Itoa(page) + "&limit=100"
		var response types.SchedulerRunList
		if proxyJSONRequestErr := s.environments.ProxyJSONRequest(ctx, environmentID, http.MethodGet, path, nil, &response); proxyJSONRequestErr != nil {
			var status *remenv.StatusError
			if errors.As(proxyJSONRequestErr, &status) && (status.StatusCode == http.StatusForbidden || status.StatusCode == http.StatusUnauthorized) {
				return nil, proxyJSONRequestErr
			}
			clear(merged)
			break
		}
		previous := len(merged)
		for _, run := range response.Runs {
			merged[run.ID] = types.SchedulerRun{ //exhaustruct:enforce
				ActivityEnvironmentID:   kit.Ternary(run.ActivityID != "", environmentID, run.ActivityEnvironmentID),
				ActivityID:              run.ActivityID,
				RequestedWithKey:        run.RequestedWithKey,
				ID:                      run.ID,
				JobID:                   run.JobID,
				EnvironmentID:           environmentID,
				Trigger:                 run.Trigger,
				RequestedBy:             run.RequestedBy,
				Status:                  run.Status,
				CreatedAt:               run.CreatedAt,
				UpdatedAt:               run.UpdatedAt,
				StartedAt:               run.StartedAt,
				FinishedAt:              run.FinishedAt,
				NextAttempt:             run.NextAttempt,
				AttemptCount:            run.AttemptCount,
				Owner:                   run.Owner,
				Outcome:                 run.Outcome,
				Attempts:                run.Attempts,
				RemoteDeliveryAttempted: run.RemoteDeliveryAttempted,
				RemoteOutcome:           run.RemoteOutcome,
				RemoteRetryRequested:    run.RemoteRetryRequested,
				RemoteRetryAttempted:    run.RemoteRetryAttempted,
				RemoteAttemptCount:      run.RemoteAttemptCount,
				RemoteAccepted:          run.RemoteAccepted,
				RemoteSettled:           run.RemoteSettled,
				LastConfirmedAt:         run.LastConfirmedAt,
			}
		}
		if len(merged) >= response.Total || len(merged) == previous {
			break
		}
	}
	for _, record := range records {
		if record.EnvironmentID != environmentID || record.JobID != jobID {
			continue
		}
		for _, run := range record.Runs {
			merged[run.ID] = run
		}
	}
	return slices.Collect(maps.Values(merged)), nil
}

// AgentRun queries agent-owned history for a run the manager never accepted.
func (s *Service) AgentRun(ctx context.Context, environmentID, jobID, runID string) (types.SchedulerRun, error) {
	var run types.SchedulerRun
	path := "/api/environments/0/jobs/" + url.PathEscape(jobID) + "/runs/" + url.PathEscape(runID)
	if proxyJSONRequestErr := s.environments.ProxyJSONRequest(ctx, environmentID, http.MethodGet, path, nil, &run); proxyJSONRequestErr != nil {
		return types.SchedulerRun{}, proxyJSONRequestErr
	}
	if run.ID != runID || run.JobID != jobID {
		return types.SchedulerRun{}, errors.New("agent returned an inconsistent run identity")
	}
	return types.SchedulerRun{ //exhaustruct:enforce
		ActivityEnvironmentID:   kit.Ternary(run.ActivityID != "", environmentID, run.ActivityEnvironmentID),
		ActivityID:              run.ActivityID,
		RequestedWithKey:        run.RequestedWithKey,
		ID:                      run.ID,
		JobID:                   run.JobID,
		EnvironmentID:           environmentID,
		Trigger:                 run.Trigger,
		RequestedBy:             run.RequestedBy,
		Status:                  run.Status,
		CreatedAt:               run.CreatedAt,
		UpdatedAt:               run.UpdatedAt,
		StartedAt:               run.StartedAt,
		FinishedAt:              run.FinishedAt,
		NextAttempt:             run.NextAttempt,
		AttemptCount:            run.AttemptCount,
		Owner:                   run.Owner,
		Outcome:                 run.Outcome,
		Attempts:                run.Attempts,
		RemoteDeliveryAttempted: run.RemoteDeliveryAttempted,
		RemoteOutcome:           run.RemoteOutcome,
		RemoteRetryRequested:    run.RemoteRetryRequested,
		RemoteRetryAttempted:    run.RemoteRetryAttempted,
		RemoteAttemptCount:      run.RemoteAttemptCount,
		RemoteAccepted:          run.RemoteAccepted,
		RemoteSettled:           run.RemoteSettled,
		LastConfirmedAt:         run.LastConfirmedAt,
	}, nil
}

// MutateAgentRun forwards an explicit operator action to an agent-owned run.
func (s *Service) MutateAgentRun(ctx context.Context, environmentID, jobID, runID, action string) (types.SchedulerRun, error) {
	targetEnvironment, err := s.environments.GetEnvironmentByID(ctx, environmentID)
	if err != nil {
		return types.SchedulerRun{}, err
	}
	if !targetEnvironment.Enabled {
		return types.SchedulerRun{}, errors.New("environment is disabled")
	}
	path := "/api/environments/0/jobs/" + url.PathEscape(jobID) + "/runs/" + url.PathEscape(runID) + "?action=" + action
	var run types.SchedulerRun
	// This is an explicit operator action. Never replay a lost mutation response.
	if proxyJSONRequestErr := s.environments.ProxyJSONRequest(ctx, environmentID, http.MethodPost, path, nil, &run); proxyJSONRequestErr != nil {
		return types.SchedulerRun{}, proxyJSONRequestErr
	}
	if run.ID != runID || run.JobID != jobID {
		return types.SchedulerRun{}, errors.New("agent returned an inconsistent run identity")
	}
	return types.SchedulerRun{ //exhaustruct:enforce
		ActivityEnvironmentID:   kit.Ternary(run.ActivityID != "", environmentID, run.ActivityEnvironmentID),
		ActivityID:              run.ActivityID,
		RequestedWithKey:        run.RequestedWithKey,
		ID:                      run.ID,
		JobID:                   run.JobID,
		EnvironmentID:           environmentID,
		Trigger:                 run.Trigger,
		RequestedBy:             run.RequestedBy,
		Status:                  run.Status,
		CreatedAt:               run.CreatedAt,
		UpdatedAt:               run.UpdatedAt,
		StartedAt:               run.StartedAt,
		FinishedAt:              run.FinishedAt,
		NextAttempt:             run.NextAttempt,
		AttemptCount:            run.AttemptCount,
		Owner:                   run.Owner,
		Outcome:                 run.Outcome,
		Attempts:                run.Attempts,
		RemoteDeliveryAttempted: run.RemoteDeliveryAttempted,
		RemoteOutcome:           run.RemoteOutcome,
		RemoteRetryRequested:    run.RemoteRetryRequested,
		RemoteRetryAttempted:    run.RemoteRetryAttempted,
		RemoteAttemptCount:      run.RemoteAttemptCount,
		RemoteAccepted:          run.RemoteAccepted,
		RemoteSettled:           run.RemoteSettled,
		LastConfirmedAt:         run.LastConfirmedAt,
	}, nil
}

func remoteJobEligible(jobs []types.JobStatus, id string) bool {
	for _, job := range jobs {
		if job.ID != id {
			if remoteJobEligible(job.Children, id) {
				return true
			}
			continue
		}
		return job.Enabled && job.CanRunManually && !job.ManagerOnly &&
			!slices.ContainsFunc(job.Prerequisites, func(prerequisite types.JobPrerequisite) bool { return !prerequisite.IsMet })
	}
	return false
}

func remoteFailure(err error) (types.SchedulerOutcome, error) {
	var status *remenv.StatusError
	rejected := errors.As(err, &status) && status.StatusCode >= 400 && status.StatusCode < 500 &&
		status.StatusCode != http.StatusRequestTimeout && status.StatusCode != http.StatusTooManyRequests
	if rejected {
		return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Remote agent rejected the request; check configuration and permissions"}, err
	}
	return types.SchedulerOutcome{Status: types.RunStatusWaiting, Message: "Waiting for environment"}, err
}
