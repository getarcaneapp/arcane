package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
)

const AuditRetentionJobName = "audit-retention"

func NewAuditRetentionJob(service *audit.AuditService, settingsService *settings.SettingsService) types.SchedulerJob {
	return &types.GenericJob{
		JobName: AuditRetentionJobName,
		ScheduleFn: func(context.Context) string {
			return "0 30 3 * * *"
		},
		RunFn: func(ctx context.Context) (types.SchedulerOutcome, error) {
			days := settingsService.GetIntSetting(ctx, "auditRetentionDays")
			if days <= 0 {
				return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
			}
			removed, err := service.Prune(ctx, time.Now().AddDate(0, 0, -days))
			if err != nil {
				slog.ErrorContext(ctx, "Failed to prune audit logs", "jobName", AuditRetentionJobName, "retentionDays", days, "error", err)
				return types.SchedulerOutcome{}, err
			}
			if removed > 0 {
				slog.InfoContext(ctx, "Pruned expired audit log entries", "jobName", AuditRetentionJobName, "retentionDays", days, "removed", removed)
			}
			return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
		},
	}
}
