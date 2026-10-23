package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/system"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
)

const UpgradeLogCleanupJobName = "upgrade-log-cleanup"

// NewUpgradeLogCleanupJob creates the hourly local upgrade log cleanup job.
func NewUpgradeLogCleanupJob(service *system.SystemService) types.SchedulerJob {
	return &types.GenericJob{
		JobName:    UpgradeLogCleanupJobName,
		ScheduleFn: func(context.Context) string { return "0 15 * * * *" },
		RunFn: func(ctx context.Context) (types.SchedulerOutcome, error) {
			removed, err := service.PruneUpgradeLogs(ctx, libarcane.UpgradeLogDirectory, time.Now())
			if err != nil {
				slog.ErrorContext(ctx, "Failed to clean up upgrade logs", "jobName", UpgradeLogCleanupJobName, "removed", removed, "error", err)
				return types.SchedulerOutcome{}, err
			}
			if removed > 0 {
				slog.InfoContext(ctx, "Removed expired upgrade logs", "jobName", UpgradeLogCleanupJobName, "removed", removed)
			}
			return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
		},
	}
}
