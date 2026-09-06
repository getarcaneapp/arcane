package job

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"sort"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/queue"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	"github.com/getarcaneapp/arcane/types/v2/meta"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

func remoteFailureInternal(err error) (st.Outcome, error) {
	var status *remenv.StatusError
	if errors.As(err, &status) && status.StatusCode >= 400 && status.StatusCode < 500 && status.StatusCode != http.StatusRequestTimeout && status.StatusCode != http.StatusTooManyRequests {
		return st.Outcome{Status: st.NeedsAttention, Message: "Remote agent rejected the request; check configuration and permissions"}, err
	}
	return st.Outcome{Status: st.Waiting, Message: "Waiting for environment"}, err
}

func (s *JobService) deliverRemoteInternal(ctx context.Context, run st.Run) (st.Outcome, error) {
	env, err := s.environment.GetEnvironmentByID(ctx, run.EnvironmentID)
	if err != nil {
		return st.Outcome{Status: st.NeedsAttention}, err
	}
	if !env.Enabled {
		return st.Outcome{Status: st.NeedsAttention, Message: "Environment is disabled"}, nil
	}
	basePath := "/api/environments/0/jobs/" + url.PathEscape(run.JobID)
	runPath := basePath + "/runs/" + url.PathEscape(run.ID)
	// A previously confirmed terminal result needs only acknowledgement, never execution.
	if !run.RemoteRetryRequested && run.RemoteAccepted && run.RemoteOutcome != nil && run.RemoteOutcome.Status.Terminal() {
		return s.acknowledgeRemoteInternal(ctx, run, runPath, *run.RemoteOutcome)
	}
	var catalog jobschedule.JobListResponse
	if err := s.environment.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodGet, "/api/environments/0/jobs", nil, &catalog); err != nil {
		return remoteFailureInternal(err)
	}
	if !catalog.DurableRuns {
		return st.Outcome{Status: st.NeedsAttention, Message: "Upgrade required: agent does not support durable job runs"}, nil
	}
	var remoteRun st.Run
	err = s.environment.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodGet, runPath, nil, &remoteRun)
	if err != nil {
		var status *remenv.StatusError
		if !errors.As(err, &status) || status.StatusCode != http.StatusNotFound {
			return remoteFailureInternal(err)
		}
		remoteRun, err = s.admitRemoteInternal(ctx, run, catalog)
		if err != nil {
			if errors.Is(err, errRemoteIneligibleInternal) {
				return st.Outcome{Status: st.Canceled, Message: err.Error()}, nil
			}
			if errors.Is(err, errRemoteReceiptMissingInternal) {
				return st.Outcome{Status: st.NeedsAttention, Message: err.Error()}, nil
			}
			return remoteFailureInternal(err)
		}
	}
	if remoteRun.ID != run.ID || remoteRun.JobID != run.JobID {
		return st.Outcome{Status: st.NeedsAttention, Message: "Agent returned an inconsistent run identity"}, nil
	}
	if run.RemoteRetryRequested && (remoteRun.Status.Terminal() || remoteRun.Status == st.NeedsAttention) && remoteRun.AttemptCount <= run.RemoteAttemptCount {
		remoteRun, err = s.retryDeliveryInternal(ctx, run, runPath)
		if err != nil {
			if errors.Is(err, errRemoteRetryUncertainInternal) {
				return st.Outcome{Status: st.NeedsAttention, Message: err.Error()}, nil
			}
			return remoteFailureInternal(err)
		}
	}
	return s.confirmRemoteRunInternal(ctx, run, remoteRun, runPath)
}

