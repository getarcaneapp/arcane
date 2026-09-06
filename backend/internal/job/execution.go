package job

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/queue"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/meta"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

// Submit validates job eligibility and persists acceptance before execution.
// Managers may accept requests for offline environments. Reusing a run ID
// deduplicates admission within the same job and environment.
func (s *JobService) Submit(ctx context.Context, request st.Request) (st.Run, error) {
	if request.EnvironmentID == "" {
		request.EnvironmentID = "0"
	}
	if request.EnvironmentID != "0" {
		if s.cfg.AgentMode {
			return st.Run{}, errors.New("agents cannot queue work for another environment")
		}
		env, err := s.environment.GetEnvironmentByID(ctx, request.EnvironmentID)
		if err != nil {
			return st.Run{}, err
		}
		if !env.Enabled {
			return st.Run{}, errors.New("environment is disabled")
		}
		metadata, ok := meta.GetJobMetadata(request.JobID)
		dynamicRemote := strings.HasPrefix(request.JobID, "gitops-sync:") || strings.HasPrefix(request.JobID, "volume-backup:")
		if !dynamicRemote && (!ok || metadata.ManagerOnly || !metadata.CanRunManually) {
			return st.Run{}, errors.New("job is not remotely runnable")
		}
	} else if err := s.validateLocalJobInternal(ctx, request.JobID); err != nil {
		return st.Run{}, err
	}
	return s.Queue.Submit(ctx, request)
}

func (s *JobService) validateLocalJobInternal(ctx context.Context, jobID string) error {
	if metadata, ok := meta.GetJobMetadata(jobID); ok {
		if !metadata.CanRunManually {
			return errors.New("job cannot be run manually")
		}
		if s.cfg.AgentMode && metadata.ManagerOnly {
			return errors.New("job is manager-only")
		}
		if !s.isJobEnabledInternal(ctx, metadata) {
			return errors.New("job is disabled")
		}
		for _, prereq := range s.evaluatePrerequisitesInternal(ctx, metadata) {
			if !prereq.IsMet {
				return errors.New("job prerequisites are not met")
			}
		}
		if metadata.IsContinuous || jobID == "environment-health" {
			return nil
		}
	}
	if s.scheduler == nil {
		return errors.New("scheduler unavailable")
	}
	job, ok := s.scheduler.GetJob(jobID)
	if !ok {
		return errors.New("job is not registered")
	}
	if conditional, ok := job.(st.ConditionalJob); ok && !conditional.ShouldSchedule(ctx) {
		return errors.New("job is disabled")
	}
	return nil
}

func (s *JobService) authorizeRunInternal(ctx context.Context, run st.Run) error {
	if run.RequestedBy == "" {
		if run.Trigger == "manual" && !s.cfg.AgentMode {
			return errors.New("requesting user unavailable")
		}
		return nil
	}
	if s.roles == nil {
		return errors.New("permission resolver unavailable")
	}
	var user common.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", run.RequestedBy).Error; err != nil {
		return err
	}
	if run.RequestedWithKey != "" {
		return s.authorizeKeyInternal(ctx, run)
	}
	permissions, err := s.roles.ResolveUserPermissionsInDB(ctx, s.db.WithContext(ctx), user.ID)
	if err != nil {
		return err
	}
	if !permissions.Allows(authz.PermJobsManage, run.EnvironmentID) {
		return errors.New("requesting user no longer has permission to manage jobs")
	}
	return nil
}

func (s *JobService) executeRunInternal(ctx context.Context, run st.Run) (st.Outcome, error) {
	if err := s.authorizeRunInternal(ctx, run); err != nil {
		return st.Outcome{Status: st.NeedsAttention}, err
	}
	if run.EnvironmentID != "0" {
		return s.deliverRemoteInternal(ctx, run)
	}
	if requiresDockerInternal(run.JobID) && s.environment != nil {
		status, err := s.environment.TestConnection(ctx, "0", nil)
		if err != nil || status != "online" {
			return st.Outcome{Status: st.Waiting, Message: "Waiting for Docker"}, err
		}
	}
	ctx = s.runContextInternal(ctx, run)
	if run.JobID == "environment-health" && s.RunEnvironmentHealthNow != nil {
		err := s.RunEnvironmentHealthNow(ctx)
		return classifyOutcomeInternal(run.JobID, st.Outcome{}, err)
	}
	if metadata, ok := meta.GetJobMetadata(run.JobID); ok && metadata.IsContinuous {
		if err := s.validateLocalJobInternal(ctx, run.JobID); err != nil {
			return st.Outcome{Status: st.Canceled}, err
		}
		err := s.scheduler.RunBusWatcherNow(ctx, run.JobID)
		return classifyOutcomeInternal(run.JobID, st.Outcome{}, err)
	}
	job, ok := s.scheduler.GetJob(run.JobID)
	if !ok {
		return st.Outcome{Status: st.Canceled, Message: "Job or target no longer exists"}, nil
	}
	if conditional, ok := job.(st.ConditionalJob); ok && !conditional.ShouldSchedule(ctx) {
		return st.Outcome{Status: st.Canceled, Message: "Job is disabled"}, nil
	}
	outcome, err := job.Run(ctx)
	return classifyOutcomeInternal(run.JobID, outcome, err)
}

