package job

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/queue"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

func (s *JobService) resolveRemoteRunInternal(ctx context.Context, environmentID, jobID, runID, actor string) (st.Run, error) {
	local, localErr := s.Queue.Get(ctx, environmentID, jobID, runID)
	if localErr != nil && !errors.Is(localErr, queue.ErrRunNotFound) {
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
			return s.Queue.Resolve(ctx, environmentID, jobID, runID, actor)
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
	if err := s.Queue.UpdateRun(ctx, local, func(current *st.Run) error {
		if current.Status != st.NeedsAttention {
			return queue.ErrRunConflict
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
	return s.Queue.Resolve(ctx, environmentID, jobID, runID, acknowledged.Resolution.ResolvedBy)
}

// resolveAgentReviewInternal confirms ownership, records review, and settles delivery.
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
	if remote.Status != st.Canceled || remote.Resolution == nil {
		if remote.Status != st.NeedsAttention {
			return st.Run{}, errors.New("agent run must need attention before review resolution")
		}
		body, err := json.Marshal(map[string]string{"resolvedBy": actor})
		if err != nil {
			return st.Run{}, err
		}
		if err := s.environment.ProxyJSONRequest(ctx, environmentID, http.MethodPost, path+"/resolve", body, &remote); err != nil {
			return st.Run{}, err
		}
	}
	if remote.ID != runID || remote.JobID != jobID || remote.EnvironmentID != "0" || remote.Status != st.Canceled || remote.Resolution == nil {
		return st.Run{}, errors.New("agent resolution was not confirmed")
	}
	var acknowledged st.Run
	if err := s.environment.ProxyJSONRequest(ctx, environmentID, http.MethodPost, path+"/ack", nil, &acknowledged); err != nil {
		return st.Run{}, err
	}
	if acknowledged.ID != runID || acknowledged.JobID != jobID || acknowledged.EnvironmentID != "0" || acknowledged.Status != st.Canceled || acknowledged.Resolution == nil || !acknowledged.RemoteSettled {
		return st.Run{}, errors.New("agent resolution acknowledgement was not confirmed")
	}
	return acknowledged, nil
}
