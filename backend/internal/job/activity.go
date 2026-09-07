package job

import (
	"context"
	"strings"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/queue"
	"github.com/getarcaneapp/arcane/types/v2/meta"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/google/uuid"
)

// ActivityID gives visible runs a stable summary identity before execution.
func (s *JobService) ActivityID(run st.Run) string {
	// A remotely delivered run already has a summary on its accepting manager.
	if run.Trigger == "remote" {
		return ""
	}
	visible := run.Trigger == "manual"
	switch run.JobID {
	case "auto-update", "auto-patch", "auto-heal", "scheduled-prune", "vulnerability-scan":
		visible = true
	default:
		visible = visible || strings.HasPrefix(run.JobID, "gitops-sync:") || strings.HasPrefix(run.JobID, "volume-backup:") || strings.HasPrefix(run.JobID, "system-backup:")
	}
	if !visible {
		return ""
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("arcane-job/"+run.EnvironmentID+"/"+run.JobID+"/"+run.ID)).String()
}

// SyncRunActivity projects the latest committed run without changing execution state.
func (s *JobService) SyncRunActivity(ctx context.Context, run st.Run) error {
	if s.activity == nil {
		return nil
	}
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	current, err := s.Queue.Get(ctx, run.EnvironmentID, run.JobID, run.ID)
	if errors.Is(err, queue.ErrRunNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.ActivityID == "" {
		return nil
	}
	name := current.JobID
	if metadata, found := meta.GetJobMetadata(current.JobID); found {
		name = metadata.Name
	}
	return s.activity.SyncJobRun(ctx, current, name)
}
