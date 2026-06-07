package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/types/jobschedule"
	schedulertypes "github.com/getarcaneapp/arcane/types/scheduler"
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

func TestJobService_GetJobSchedules_DefaultDockerClientRefreshInterval(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	cfg := jobSvc.GetJobSchedules(ctx)

	require.Equal(t, "*/30 * * * * *", cfg.DockerClientRefreshInterval)
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

func TestJobService_UpdateJobSchedules_ReschedulesChangedJob(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	scheduler := newFakeJobSchedulerInternal("image-polling", "auto-update")
	jobSvc.SetScheduler(ctx, scheduler)

	nextPollingInterval := "0 */10 * * * *"
	_, err = jobSvc.UpdateJobSchedules(ctx, jobschedule.Update{
		PollingInterval: &nextPollingInterval,
	})
	require.NoError(t, err)

	require.Equal(t, []string{"image-polling"}, scheduler.rescheduled)
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
	scheduler := newFakeJobSchedulerInternal("image-polling")
	jobSvc.SetScheduler(lifecycleCtx, scheduler)

	nextPollingInterval := "0 */10 * * * *"
	_, err = jobSvc.UpdateJobSchedules(requestCtx, jobschedule.Update{
		PollingInterval: &nextPollingInterval,
	})
	require.NoError(t, err)

	cancelRequest()

	require.Len(t, scheduler.rescheduleContexts, 1)
	require.NoError(t, scheduler.rescheduleContexts[0].Err())
	require.Equal(t, true, scheduler.rescheduleContexts[0].Value(lifecycleContextKey{}))
}

func TestJobService_UpdateJobSchedules_SkipsManagerOnlyJobsInAgentMode(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{AgentMode: true})
	scheduler := newFakeJobSchedulerInternal("environment-health")
	jobSvc.SetScheduler(ctx, scheduler)

	nextHealthInterval := "0 */5 * * * *"
	_, err = jobSvc.UpdateJobSchedules(ctx, jobschedule.Update{
		EnvironmentHealthInterval: &nextHealthInterval,
	})
	require.NoError(t, err)

	require.Empty(t, scheduler.rescheduled)
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
	rescheduled        []string
	rescheduleContexts []context.Context
}

func newFakeJobSchedulerInternal(jobIDs ...string) *fakeJobSchedulerInternal {
	jobs := make(map[string]schedulertypes.Job, len(jobIDs))
	for _, jobID := range jobIDs {
		jobs[jobID] = fakeJobInternal{name: jobID}
	}

	return &fakeJobSchedulerInternal{
		jobs: jobs,
	}
}

func (s *fakeJobSchedulerInternal) GetJob(jobID string) (schedulertypes.Job, bool) {
	job, ok := s.jobs[jobID]
	return job, ok
}

func (s *fakeJobSchedulerInternal) RescheduleJob(ctx context.Context, job schedulertypes.Job) error {
	s.rescheduled = append(s.rescheduled, job.Name())
	s.rescheduleContexts = append(s.rescheduleContexts, ctx)
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
