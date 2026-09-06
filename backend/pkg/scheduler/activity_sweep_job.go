package scheduler

import (
	"context"
	"log/slog"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
)

const ActivitySweepJobName = "activity-sweep"

// ActivitySweepJob periodically fails queued/running activities whose worker
// is no longer alive in this process, so a lost terminal write (crash, panic,
// dropped DB write) cannot leave an activity stuck in running forever. It is
// an internal job: it has no job_metadata entry and is invisible in the Jobs
// UI.
type ActivitySweepJob struct {
	activityService *activity.ActivityService
}

func NewActivitySweepJob(activityService *activity.ActivityService) *ActivitySweepJob {
	return &ActivitySweepJob{activityService: activityService}
}

func (j *ActivitySweepJob) Name() string {
	return ActivitySweepJobName
}

func (j *ActivitySweepJob) Schedule(_ context.Context) string {
	return "0 */5 * * * *"
}

func (j *ActivitySweepJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	swept, err := j.activityService.FailAbandonedActivities(ctx)
	if err != nil {
		slog.WarnContext(ctx, "activity sweep failed", "jobName", ActivitySweepJobName, "swept", swept, "error", err)
		return schedulertypes.Outcome{}, err
	}
	if swept > 0 {
		slog.InfoContext(ctx, "marked abandoned activities as failed", "jobName", ActivitySweepJobName, "count", swept)
	}
	return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
}
