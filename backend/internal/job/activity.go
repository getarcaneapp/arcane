package job

import (
	"context"
	"errors"
	"strings"
	"uuid"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/types/v2/meta"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

// ActivityID assigns a summary identity before a visible run is persisted.
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
	return uuid.New().String()
}

// SyncRunActivity projects the latest committed run without changing execution state.
func (s *JobService) SyncRunActivity(ctx context.Context, run st.Run) error {
	if s.activity == nil {
		return nil
	}
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	current, err := s.runs.Get(ctx, run.EnvironmentID, run.JobID, run.ID)
	if errors.Is(err, runs.ErrRunNotFound) {
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
	return s.activity.SyncJobRun(ctx, projectRunOutcomeInternal(current), name)
}

func (s *JobService) ReconcileStartupActivities(ctx context.Context, extraProtectedIDs ...string) error {
	if s.activity == nil {
		return nil
	}
	records, err := s.runs.Records(ctx)
	if err != nil {
		return err
	}
	protected := append([]string(nil), extraProtectedIDs...)
	for _, record := range records {
		for _, run := range record.Runs {
			if run.Status.Terminal() {
				continue
			}
			if run.ActivityID != "" {
				protected = append(protected, run.ActivityID)
			}
			if run.Outcome.ActivityID != "" {
				protected = append(protected, run.Outcome.ActivityID)
			}
			for _, target := range run.Outcome.Targets {
				if target.ActivityID != "" {
					protected = append(protected, target.ActivityID)
				}
			}
		}
	}
	if err := s.activity.FailInterruptedBackups(ctx, protected...); err != nil {
		return err
	}
	if _, err := s.activity.FailStaleImageUpdateChecks(ctx); err != nil {
		return err
	}
	if _, err := s.activity.ResolveStaleAutoUpdateActivities(ctx, protected...); err != nil {
		return err
	}
	_, err = s.activity.ResolveOrphanedQueuedActivities(ctx, protected...)
	return err
}