func (s *JobService) confirmRemoteRunInternal(ctx context.Context, run, remoteRun st.Run, runPath string) (st.Outcome, error) {
	now := time.Now().UTC()
	outcome := remoteRun.Outcome
	outcome.Status = remoteRun.Status
	if err := s.Queue.UpdateRun(ctx, run, func(current *st.Run) error {
		current.RemoteAccepted = true
		current.RemoteDeliveryAttempted = true
		current.LastConfirmedAt = &now
		current.RemoteOutcome = &outcome
		current.RemoteAttemptCount = remoteRun.AttemptCount
		current.RemoteRetryRequested = false
		current.Outcome = outcome
		return nil
	}); err != nil {
		return st.Outcome{Status: st.Retrying}, err
	}
	if outcome.Status.Terminal() {
		return s.acknowledgeRemoteInternal(ctx, run, runPath, outcome)
	}
	if outcome.Status == st.NeedsAttention {
		return outcome, nil
	}
	return st.Outcome{Status: st.Waiting, Message: "Waiting for the agent to complete this run", Targets: outcome.Targets, ActivityID: outcome.ActivityID}, nil
}

func (s *JobService) acknowledgeRemoteInternal(ctx context.Context, run st.Run, path string, outcome st.Outcome) (st.Outcome, error) {
	var response st.Run
	if err := s.environment.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodPost, path+"/ack", nil, &response); err != nil {
		return st.Outcome{Status: st.Waiting, Message: "Remote operation completed; waiting for delivery acknowledgement", Targets: outcome.Targets, ActivityID: outcome.ActivityID}, err
	}
	if response.ID != run.ID || !response.RemoteSettled {
		return st.Outcome{Status: st.Waiting, Message: "Remote operation completed; acknowledgement not confirmed"}, nil
	}
	if err := s.Queue.UpdateRun(ctx, run, func(current *st.Run) error { current.RemoteSettled = true; return nil }); err != nil {
		return st.Outcome{Status: st.Retrying}, err
	}
	return outcome, nil
}

// ListRemoteJobs retains the last observed agent catalog while its environment is offline.
func (s *JobService) ListRemoteJobs(ctx context.Context, environmentID string) (*jobschedule.JobListResponse, error) {
	if _, err := s.environment.GetEnvironmentByID(ctx, environmentID); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(environmentID))
	key := "jobs-catalog/" + hex.EncodeToString(digest[:])
	var catalog jobschedule.JobListResponse
	remoteErr := s.environment.ProxyJSONRequest(ctx, environmentID, http.MethodGet, "/api/environments/0/jobs", nil, &catalog)
	if remoteErr == nil {
		catalog.ObservedAt = time.Now().UTC()
		catalog.Offline = false
		raw, err := json.Marshal(catalog)
		if err != nil {
			return nil, err
		}
		if err := s.store.Set(ctx, key, string(raw)); err != nil {
			return nil, err
		}
	} else {
		var status *remenv.StatusError
		if errors.As(remoteErr, &status) && (status.StatusCode == http.StatusUnauthorized || status.StatusCode == http.StatusForbidden) {
			return nil, remoteErr
		}
		raw, found, err := s.store.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if found {
			if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
				return nil, err
			}
		} else {
			catalog.IsAgent = true
			for _, metadata := range meta.GetAllJobMetadata() {
				if !metadata.ManagerOnly {
					catalog.Jobs = append(catalog.Jobs, metadata.ToJobStatus("", nil, metadata.CanRunManually, []jobschedule.JobPrerequisite{}))
				}
			}
		}
		catalog.Offline = true
	}
	if err := s.addRuntimeStatusInternal(ctx, environmentID, &catalog.Jobs); err != nil {
		return nil, err
	}
	sort.Slice(catalog.Jobs, func(i, j int) bool { return catalog.Jobs[i].ID < catalog.Jobs[j].ID })
	return &catalog, nil
}

