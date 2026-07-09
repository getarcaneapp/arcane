package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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

func TestImagePollingScheduleUpdatePersistsAndReplacesRuntimeEntry(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "arcane.db")
	db, settingsService := openJobScheduleTestDatabaseInternal(t, ctx, databasePath)

	appConfig := &config.Config{Timezone: "UTC"}
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	jobScheduler := NewJobScheduler(lifecycleCtx, appConfig.GetLocation())
	jobScheduler.RegisterJob(NewImagePollingJob(nil, settingsService, nil))

	jobService := services.NewJobService(db, settingsService, appConfig)
	jobService.SetScheduler(lifecycleCtx, jobScheduler)
	jobScheduler.StartScheduler()

	initialState, ok := jobScheduler.GetJobRuntimeState("image-polling")
	require.True(t, ok)
	require.True(t, initialState.Scheduled)
	require.Equal(t, "0 0 * * * *", initialState.Schedule)
	require.Len(t, jobScheduler.cron.Entries(), 1)
	initialEntryID := jobScheduler.entryIDs["image-polling"]

	requestedSchedule := "0 0 8 * * *"
	beforeUpdate := time.Now().UTC()
	updatedSchedules, err := jobService.UpdateJobSchedules(ctx, jobschedule.Update{
		PollingInterval: &requestedSchedule,
	})
	afterUpdate := time.Now().UTC()
	require.NoError(t, err)
	require.Equal(t, requestedSchedule, updatedSchedules.PollingInterval)

	var persisted models.SettingVariable
	require.NoError(t, db.WithContext(ctx).First(&persisted, "key = ?", "pollingInterval").Error)
	require.Equal(t, requestedSchedule, persisted.Value)
	require.Equal(t, requestedSchedule, settingsService.GetStringSetting(ctx, "pollingInterval", ""))

	runtimeState, ok := jobScheduler.GetJobRuntimeState("image-polling")
	require.True(t, ok)
	require.True(t, runtimeState.Scheduled)
	require.Equal(t, requestedSchedule, runtimeState.Schedule)
	require.NotNil(t, runtimeState.NextRun)
	require.Equal(t, "UTC", runtimeState.NextRun.Location().String())
	requireNextDailyRunInternal(t, *runtimeState.NextRun, beforeUpdate, afterUpdate)
	require.Len(t, jobScheduler.cron.Entries(), 1)
	require.NotEqual(t, initialEntryID, jobScheduler.entryIDs["image-polling"])
	for _, entry := range jobScheduler.cron.Entries() {
		require.NotEqual(t, initialEntryID, entry.ID)
	}

	jobs, err := jobService.ListJobs(ctx)
	require.NoError(t, err)
	imagePollingStatus := findJobStatusInternal(t, jobs, "image-polling")
	require.Equal(t, requestedSchedule, imagePollingStatus.Schedule)
	require.Equal(t, runtimeState.NextRun, imagePollingStatus.NextRun)

	cancelLifecycle()
	waitForSchedulerStopInternal(jobScheduler)
	closeJobScheduleTestDatabaseInternal(t, db)

	restartDB, restartSettingsService := openJobScheduleTestDatabaseInternal(t, ctx, databasePath)
	restartLifecycleCtx, cancelRestartLifecycle := context.WithCancel(context.Background())
	restartScheduler := NewJobScheduler(restartLifecycleCtx, appConfig.GetLocation())
	restartScheduler.RegisterJob(NewImagePollingJob(nil, restartSettingsService, nil))
	restartJobService := services.NewJobService(restartDB, restartSettingsService, appConfig)
	restartJobService.SetScheduler(restartLifecycleCtx, restartScheduler)
	restartScheduler.StartScheduler()
	t.Cleanup(func() {
		cancelRestartLifecycle()
		waitForSchedulerStopInternal(restartScheduler)
		closeJobScheduleTestDatabaseInternal(t, restartDB)
	})

	restartedState, ok := restartScheduler.GetJobRuntimeState("image-polling")
	require.True(t, ok)
	require.True(t, restartedState.Scheduled)
	require.Equal(t, requestedSchedule, restartedState.Schedule)
	require.NotNil(t, restartedState.NextRun)
	require.Len(t, restartScheduler.cron.Entries(), 1)
	require.Equal(t, requestedSchedule, restartSettingsService.GetStringSetting(ctx, "pollingInterval", ""))

	restartedJobs, err := restartJobService.ListJobs(ctx)
	require.NoError(t, err)
	require.Equal(t, requestedSchedule, findJobStatusInternal(t, restartedJobs, "image-polling").Schedule)

	require.NoError(t, restartSettingsService.SetBoolSetting(ctx, "pollingEnabled", false))
	disabledSchedule := "0 0 9 * * *"
	_, err = restartJobService.UpdateJobSchedules(ctx, jobschedule.Update{PollingInterval: &disabledSchedule})
	require.NoError(t, err)

	disabledState, ok := restartScheduler.GetJobRuntimeState("image-polling")
	require.True(t, ok)
	require.False(t, disabledState.Scheduled)
	require.Nil(t, disabledState.NextRun)
	require.Equal(t, disabledSchedule, disabledState.Schedule)
	require.Empty(t, restartScheduler.cron.Entries())

	disabledJobs, err := restartJobService.ListJobs(ctx)
	require.NoError(t, err)
	disabledStatus := findJobStatusInternal(t, disabledJobs, "image-polling")
	require.False(t, disabledStatus.Enabled)
	require.Nil(t, disabledStatus.NextRun)
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

func requireNextDailyRunInternal(t *testing.T, actual, beforeUpdate, afterUpdate time.Time) {
	t.Helper()

	nextAtEight := func(reference time.Time) time.Time {
		next := time.Date(reference.Year(), reference.Month(), reference.Day(), 8, 0, 0, 0, time.UTC)
		if !next.After(reference) {
			next = next.AddDate(0, 0, 1)
		}
		return next
	}

	expectedBefore := nextAtEight(beforeUpdate)
	expectedAfter := nextAtEight(afterUpdate)
	require.Truef(t, actual.Equal(expectedBefore) || actual.Equal(expectedAfter), "next run %s is neither %s nor %s", actual, expectedBefore, expectedAfter)
}
