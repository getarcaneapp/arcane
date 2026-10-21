package scheduler

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
)

func TestDeprecatedImagePollingSchedulePersistsWithoutRuntimeJob(t *testing.T) {
	ctx := t.Context()
	databasePath := filepath.Join(t.TempDir(), "arcane.db")
	db, settingsService := openJobScheduleTestDatabase(t, ctx, databasePath)

	appConfig := &config.Config{Timezone: "UTC"}
	lifecycleCtx, cancelLifecycle := context.WithCancel(t.Context())
	jobScheduler := newJobSchedulerForTest(t, lifecycleCtx, appConfig.GetLocation())

	jobRuntime := francistest.New(t)
	jobCoordinator := runs.New(kv.NewKVService(db), jobRuntime.Service(), appConfig.GetLocation())
	require.NoError(t, jobCoordinator.Register(jobRuntime))
	francistest.Start(t, jobRuntime)
	jobService := job.NewJobService(
		db,
		settingsService,
		appConfig,
		jobCoordinator,
		role.NewRoleService(db),
		environment.NewEnvironmentService(db, nil, nil, nil, nil, nil),
		activity.NewActivityService(db, settingsService),
	)
	jobService.SetScheduler(lifecycleCtx, jobScheduler)
	require.NoError(t, jobScheduler.StartScheduler(t.Context()))

	_, ok := jobScheduler.GetJobRuntimeState("image-polling")
	require.False(t, ok)
	require.Empty(t, jobScheduler.ListRegisteredJobs())

	requestedSchedule := "0 0 8 * * *"
	updatedSchedules, err := jobService.UpdateJobSchedules(ctx, types.JobScheduleConfig{
		PollingInterval: requestedSchedule,
	}, map[string]bool{"pollingInterval": true})
	require.NoError(t, err)
	require.Equal(t, requestedSchedule, updatedSchedules.PollingInterval)

	var persisted settings.SettingVariable
	require.NoError(t, db.WithContext(ctx).First(&persisted, "key = ?", "pollingInterval").Error)
	require.Equal(t, requestedSchedule, persisted.Value)
	require.Equal(t, requestedSchedule, settingsService.GetStringSetting(ctx, "pollingInterval", ""))

	_, ok = jobScheduler.GetJobRuntimeState("image-polling")
	require.False(t, ok)
	require.Empty(t, jobScheduler.ListRegisteredJobs())

	jobs, err := jobService.ListJobs(ctx)
	require.NoError(t, err)
	imagePollingStatus := findJobStatus(t, jobs, "image-polling")
	require.Equal(t, requestedSchedule, imagePollingStatus.Schedule)
	require.NotNil(t, imagePollingStatus.NextRun)
	require.True(t, imagePollingStatus.IsContinuous)
	require.Equal(t, "pollingInterval", imagePollingStatus.SettingsKey)

	cancelLifecycle()
	require.NoError(t, stopJobSchedulerForTest(t.Context(), jobScheduler))
	closeJobScheduleTestDatabase(t, db)

	restartDB, restartSettingsService := openJobScheduleTestDatabase(t, ctx, databasePath)
	restartLifecycleCtx, cancelRestartLifecycle := context.WithCancel(t.Context())
	restartScheduler := newJobSchedulerForTest(t, restartLifecycleCtx, appConfig.GetLocation())
	restartRuntime := francistest.New(t)
	restartCoordinator := runs.New(kv.NewKVService(restartDB), restartRuntime.Service(), appConfig.GetLocation())
	require.NoError(t, restartCoordinator.Register(restartRuntime))
	francistest.Start(t, restartRuntime)
	restartJobService := job.NewJobService(
		restartDB,
		restartSettingsService,
		appConfig,
		restartCoordinator,
		role.NewRoleService(restartDB),
		environment.NewEnvironmentService(restartDB, nil, nil, nil, nil, nil),
		activity.NewActivityService(restartDB, restartSettingsService),
	)
	restartJobService.SetScheduler(restartLifecycleCtx, restartScheduler)
	require.NoError(t, restartScheduler.StartScheduler(t.Context()))
	t.Cleanup(func() {
		cancelRestartLifecycle()
		require.NoError(t, stopJobSchedulerForTest(context.WithoutCancel(t.Context()), restartScheduler))
		closeJobScheduleTestDatabase(t, restartDB)
	})

	_, ok = restartScheduler.GetJobRuntimeState("image-polling")
	require.False(t, ok)
	require.Empty(t, restartScheduler.ListRegisteredJobs())
	require.Equal(t, requestedSchedule, restartSettingsService.GetStringSetting(ctx, "pollingInterval", ""))

	restartedJobs, err := restartJobService.ListJobs(ctx)
	require.NoError(t, err)
	require.Equal(t, requestedSchedule, findJobStatus(t, restartedJobs, "image-polling").Schedule)

	require.NoError(t, restartSettingsService.SetBoolSetting(ctx, "pollingEnabled", false))
	disabledSchedule := "0 0 9 * * *"
	_, err = restartJobService.UpdateJobSchedules(ctx, types.JobScheduleConfig{PollingInterval: disabledSchedule}, map[string]bool{"pollingInterval": true})
	require.NoError(t, err)

	_, ok = restartScheduler.GetJobRuntimeState("image-polling")
	require.False(t, ok)
	require.Empty(t, restartScheduler.ListRegisteredJobs())

	disabledJobs, err := restartJobService.ListJobs(ctx)
	require.NoError(t, err)
	disabledStatus := findJobStatus(t, disabledJobs, "image-polling")
	require.False(t, disabledStatus.Enabled)
	require.Equal(t, disabledSchedule, disabledStatus.Schedule)
}

func openJobScheduleTestDatabase(t *testing.T, ctx context.Context, databasePath string) (*database.DB, *settings.SettingsService) {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}, &kv.KVEntry{}))

	db := &database.DB{DB: gormDB}
	settingsService, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsService.LoadDatabaseSettings(ctx))

	return db, settingsService
}

func closeJobScheduleTestDatabase(t *testing.T, db *database.DB) {
	t.Helper()

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

func findJobStatus(t *testing.T, jobs *types.JobListResponse, jobID string) types.JobStatus {
	t.Helper()

	for _, job := range jobs.Jobs {
		if job.ID == jobID {
			return job
		}
	}
	require.FailNowf(t, "unexpected failure", "job %q not found", jobID)
	return types.JobStatus{}
}
