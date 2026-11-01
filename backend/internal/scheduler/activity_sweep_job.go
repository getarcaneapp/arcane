package scheduler

import (
	"context"
	"log/slog"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/types/v3"
)

type (
	// ActivitySweepJob periodically fails queued/running activities whose worker
	// is no longer alive in this process, so a lost terminal write (crash, panic,
	// dropped DB write) cannot leave an activity stuck in running forever. It is
	// an internal job: it has no job_metadata entry and is invisible in the Jobs
	// UI.
	ActivitySweepJob struct {
		activityService *activity.ActivityService
	}
)

const (
	ActivitySweepJobName = "activity-sweep"
)

func NewActivitySweepJob(activityService *activity.ActivityService) *ActivitySweepJob {
	return &ActivitySweepJob{activityService: activityService}
}

func (j *ActivitySweepJob) Name() string {
	return ActivitySweepJobName
}

func (j *ActivitySweepJob) Schedule(_ context.Context) string {
	return "*/5 * * * *"
}

func (j *ActivitySweepJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	swept, err := j.activityService.FailAbandonedActivities(ctx)
	if err != nil {
		slog.WarnContext(ctx, "activity sweep failed", "jobName", ActivitySweepJobName, "swept", swept, "error", err)
		return types.SchedulerOutcome{}, err
	}
	if swept > 0 {
		slog.InfoContext(ctx, "marked abandoned activities as failed", "jobName", ActivitySweepJobName, "count", swept)
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}
