package activity

import (
	"context"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"gorm.io/gorm"
)

// SyncJobRun updates a durable job summary without taking an execution slot.
// Only the queue owns its lifecycle, including reopening it for an explicit retry.
func (s *ActivityService) SyncJobRun(ctx context.Context, run st.Run, name string) error {
	if err := s.checkInitInternal(); err != nil {
		return err
	}
	if run.ActivityID == "" {
		return errors.New("job activity ID is required")
	}
	lock := s.publishLockInternal(run.ActivityID)
	lock.Lock()
	defer lock.Unlock()
	model := jobActivityInternal(run, name)
	changed := false
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing Activity
		err := tx.First(&existing, "id = ?", run.ActivityID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			changed = true
			return tx.Create(&model).Error
		}
		if err != nil {
			return err
		}
		if existing.Type != activitytypes.TypeJobRun {
			return errors.New("job activity identity belongs to another operation")
		}
		if stamp, ok := existing.Metadata["runUpdatedAt"].(string); ok {
			previous, parseErr := time.Parse(time.RFC3339Nano, stamp)
			if parseErr == nil && !previous.Before(run.UpdatedAt) {
				if existing.EnvironmentID == model.EnvironmentID {
					return nil
				}
				existing.EnvironmentID = model.EnvironmentID
				model = existing
			}
		}
		changed = true
		return tx.Table("activities").Where("id = ?", run.ActivityID).Updates(map[string]any{
			"environment_id": model.EnvironmentID,
			"status":         model.Status, "step": model.Step, "latest_message": model.LatestMessage,
			"ended_at": model.EndedAt, "duration_ms": model.DurationMs, "error": model.Error,
			"metadata": model.Metadata, "updated_at": model.UpdatedAt,
		}).Error
	}); err != nil {
		return errors.WrapIf(err, "synchronize job activity")
	}
	if changed {
		// Ordinary activities cannot reopen. Job summaries can, after an explicit
		// retry; the publish lock and durable timestamp reject stale snapshots.
		s.terminalPublishedMu.Lock()
		delete(s.terminalPublished, model.ID)
		s.terminalPublishedMu.Unlock()
		s.publishActivityInternal(activityToDTOInternal(&model))
	}
	return nil
}

func jobActivityInternal(run st.Run, name string) Activity {
	status := activitytypes.StatusQueued
	switch run.Status {
	case st.Running:
		status = activitytypes.StatusRunning
	case st.Succeeded, st.Skipped:
		status = activitytypes.StatusSuccess
	case st.Failed, st.Partial, st.NeedsAttention:
		status = activitytypes.StatusFailed
	case st.Canceled:
		status = activitytypes.StatusCancelled
	case st.Queued, st.Waiting, st.Retrying:
	}
	message := run.Outcome.Message
	if message == "" {
		message = name + ": " + string(run.Status)
	}
	model := Activity{
		ID: run.ActivityID, CreatedAt: run.CreatedAt, UpdatedAt: new(run.UpdatedAt),
		EnvironmentID: run.EnvironmentID, BatchID: new(run.ID), Type: activitytypes.TypeJobRun, Status: status,
		ResourceType: new("job"), ResourceID: new(run.JobID), ResourceName: new(name),
		StartedAt: run.CreatedAt, Step: string(run.Status), LatestMessage: message,
		Metadata: database.JSON{
			"jobId": run.JobID, "runId": run.ID, "environmentId": run.EnvironmentID,
			"jobStatus": string(run.Status), "trigger": run.Trigger, "attemptCount": run.AttemptCount,
			"runUpdatedAt": run.UpdatedAt.Format(time.RFC3339Nano), "domainActivityId": run.Outcome.ActivityID,
		},
	}
	if run.RequestedBy != "" {
		model.StartedByUserID = new(run.RequestedBy)
		model.StartedByUsername = new(run.RequestedBy)
	}
	if run.Resolution != nil {
		model.Metadata["resolution"] = run.Resolution
		model.LatestMessage = "Resolved after review; future scheduled runs may proceed"
	}
	if isTerminalActivityStatusInternal(status) {
		model.EndedAt = new(run.UpdatedAt)
		model.DurationMs = new(max(int64(0), run.UpdatedAt.Sub(run.CreatedAt).Milliseconds()))
	}
	if status == activitytypes.StatusFailed {
		model.Error = new(message)
	}
	return model
}
