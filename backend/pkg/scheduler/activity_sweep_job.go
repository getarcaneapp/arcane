package scheduler

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
)

const ActivitySweepJobName = "activity-sweep"

// ActivitySweepJob periodically fails queued/running activities whose worker
// is no longer alive in this process, so a lost terminal write (crash, panic,
// dropped DB write) cannot leave an activity stuck in running forever. It is
// an internal job: it has no job_metadata entry and is invisible in the Jobs
// UI.
type ActivitySweepJob struct {
	activityService *services.ActivityService
}

func NewActivitySweepJob(activityService *services.ActivityService) *ActivitySweepJob {
	return &ActivitySweepJob{activityService: activityService}
}

func (j *ActivitySweepJob) Name() string {
	return ActivitySweepJobName
}

func (j *ActivitySweepJob) Schedule(_ context.Context) string {
	return "0 */5 * * * *"
}

func (j *ActivitySweepJob) Run(ctx context.Context) {
	swept, err := j.activityService.FailAbandonedActivities(ctx)
	if err != nil {
		slog.WarnContext(ctx, "activity sweep failed", "jobName", ActivitySweepJobName, "swept", swept, "error", err)
		return
	}
	if swept > 0 {
		slog.InfoContext(ctx, "marked abandoned activities as failed", "jobName", ActivitySweepJobName, "count", swept)
	}
}
