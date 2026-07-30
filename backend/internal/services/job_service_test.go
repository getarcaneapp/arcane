package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
)

func TestJobService_GetJobSchedules_DefaultDockerClientRefreshInterval(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	cfg := jobSvc.GetJobSchedules(ctx)

	require.Equal(t, "*/30 * * * * *", cfg.DockerClientRefreshInterval)
	require.Equal(t, "0 0 * * * *", cfg.PollingInterval)
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

func TestJobService_ListJobs_IncludesDockerClientRefreshJob(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	refreshJob := findJobStatusByIDInternal(t, jobs.Jobs, "docker-client-refresh")
	require.True(t, refreshJob.Enabled)
	require.True(t, refreshJob.CanRunManually)
	require.Equal(t, "monitoring", refreshJob.Category)
	require.Equal(t, "dockerClientRefreshInterval", refreshJob.SettingsKey)
	require.Equal(t, "*/30 * * * * *", refreshJob.Schedule)
}

func TestJobService_ListJobs_UsesRuntimeScheduleAndNextRun(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	nextRun := time.Date(2026, time.July, 10, 8, 0, 0, 0, time.UTC)
	scheduler := newFakeJobSchedulerInternal("auto-update")
	scheduler.runtimeStates["auto-update"] = schedulertypes.JobRuntimeState{
		Schedule:  "0 0 8 * * *",
		NextRun:   &nextRun,
		Scheduled: true,
	}

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	jobSvc.SetScheduler(ctx, scheduler)
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	autoUpdateJob := findJobStatusByIDInternal(t, jobs.Jobs, "auto-update")
	require.Equal(t, "0 0 8 * * *", autoUpdateJob.Schedule)
	require.Equal(t, nextRun, *autoUpdateJob.NextRun)
}

func TestJobService_ListJobs_ImageUpdateWatcherIsContinuousAndRespectsEnabled(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "pollingEnabled", false))

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	watcher := findJobStatusByIDInternal(t, jobs.Jobs, "image-polling")
	require.Equal(t, "Image Update Watcher", watcher.Name)
	require.Equal(t, "0 0 * * * *", watcher.Schedule)
	require.Equal(t, "pollingInterval", watcher.SettingsKey)
	require.NotNil(t, watcher.NextRun)
	require.True(t, watcher.IsContinuous)
	require.True(t, watcher.CanRunManually)
	require.False(t, watcher.Enabled)
}

func TestJobService_UpdateJobSchedules_ReschedulesChangedJob(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	scheduler := newFakeJobSchedulerInternal("auto-update")
	jobSvc.SetScheduler(ctx, scheduler)

	_, err = jobSvc.UpdateJobSchedules(ctx, jobschedule.Update{
		AutoUpdateInterval: new("0 */10 * * * *"),
	})
	require.NoError(t, err)

	require.Equal(t, []string{"auto-update"}, scheduler.rescheduled)
}

func TestJobService_UpdateJobSchedules_DeprecatedPollingIntervalDoesNotReschedule(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	scheduler := newFakeJobSchedulerInternal("auto-update")
	jobSvc.SetScheduler(ctx, scheduler)

	updated, err := jobSvc.UpdateJobSchedules(ctx, jobschedule.Update{
		PollingInterval: new("0 */10 * * * *"),
	})
	require.NoError(t, err)
	require.Equal(t, "0 */10 * * * *", updated.PollingInterval)
	require.Empty(t, scheduler.rescheduled)
}

func TestJobService_UpdateJobSchedules_UsesLifecycleContextForReschedule(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	type lifecycleContextKey struct{}
	lifecycleCtx := context.WithValue(context.Background(), lifecycleContextKey{}, true)
	requestCtx, cancelRequest := context.WithCancel(context.Background())

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	scheduler := newFakeJobSchedulerInternal("auto-update")
	jobSvc.SetScheduler(lifecycleCtx, scheduler)

	_, err = jobSvc.UpdateJobSchedules(requestCtx, jobschedule.Update{
		AutoUpdateInterval: new("0 */10 * * * *"),
	})
	require.NoError(t, err)

	cancelRequest()

	require.Len(t, scheduler.rescheduleContexts, 1)
	require.NoError(t, scheduler.rescheduleContexts[0].Err())
	require.Equal(t, true, scheduler.rescheduleContexts[0].Value(lifecycleContextKey{}))
}

func TestJobService_UpdateJobSchedules_RejectsInvalidCronWithoutChangingSetting(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.LoadDatabaseSettings(ctx))

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	scheduler := newFakeJobSchedulerInternal()
	jobSvc.SetScheduler(ctx, scheduler)

	_, err = jobSvc.UpdateJobSchedules(ctx, jobschedule.Update{
		PollingInterval: new("not a cron expression"),
	})
	require.ErrorContains(t, err, "invalid cron expression for pollingInterval")
	require.Equal(t, "0 0 * * * *", settingsSvc.GetStringSetting(ctx, "pollingInterval", ""))
	require.Empty(t, scheduler.rescheduled)
}