// RetryRemoteRun queues one explicit retry while retaining the agent's target progress.
func (s *JobService) RetryRemoteRun(ctx context.Context, environmentID, jobID, runID string) (st.Run, error) {
	run, err := s.Queue.Get(ctx, environmentID, jobID, runID)
	if errors.Is(err, queue.ErrRunNotFound) {
		return s.mutateAgentRunInternal(ctx, environmentID, jobID, runID, "retry")
	}
	if err != nil {
		return run, err
	}
	if err := s.authorizeRunInternal(ctx, run); err != nil {
		return run, err
	}
	err = s.Queue.UpdateRun(ctx, run, func(current *st.Run) error {
		if current.Status != st.Failed && current.Status != st.Partial && current.Status != st.NeedsAttention {
			return errors.New("run is not eligible for retry")
		}
		if !current.RemoteAccepted {
			if current.RemoteDeliveryAttempted {
				return errors.New("remote delivery is uncertain; inspect the agent before submitting new work")
			}
		} else {
			if current.RemoteOutcome == nil || (!current.RemoteOutcome.Status.Terminal() && current.RemoteOutcome.Status != st.NeedsAttention) {
				return errors.New("remote outcome must be confirmed before retry")
			}
			current.RemoteRetryRequested = true
			current.RemoteRetryAttempted = false
			current.RemoteSettled = false
		}
		current.Status = st.Queued
		current.Owner = ""
		current.NextAttempt = nil
		current.FinishedAt = nil
		current.UpdatedAt = time.Now().UTC()
		run = *current
		return nil
	})
	return run, err
}

var (
	errRemoteIneligibleInternal     = errors.Sentinel("Remote job is disabled or no longer eligible")
	errRemoteReceiptMissingInternal = errors.Sentinel("Agent no longer has the delivery receipt; verify the operation before retrying")
	errRemoteRetryUncertainInternal = errors.Sentinel("Remote retry acknowledgement was lost; inspect the agent before retrying again")
)

func remoteJobEligibleInternal(jobs []jobschedule.JobStatus, id string) bool {
	for _, job := range jobs {
		if job.ID != id {
			if remoteJobEligibleInternal(job.Children, id) {
				return true
			}
			continue
		}
		if !job.Enabled || !job.CanRunManually || job.ManagerOnly {
			return false
		}
		for _, prerequisite := range job.Prerequisites {
			if !prerequisite.IsMet {
				return false
			}
		}
		return true
	}
	return false
}

func (s *JobService) admitRemoteInternal(ctx context.Context, run st.Run, catalog jobschedule.JobListResponse) (st.Run, error) {
	if run.RemoteAccepted {
		return st.Run{}, errRemoteReceiptMissingInternal
	}
	if !remoteJobEligibleInternal(catalog.Jobs, run.JobID) {
		return st.Run{}, errRemoteIneligibleInternal
	}
	// A confirmed 404 permits another delivery of the same deduplicated ID.
	if err := s.Queue.UpdateRun(ctx, run, func(current *st.Run) error {
		current.RemoteDeliveryAttempted = true
		return nil
	}); err != nil {
		return st.Run{}, err
	}
	body, err := json.Marshal(map[string]string{"runId": run.ID})
	if err != nil {
		return st.Run{}, err
	}
	path := "/api/environments/0/jobs/" + url.PathEscape(run.JobID) + "/run"
	var accepted jobschedule.JobRunResponse
	if err := s.environment.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodPost, path, body, &accepted); err != nil {
		return st.Run{}, err
	}
	if accepted.RunID != run.ID {
		return st.Run{}, errors.New("Agent returned an inconsistent delivery receipt")
	}
	return st.Run{ID: run.ID, JobID: run.JobID, Status: accepted.Status, Outcome: st.Outcome{Status: accepted.Status}}, nil
}

func (s *JobService) retryDeliveryInternal(ctx context.Context, run st.Run, path string) (st.Run, error) {
	if run.RemoteRetryAttempted {
		return st.Run{}, errRemoteRetryUncertainInternal
	}
	if err := s.Queue.UpdateRun(ctx, run, func(current *st.Run) error {
		current.RemoteRetryAttempted = true
		return nil
	}); err != nil {
		return st.Run{}, err
	}
	var retried st.Run
	if err := s.environment.ProxyJSONRequest(ctx, run.EnvironmentID, http.MethodPost, path+"/retry", nil, &retried); err != nil {
		return st.Run{}, err
	}
	if retried.ID != run.ID || retried.JobID != run.JobID {
		return st.Run{}, errors.New("Agent returned an inconsistent retry receipt")
	}
	return retried, nil
}
