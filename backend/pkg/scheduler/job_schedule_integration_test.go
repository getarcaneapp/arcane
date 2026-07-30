package scheduler

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeprecatedImagePollingSchedulePersistsWithoutRuntimeJob(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "arcane.db")
	db, settingsService := openJobScheduleTestDatabaseInternal(t, ctx, databasePath)

	appConfig := &config.Config{Timezone: "UTC"}
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	jobScheduler := NewJobScheduler(lifecycleCtx, appConfig.GetLocation())

	jobService := services.NewJobService(db, settingsService, appConfig)
	jobService.SetScheduler(lifecycleCtx, jobScheduler)
	jobScheduler.StartScheduler()

	_, ok := jobScheduler.GetJobRuntimeState("image-polling")
	require.False(t, ok)
	require.Empty(t, jobScheduler.cron.Entries())

	requestedSchedule := "0 0 8 * * *"
	updatedSchedules, err := jobService.UpdateJobSchedules(ctx, jobschedule.Update{
		PollingInterval: &requestedSchedule,
	})
	require.NoError(t, err)
	require.Equal(t, requestedSchedule, updatedSchedules.PollingInterval)

	var persisted models.SettingVariable
	require.NoError(t, db.WithContext(ctx).First(&persisted, "key = ?", "pollingInterval").Error)
	require.Equal(t, requestedSchedule, persisted.Value)
	require.Equal(t, requestedSchedule, settingsService.GetStringSetting(ctx, "pollingInterval", ""))

	_, ok = jobScheduler.GetJobRuntimeState("image-polling")
	require.False(t, ok)
	require.Empty(t, jobScheduler.cron.Entries())

	jobs, err := jobService.ListJobs(ctx)
	require.NoError(t, err)
	imagePollingStatus := findJobStatusInternal(t, jobs, "image-polling")
	require.Equal(t, requestedSchedule, imagePollingStatus.Schedule)
	require.NotNil(t, imagePollingStatus.NextRun)
	require.True(t, imagePollingStatus.IsContinuous)
	require.Equal(t, "pollingInterval", imagePollingStatus.SettingsKey)

	cancelLifecycle()
	waitForSchedulerStopInternal(jobScheduler)
	closeJobScheduleTestDatabaseInternal(t, db)

	restartDB, restartSettingsService := openJobScheduleTestDatabaseInternal(t, ctx, databasePath)
	restartLifecycleCtx, cancelRestartLifecycle := context.WithCancel(context.Background())
	restartScheduler := NewJobScheduler(restartLifecycleCtx, appConfig.GetLocation())
	restartJobService := services.NewJobService(restartDB, restartSettingsService, appConfig)
	restartJobService.SetScheduler(restartLifecycleCtx, restartScheduler)
	restartScheduler.StartScheduler()
	t.Cleanup(func() {
		cancelRestartLifecycle()
		waitForSchedulerStopInternal(restartScheduler)
		closeJobScheduleTestDatabaseInternal(t, restartDB)
	})

	_, ok = restartScheduler.GetJobRuntimeState("image-polling")
	require.False(t, ok)
	require.Empty(t, restartScheduler.cron.Entries())
	require.Equal(t, requestedSchedule, restartSettingsService.GetStringSetting(ctx, "pollingInterval", ""))

	restartedJobs, err := restartJobService.ListJobs(ctx)
	require.NoError(t, err)
	require.Equal(t, requestedSchedule, findJobStatusInternal(t, restartedJobs, "image-polling").Schedule)

	require.NoError(t, restartSettingsService.SetBoolSetting(ctx, "pollingEnabled", false))
	disabledSchedule := "0 0 9 * * *"
	_, err = restartJobService.UpdateJobSchedules(ctx, jobschedule.Update{PollingInterval: &disabledSchedule})
	require.NoError(t, err)

	_, ok = restartScheduler.GetJobRuntimeState("image-polling")
	require.False(t, ok)
	require.Empty(t, restartScheduler.cron.Entries())

	disabledJobs, err := restartJobService.ListJobs(ctx)
	require.NoError(t, err)
	disabledStatus := findJobStatusInternal(t, disabledJobs, "image-polling")
	require.False(t, disabledStatus.Enabled)
	require.Equal(t, disabledSchedule, disabledStatus.Schedule)
}

func openJobScheduleTestDatabaseInternal(t *testing.T, ctx context.Context, databasePath string) (*database.DB, *services.SettingsService) {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.SettingVariable{}))

	db := &database.DB{DB: gormDB}
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsService.LoadDatabaseSettings(ctx))

	return db, settingsService
}

func closeJobScheduleTestDatabaseInternal(t *testing.T, db *database.DB) {
	t.Helper()

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

func waitForSchedulerStopInternal(jobScheduler *JobScheduler) {
	<-jobScheduler.cron.Stop().Done()
}

func findJobStatusInternal(t *testing.T, jobs *jobschedule.JobListResponse, jobID string) jobschedule.JobStatus {
	t.Helper()

	for _, job := range jobs.Jobs {
		if job.ID == jobID {
			return job
		}
	}

	t.Fatalf("job %q not found", jobID)
	return jobschedule.JobStatus{}
}
