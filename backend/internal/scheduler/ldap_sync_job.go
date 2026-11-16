package scheduler

import (
	"context"
	"log/slog"

	"getarcane.dev/arcane/v3/internal/ldap"
	"getarcane.dev/arcane/v3/internal/scheduler/schedule"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/types/v3"
)

type (
	LdapSyncJob struct {
		ldapService     *ldap.LdapService
		settingsService *settings.SettingsService
	}
)

const (
	LdapSyncJobName = "ldap-sync"
)

func NewLdapSyncJob(ldapService *ldap.LdapService, settingsService *settings.SettingsService) *LdapSyncJob {
	return &LdapSyncJob{
		ldapService:     ldapService,
		settingsService: settingsService,
	}
}

func (j *LdapSyncJob) Name() string {
	return LdapSyncJobName
}

func (j *LdapSyncJob) ShouldSchedule(ctx context.Context) bool {
	return j.settingsService.GetBoolSetting(ctx, "ldapEnabled")
}

func (j *LdapSyncJob) Schedule(ctx context.Context) string {
	return schedule.Or(ctx, j.settingsService.GetStringSetting(ctx, "ldapSyncInterval"), "0 * * * *", LdapSyncJobName)
}

func (j *LdapSyncJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	if !j.ShouldSchedule(ctx) {
		return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
	}
	if err := j.ldapService.SyncAll(ctx); err != nil {
		slog.ErrorContext(ctx, "LDAP sync failed", "jobName", LdapSyncJobName, "error", err)
		return types.SchedulerOutcome{
			Status:  types.RunStatusFailed,
			Message: err.Error(),
		}, err
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}