func TestJobService_UpdateJobSchedules_UnchangedScheduleDoesNotReschedule(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	updated, err := jobSvc.UpdateJobSchedules(ctx, jobschedule.Update{
		PollingInterval: new("0 0 * * * *"),
	})
	require.NoError(t, err)
	require.Equal(t, "0 0 * * * *", updated.PollingInterval)
}

func TestJobService_UpdateJobSchedules_RestoresPreviousScheduleWhenRescheduleFails(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.LoadDatabaseSettings(ctx))

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	scheduler := newFakeJobSchedulerInternal("auto-update")
	scheduler.rescheduleErr = errors.New("scheduler unavailable")
	jobSvc.SetScheduler(ctx, scheduler)

	_, err = jobSvc.UpdateJobSchedules(ctx, jobschedule.Update{
		AutoUpdateInterval: new("0 0 8 * * *"),
	})
	require.ErrorContains(t, err, "scheduler unavailable")
	require.Equal(t, "0 0 0 * * *", settingsSvc.GetStringSetting(ctx, "autoUpdateInterval", ""))

	var persisted models.SettingVariable
	require.NoError(t, db.WithContext(ctx).First(&persisted, "key = ?", "autoUpdateInterval").Error)
	require.Equal(t, "0 0 0 * * *", persisted.Value)
}

func TestJobService_UpdateJobSchedules_SkipsManagerOnlyJobsInAgentMode(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{AgentMode: true})
	scheduler := newFakeJobSchedulerInternal("environment-health")
	jobSvc.SetScheduler(ctx, scheduler)

	_, err = jobSvc.UpdateJobSchedules(ctx, jobschedule.Update{
		EnvironmentHealthInterval: new("0 */5 * * * *"),
	})
	require.NoError(t, err)

	require.Empty(t, scheduler.rescheduled)
}

func TestJobService_UpdateJobSchedules_DelegatesEnvironmentHealthReschedule(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	scheduler := newFakeJobSchedulerInternal()
	jobSvc.SetScheduler(ctx, scheduler)

	rescheduled := 0
	jobSvc.OnEnvironmentHealthReschedule = func(context.Context) {
		rescheduled++
	}

	_, err = jobSvc.UpdateJobSchedules(ctx, jobschedule.Update{
		EnvironmentHealthInterval: new("0 */5 * * * *"),
	})
	require.NoError(t, err)
	require.Equal(t, 1, rescheduled)
	require.Empty(t, scheduler.rescheduled)
}

func TestJobService_RunJobNowInline_DelegatesImageUpdateWatcherWithDetachedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scheduler := newFakeJobSchedulerInternal()
	jobSvc := &JobService{scheduler: scheduler}

	require.NoError(t, jobSvc.RunJobNowInline(ctx, "image-polling"))
	require.Equal(t, []string{"image-polling"}, scheduler.busWatcherRuns)
	require.Len(t, scheduler.busWatcherContexts, 1)
	require.NoError(t, scheduler.busWatcherContexts[0].Err())
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

type fakeJobSchedulerInternal struct {
	jobs               map[string]schedulertypes.Job
	runtimeStates      map[string]schedulertypes.JobRuntimeState
	rescheduled        []string
	rescheduleContexts []context.Context
	rescheduleErr      error
	busWatcherRuns     []string
	busWatcherContexts []context.Context
}

func newFakeJobSchedulerInternal(jobIDs ...string) *fakeJobSchedulerInternal {
	jobs := make(map[string]schedulertypes.Job, len(jobIDs))
	for _, jobID := range jobIDs {
		jobs[jobID] = fakeJobInternal{name: jobID}
	}

	return &fakeJobSchedulerInternal{
		jobs:          jobs,
		runtimeStates: make(map[string]schedulertypes.JobRuntimeState),
	}
}

func (s *fakeJobSchedulerInternal) GetJob(jobID string) (schedulertypes.Job, bool) {
	job, ok := s.jobs[jobID]
	return job, ok
}

func (s *fakeJobSchedulerInternal) GetJobRuntimeState(jobID string) (schedulertypes.JobRuntimeState, bool) {
	state, ok := s.runtimeStates[jobID]
	return state, ok
}

func (s *fakeJobSchedulerInternal) RescheduleJob(ctx context.Context, job schedulertypes.Job) error {
	s.rescheduled = append(s.rescheduled, job.Name())
	s.rescheduleContexts = append(s.rescheduleContexts, ctx)
	if s.rescheduleErr != nil {
		return s.rescheduleErr
	}

	s.runtimeStates[job.Name()] = schedulertypes.JobRuntimeState{
		Schedule:  job.Schedule(ctx),
		Scheduled: true,
	}
	return nil
}

func (s *fakeJobSchedulerInternal) RunBusWatcherNow(ctx context.Context, watcherID string) error {
	s.busWatcherRuns = append(s.busWatcherRuns, watcherID)
	s.busWatcherContexts = append(s.busWatcherContexts, ctx)
	return nil
}

type fakeJobInternal struct {
	name string
}

func (j fakeJobInternal) Name() string {
	return j.name
}

func (j fakeJobInternal) Schedule(context.Context) string {
	return "0 0 0 * * *"
}

func (j fakeJobInternal) Run(context.Context) {}
