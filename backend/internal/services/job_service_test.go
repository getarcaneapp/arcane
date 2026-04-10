package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/types/jobschedule"
	"github.com/stretchr/testify/require"
)

func TestJobService_GetJobSchedules_DefaultGitOpsInterval(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	cfg := jobSvc.GetJobSchedules(ctx)

	require.Equal(t, "0 */1 * * * *", cfg.GitopsSyncInterval)
}

func TestJobService_ListJobs_AnalyticsHeartbeatIsManagedInternally(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	analyticsJob := findJobStatusByIDInternal(t, jobs.Jobs, "analytics-heartbeat")
	require.Equal(t, "automatic (checked hourly; sent once per 24h)", analyticsJob.Schedule)
	require.Empty(t, analyticsJob.SettingsKey)
	require.Nil(t, analyticsJob.NextRun)
	require.True(t, analyticsJob.CanRunManually)
	require.False(t, analyticsJob.IsContinuous)
}

func TestJobService_ListJobs_IncludesDisabledAutoHealJob(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoHealEnabled", false))

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	autoHealJob := findJobStatusByIDInternal(t, jobs.Jobs, "auto-heal")
	require.False(t, autoHealJob.Enabled)
	require.Equal(t, "autoHealInterval", autoHealJob.SettingsKey)
}

func findJobStatusByIDInternal(t *testing.T, jobs []jobschedule.JobStatus, id string) jobschedule.JobStatus {
	t.Helper()

	for _, job := range jobs {
		if job.ID == id {
			return job
		}
	}

	t.Fatalf("job %q not found", id)
	return jobschedule.JobStatus{}
}
