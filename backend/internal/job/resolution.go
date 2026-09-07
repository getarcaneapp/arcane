package job

import (
	"context"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

// ResolveRun records operator review and releases the schedule's blocked run.
func (h *JobSchedulesHandler) ResolveRun(ctx context.Context, input *jobschedule.ResolveRunInput) (*jobschedule.RunOutput, error) {
	actor, _ := middleware.GetUserIDFromContext(ctx)
	permissions, _ := middleware.PermissionsFromContext(ctx)
	if input.Body.ResolvedBy != "" {
		if permissions == nil || !permissions.Sudo || !h.jobService.cfg.AgentMode {
			return nil, huma.Error403Forbidden("only authenticated manager transport may forward an operator identity")
		}
		actor = input.Body.ResolvedBy
	}
	run, err := h.jobService.ResolveRun(ctx, input.ID, input.JobID, input.RunID, actor)
	if err != nil {
		return nil, jobHTTPErrorInternal(err)
	}
	return &jobschedule.RunOutput{Body: run}, nil
}

// ResolveRun authorizes the current operator independently of the original requester.
func (s *JobService) ResolveRun(ctx context.Context, environmentID, jobID, runID, actor string) (st.Run, error) {
	permissions, _ := middleware.PermissionsFromContext(ctx)
	if !permissions.Allows(authz.PermJobsManage, environmentID) {
		return st.Run{}, huma.Error403Forbidden("permission denied: " + authz.PermJobsManage)
	}
	if actor == "" {
		return st.Run{}, errors.New("resolving operator identity is required")
	}
	if environmentID != "0" {
		return s.resolveRemoteRunInternal(ctx, environmentID, jobID, runID, actor)
	}
	return s.Queue.Resolve(ctx, environmentID, jobID, runID, actor)
}
