package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

const UpgradeLogCleanupJobName = "upgrade-log-cleanup"

// NewUpgradeLogCleanupJob creates the hourly local upgrade log cleanup job.
func NewUpgradeLogCleanupJob(service *system.SystemUpgradeService) schedulertypes.Job {
	return &schedulertypes.GenericJob{
		JobName:    UpgradeLogCleanupJobName,
		ScheduleFn: func(context.Context) string { return "0 15 * * * *" },
		RunFn: func(ctx context.Context) (schedulertypes.Outcome, error) {
			removed, err := service.PruneUpgradeLogs(ctx, libarcane.UpgradeLogDirectory, time.Now())
			if err != nil {
				slog.ErrorContext(ctx, "Failed to clean up upgrade logs", "jobName", UpgradeLogCleanupJobName, "removed", removed, "error", err)
				return schedulertypes.Outcome{}, err
			}
			if removed > 0 {
				slog.InfoContext(ctx, "Removed expired upgrade logs", "jobName", UpgradeLogCleanupJobName, "removed", removed)
			}
			return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
		},
	}
}
