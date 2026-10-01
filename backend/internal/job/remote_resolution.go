package job

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

// TODO(v3): remove adoption of legacy agent-owned runs.
func (s *JobService) reconcileLegacyRemoteCatalogInternal(ctx context.Context, environmentID string, jobs []jobschedule.JobStatus) error {
	for _, job := range jobs {
		for _, remote := range []*st.Run{job.CurrentRun, job.LastRun} {
			if remote == nil || remote.Status != st.NeedsAttention {
				continue
			}
			if remote.JobID != job.ID || remote.EnvironmentID != "0" {
				return errors.New("agent returned an inconsistent run identity")
			}
			_, err := s.runs.Get(ctx, environmentID, job.ID, remote.ID)
			if err == nil {
				continue
			}
			if !errors.Is(err, runs.ErrRunNotFound) {
				return err
			}
			_, err = s.runs.Submit(ctx, st.Request{RunID: remote.ID, JobID: job.ID, EnvironmentID: environmentID, Trigger: "recovery", ObservedAgentRun: remote})
			if err != nil {
				return err
			}
		}
		if err := s.reconcileLegacyRemoteCatalogInternal(ctx, environmentID, job.Children); err != nil {
			return err
		}
	}
	return nil
}

func (s *JobService) resolveRemoteRunInternal(ctx context.Context, environmentID, jobID, runID, actor string) (st.Run, error) {
	local, localErr := s.runs.Get(ctx, environmentID, jobID, runID)
	if localErr != nil && !errors.Is(localErr, runs.ErrRunNotFound) {
		return st.Run{}, localErr
	}
	if localErr == nil {
		if local.Status == st.Canceled && local.Resolution != nil {
			return local, nil
		}
		if local.Status != st.NeedsAttention {
			return local, errors.New("only runs needing attention can be resolved")
		}
		if !local.RemoteDeliveryAttempted && !local.RemoteAccepted {
			return s.runs.Resolve(ctx, environmentID, jobID, runID, actor)
		}
	}
	acknowledged, err := s.resolveAgentReviewInternal(ctx, environmentID, jobID, runID, actor)
	if err != nil {
		return st.Run{}, err
	}
	if localErr != nil {
		acknowledged.EnvironmentID = environmentID
		if acknowledged.ActivityID != "" {
			acknowledged.ActivityEnvironmentID = environmentID
		}
		return acknowledged, nil
	}
	now := time.Now().UTC()
	if err := s.runs.UpdateRun(ctx, local, func(current *st.Run) error {
		if current.Status != st.NeedsAttention {
			return runs.ErrRunConflict
		}
		outcome := acknowledged.Outcome
		outcome.Status = acknowledged.Status
		current.RemoteOutcome = &outcome
		current.RemoteAccepted = true
		current.RemoteSettled = true
		current.LastConfirmedAt = &now
		return nil
	}); err != nil {
		return st.Run{}, err
	}
	return s.runs.Resolve(ctx, environmentID, jobID, runID, acknowledged.Resolution.ResolvedBy)
}

// resolveAgentReviewInternal settles legacy inactive runs on their owning agent.
// TODO(v3): remove the legacy resolution protocol.
func (s *JobService) resolveAgentReviewInternal(ctx context.Context, environmentID, jobID, runID, actor string) (st.Run, error) {
	env, err := s.environment.GetEnvironmentByID(ctx, environmentID)
	if err != nil {
		return st.Run{}, err
	}
	if !env.Enabled {
		return st.Run{}, errors.New("environment is disabled")
	}
	path := "/api/environments/0/jobs/" + url.PathEscape(jobID) + "/runs/" + url.PathEscape(runID)
	var remote st.Run
	// Read the owner on every request, including retries after a lost response.
	if err := s.environment.ProxyJSONRequest(ctx, environmentID, http.MethodGet, path, nil, &remote); err != nil {
		return st.Run{}, err
	}
	if remote.ID != runID || remote.JobID != jobID || remote.EnvironmentID != "0" {
		return st.Run{}, errors.New("agent returned an inconsistent run identity")
	}
	if remote.Status == st.NeedsAttention {
		body, err := json.Marshal(map[string]string{"resolvedBy": actor})
		if err != nil {
			return st.Run{}, err
		}
		if err := s.environment.ProxyJSONRequest(ctx, environmentID, http.MethodPost, path+"/resolve", body, &remote); err != nil {
			return st.Run{}, err
		}
	} else if actor != common.SystemUser.Username {
		if remote.Status != st.Canceled || remote.Resolution == nil {
			return st.Run{}, errors.New("agent run must need attention before review resolution")
		}
	}
	if remote.ID != runID || remote.JobID != jobID || remote.EnvironmentID != "0" || !remote.Status.Terminal() ||
		(actor != common.SystemUser.Username && (remote.Status != st.Canceled || remote.Resolution == nil)) {
		return st.Run{}, errors.New("agent resolution was not confirmed")
	}
	var acknowledged st.Run
	if err := s.environment.ProxyJSONRequest(ctx, environmentID, http.MethodPost, path+"/ack", nil, &acknowledged); err != nil {
		return st.Run{}, err
	}
	if acknowledged.ID != runID || acknowledged.JobID != jobID || acknowledged.EnvironmentID != "0" || acknowledged.Status != remote.Status || !acknowledged.RemoteSettled ||
		(remote.Resolution != nil && (acknowledged.Resolution == nil || acknowledged.Resolution.ResolvedBy != remote.Resolution.ResolvedBy)) {
		return st.Run{}, errors.New("agent resolution acknowledgement was not confirmed")
	}
	return acknowledged, nil
}