func classifyOutcomeInternal(jobID string, outcome st.Outcome, err error) (st.Outcome, error) {
	var resultErr *st.OutcomeError
	if errors.As(err, &resultErr) {
		return resultErr.Outcome, err
	}
	if err == nil {
		if outcome.Status == "" {
			outcome.Status = st.Succeeded
		}
		return outcome, nil
	}
	if outcome.Status == st.Waiting || outcome.Status == st.Retrying {
		return outcome, err
	}
	if outcome.Status == st.NeedsAttention || outcome.Status == st.Partial || outcome.Status == st.Canceled || outcome.Status == st.Skipped {
		return outcome, err
	}
	var networkErr net.Error
	switch {
	case safeJobInternal(jobID) && (errors.As(err, &networkErr) || errors.Is(err, context.DeadlineExceeded)):
		outcome.Status = st.Retrying
	case !safeJobInternal(jobID):
		outcome.Status = st.NeedsAttention
	case outcome.Status == "":
		outcome.Status = st.Failed
	}

	return outcome, err
}

func safeJobInternal(jobID string) bool {
	switch jobID {
	case "image-polling", "environment-health", "docker-client-refresh", "event-cleanup", "expired-sessions-cleanup", "activity-sweep", "upload-sessions-cleanup", "git-clone-scratch-cleanup", "analytics-heartbeat", "apns-outbox", "vulnerability-scan":
		return true
	}
	return strings.HasPrefix(jobID, "environment-health:")
}

func (s *JobService) reconcileRunInternal(ctx context.Context, run st.Run) (st.Outcome, error) {
	if err := s.authorizeRunInternal(ctx, run); err != nil {
		return st.Outcome{Status: st.NeedsAttention}, err
	}
	if run.EnvironmentID != "0" {
		return s.executeRunInternal(ctx, run)
	}
	if safeJobInternal(run.JobID) {
		return s.executeRunInternal(ctx, run)
	}
	// A successful activity proves only that target, never the entire batch.
	for index, target := range run.Outcome.Targets {
		if target.Status == st.Succeeded || target.Status == st.Skipped {
			continue
		}
		if target.ActivityID == "" {
			return st.Outcome{Status: st.NeedsAttention, Message: "Interrupted operation has no confirmed outcome", Targets: run.Outcome.Targets}, nil
		}
		var record activity.Activity
		if err := s.db.WithContext(ctx).First(&record, "id = ?", target.ActivityID).Error; err != nil {
			return st.Outcome{Status: st.NeedsAttention}, err
		}
		if record.Status != activitytypes.StatusSuccess {
			return st.Outcome{Status: st.NeedsAttention, Message: "Interrupted operation requires review", Targets: run.Outcome.Targets}, nil
		}
		run.Outcome.Targets[index].Status = st.Succeeded
	}
	if job, ok := s.scheduler.GetJob(run.JobID); ok {
		if reconciler, ok := job.(st.Reconciler); ok {
			outcome, err := reconciler.Reconcile(s.runContextInternal(ctx, run), run)
			if err != nil {
				slog.WarnContext(ctx, "Job reconciliation requires attention", "runId", run.ID, "error", err)
			}
			return outcome, err
		}
	}
	return st.Outcome{Status: st.NeedsAttention, Message: "Interrupted operation requires review before retrying", Targets: run.Outcome.Targets}, nil
}

func (s *JobService) runContextInternal(ctx context.Context, run st.Run) context.Context {
	ctx = jobcontext.WithExecution(ctx, run, func(target st.TargetOutcome) error {
		return s.Queue.UpdateRun(ctx, run, func(current *st.Run) error {
			if current.Status != st.Running || current.Owner != run.Owner {
				return queue.ErrRunConflict
			}
			for index := range current.Outcome.Targets {
				if current.Outcome.Targets[index].ID == target.ID {
					current.Outcome.Targets[index] = target
					return nil
				}
			}
			current.Outcome.Targets = append(current.Outcome.Targets, target)
			return nil
		})
	})
	return ctx
}

func (s *JobService) authorizeKeyInternal(ctx context.Context, run st.Run) error {
	var key apikey.ApiKey
	if err := s.db.WithContext(ctx).First(&key, "id = ?", run.RequestedWithKey).Error; err != nil {
		return err
	}
	if key.UserID == nil || *key.UserID != run.RequestedBy || (key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now())) {
		return errors.New("requesting API key is no longer valid")
	}
	if key.Kind == apikey.ApiKeyKindPersonal {
		permissions, err := s.roles.ResolveUserPermissionsInDB(ctx, s.db.WithContext(ctx), run.RequestedBy)
		if err != nil {
			return err
		}
		if !permissions.Allows(authz.PermJobsManage, run.EnvironmentID) {
			return errors.New("requesting user no longer has permission to manage jobs")
		}
		return nil
	}
	var grants []role.ApiKeyPermission
	if err := s.db.WithContext(ctx).Where("api_key_id = ?", key.ID).Find(&grants).Error; err != nil {
		return err
	}
	permissions := authz.NewPermissionSet()
	for _, grant := range grants {
		if grant.EnvironmentID == nil {
			permissions.AddGlobal(grant.Permission)
		} else {
			permissions.AddEnv(*grant.EnvironmentID, grant.Permission)
		}
	}
	if !permissions.Allows(authz.PermJobsManage, run.EnvironmentID) {
		return errors.New("requesting API key no longer has permission to manage jobs")
	}
	return nil
}

func requiresDockerInternal(jobID string) bool {
	switch jobID {
	case "auto-update", "auto-patch", "auto-heal", "scheduled-prune", "vulnerability-scan", "image-polling":
		return true
	}
	return strings.HasPrefix(jobID, "gitops-sync:") || strings.HasPrefix(jobID, "volume-backup:") || strings.HasPrefix(jobID, "system-backup:")
}
