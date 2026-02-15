package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitOpsSyncJobSchedule_Default(t *testing.T) {
	ctx := context.Background()
	settingsSvc := setupAnalyticsSettingsService(t)
	job := NewGitOpsSyncJob(nil, settingsSvc)

	got := job.Schedule(ctx)
	require.Equal(t, "0 */1 * * * *", got)
}

func TestGitOpsSyncJobSchedule_UsesConfiguredCron(t *testing.T) {
	ctx := context.Background()
	settingsSvc := setupAnalyticsSettingsService(t)
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "gitopsSyncInterval", "0 */7 * * * *"))
	job := NewGitOpsSyncJob(nil, settingsSvc)

	got := job.Schedule(ctx)
	require.Equal(t, "0 */7 * * * *", got)
}

func TestGitOpsSyncJobSchedule_LegacyIntegerMinutes(t *testing.T) {
	ctx := context.Background()
	settingsSvc := setupAnalyticsSettingsService(t)
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "gitopsSyncInterval", "120"))
	job := NewGitOpsSyncJob(nil, settingsSvc)

	got := job.Schedule(ctx)
	require.Equal(t, "0 0 */2 * * *", got)
}

func TestGitOpsSyncJobSchedule_InvalidCronFallsBackToDefault(t *testing.T) {
	ctx := context.Background()
	settingsSvc := setupAnalyticsSettingsService(t)
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "gitopsSyncInterval", "not-a-cron"))
	job := NewGitOpsSyncJob(nil, settingsSvc)

	got := job.Schedule(ctx)
	require.Equal(t, "0 */1 * * * *", got)
}
