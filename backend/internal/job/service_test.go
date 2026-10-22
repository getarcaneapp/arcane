package job

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/group"
	"github.com/getarcaneapp/arcane/backend/v3/internal/job/children/runtime"
	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	userdomain "github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis/testing"
)

type fakeJobScheduler struct {
	jobs               map[string]types.SchedulerJob
	runtimeStates      map[string]types.JobRuntimeState
	rescheduled        []string
	rescheduleContexts []context.Context
	rescheduleErr      error
	busWatcherRuns     []string
	busWatcherContexts []context.Context
}

type fakeJob struct {
	name string
}

func setupSettingsTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}, &kv.KVEntry{}, &activity.Activity{}, &activity.ActivityMessage{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return &database.DB{DB: db}
}

func newSettingsServiceForTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	service, err := settings.NewSettingsService(ctx, db, &config.Config{}, settings.Env{})
	if err == nil {
		t.Cleanup(func() {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			require.NoError(t, service.Stop(stopCtx))
		})
	}
	return service, err
}

func newJobCoordinatorForTest(t *testing.T, db *database.DB) *runs.Coordinator {
	t.Helper()
	francisRuntime := francistest.New(t)
	coordinator := runs.New(kv.NewKVService(db), francisRuntime.Service(), time.UTC)
	require.NoError(t, coordinator.Register(francisRuntime))
	francistest.Start(t, francisRuntime)
	return coordinator
}

func newJobServiceForTest(t *testing.T, db *database.DB, settingsService *settings.SettingsService, cfg *config.Config) *JobService {
	t.Helper()
	service := NewJobService(
		db,
		settingsService,
		cfg,
		newJobCoordinatorForTest(t, db),
		role.NewRoleService(db),
		environment.NewEnvironmentService(db, http.DefaultClient, nil, nil, settingsService, nil),
		activity.NewActivityService(db, settingsService),
	)
	service.SetScheduler(t.Context(), newFakeJobScheduler())
	return service
}

func TestJobService_GetJobSchedules_DefaultDockerClientRefreshInterval(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	cfg := jobSvc.GetJobSchedules(ctx)

	require.Equal(t, "0 */5 * * * *", cfg.DockerClientRefreshInterval)
	require.Equal(t, "0 0 * * * *", cfg.PollingInterval)
}

func TestJobService_ListJobs_AnalyticsHeartbeatIsManagedInternally(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	analyticsJob := findJobStatusByID(t, jobs.Jobs, "analytics-heartbeat")
	require.Equal(t, "automatic (checked hourly; sent once per 24h)", analyticsJob.Schedule)
	require.Empty(t, analyticsJob.SettingsKey)
	require.Nil(t, analyticsJob.NextRun)
	require.True(t, analyticsJob.CanRunManually)
	require.False(t, analyticsJob.IsContinuous)
}

func TestJobService_ListJobs_IncludesDisabledAutoHealJob(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoHealEnabled", false))

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	autoHealJob := findJobStatusByID(t, jobs.Jobs, "auto-heal")
	require.False(t, autoHealJob.Enabled)
	require.Equal(t, "autoHealInterval", autoHealJob.SettingsKey)
}

func TestJobService_ListJobs_IncludesDockerClientRefreshJob(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	refreshJob := findJobStatusByID(t, jobs.Jobs, "docker-client-refresh")
	require.True(t, refreshJob.Enabled)
	require.True(t, refreshJob.CanRunManually)
	require.Equal(t, "monitoring", refreshJob.Category)
	require.Equal(t, "dockerClientRefreshInterval", refreshJob.SettingsKey)
	require.Equal(t, "0 */5 * * * *", refreshJob.Schedule)
}

func TestJobService_ListJobs_UsesRuntimeScheduleAndNextRun(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	nextRun := time.Date(2026, time.July, 10, 8, 0, 0, 0, time.UTC)
	fakeScheduler := newFakeJobScheduler("auto-update")
	fakeScheduler.runtimeStates["auto-update"] = types.JobRuntimeState{
		Schedule:  "0 0 8 * * *",
		NextRun:   &nextRun,
		Scheduled: true,
	}

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	jobSvc.SetScheduler(ctx, fakeScheduler)
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	autoUpdateJob := findJobStatusByID(t, jobs.Jobs, "auto-update")
	require.Equal(t, "0 0 8 * * *", autoUpdateJob.Schedule)
	require.Equal(t, nextRun, *autoUpdateJob.NextRun)

	now := time.Now().UTC()
	failed := types.SchedulerRun{
		ID:            "failed",
		EnvironmentID: "0",
		Status:        types.RunStatusFailed,
		CreatedAt:     now.Add(-time.Hour),
		UpdatedAt:     now,
		Outcome:       types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "clone failed"},
	}
	succeeded := types.SchedulerRun{
		ID:            "succeeded",
		EnvironmentID: "0",
		Status:        types.RunStatusSucceeded,
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now.Add(-time.Minute),
		FinishedAt:    new(now.Add(-time.Minute)),
	}
	for _, history := range [][]types.SchedulerRun{{failed, succeeded}, {succeeded, failed}} {
		status := runtime.ApplyRunStatus(types.JobStatus{LastRun: new(failed), LastError: failed.Outcome.Message}, history)
		require.Equal(t, succeeded.ID, status.LastRun.ID)
		require.Empty(t, status.LastError)
		require.Equal(t, "clone failed", history[0].Outcome.Message+history[1].Outcome.Message)
	}
	// An acknowledgement updates an old failure's revision, not its execution time.
	acknowledged := types.SchedulerRun{
		ID:            failed.ID,
		EnvironmentID: failed.EnvironmentID,
		Status:        failed.Status,
		CreatedAt:     failed.CreatedAt,
		UpdatedAt:     now.Add(time.Hour),
		Outcome:       failed.Outcome,
	}
	status := runtime.ApplyRunStatus(types.JobStatus{LastRun: new(succeeded)}, []types.SchedulerRun{acknowledged})
	require.Equal(t, succeeded.ID, status.LastRun.ID)
	require.Empty(t, status.LastError)
	// A real retry of the older run becomes the latest execution.
	retried := types.SchedulerRun{
		ID:            acknowledged.ID,
		EnvironmentID: acknowledged.EnvironmentID,
		Status:        acknowledged.Status,
		CreatedAt:     acknowledged.CreatedAt,
		UpdatedAt:     acknowledged.UpdatedAt,
		StartedAt:     new(now.Add(time.Second)),
		Outcome:       acknowledged.Outcome,
		Attempts:      []types.SchedulerAttempt{{Number: 2, StartedAt: now.Add(time.Second)}},
	}
	status = runtime.ApplyRunStatus(status, []types.SchedulerRun{retried})
	require.Equal(t, retried.ID, status.LastRun.ID)
	require.Equal(t, "clone failed", status.LastError)
	// Merge the same run's terminal revision before choosing an active run.
	status = runtime.ApplyRunStatus(
		types.JobStatus{
			CurrentRun: &types.SchedulerRun{
				ID:            succeeded.ID,
				EnvironmentID: succeeded.EnvironmentID,
				Status:        types.RunStatusRunning,
				CreatedAt:     succeeded.CreatedAt,
				UpdatedAt:     succeeded.UpdatedAt,
				FinishedAt:    succeeded.FinishedAt,
			},
			LastRun:     status.LastRun,
			LastSuccess: status.LastSuccess,
			LastError:   status.LastError,
		},
		[]types.SchedulerRun{{
			ID:            succeeded.ID,
			EnvironmentID: succeeded.EnvironmentID,
			Status:        succeeded.Status,
			CreatedAt:     succeeded.CreatedAt,
			UpdatedAt:     now.Add(2 * time.Second),
			FinishedAt:    succeeded.FinishedAt,
		}},
	)
	require.Nil(t, status.CurrentRun)
	running := types.SchedulerRun{
		ID:            retried.ID,
		EnvironmentID: retried.EnvironmentID,
		Status:        types.RunStatusRunning,
		CreatedAt:     retried.CreatedAt,
		UpdatedAt:     now.Add(2 * time.Hour),
		StartedAt:     retried.StartedAt,
		Outcome:       retried.Outcome,
		Attempts:      retried.Attempts,
	}
	status = runtime.ApplyRunStatus(status, []types.SchedulerRun{running})
	require.Equal(t, running.ID, status.CurrentRun.ID)
	require.Empty(t, status.LastError)

	// A later manual failure stays visible while older work waits to retry.
	retrying := types.SchedulerRun{
		ID:            running.ID,
		EnvironmentID: running.EnvironmentID,
		Status:        types.RunStatusRetrying,
		CreatedAt:     running.CreatedAt,
		UpdatedAt:     running.UpdatedAt,
		StartedAt:     running.StartedAt,
		NextAttempt:   new(now.Add(time.Hour)),
		Outcome:       running.Outcome,
		Attempts:      running.Attempts,
	}
	manualFailure := types.SchedulerRun{
		ID:            "manual-failure",
		EnvironmentID: running.EnvironmentID,
		Status:        types.RunStatusFailed,
		CreatedAt:     now.Add(time.Minute),
		UpdatedAt:     running.UpdatedAt,
		Outcome:       types.SchedulerOutcome{Status: running.Outcome.Status, Message: "latest manual failure"},
	}
	laterSuccess := types.SchedulerRun{
		ID:            succeeded.ID,
		EnvironmentID: succeeded.EnvironmentID,
		Status:        succeeded.Status,
		CreatedAt:     now.Add(2 * time.Minute),
		UpdatedAt:     now.Add(2 * time.Second),
		FinishedAt:    succeeded.FinishedAt,
	}
	for _, history := range [][]types.SchedulerRun{{retrying, manualFailure}, {manualFailure, retrying}} {
		status = runtime.ApplyRunStatus(types.JobStatus{}, history)
		require.Equal(t, retrying.ID, status.CurrentRun.ID)
		require.Equal(t, manualFailure.ID, status.LastRun.ID)
		require.Equal(t, manualFailure.Outcome.Message, status.LastError)
		status = runtime.ApplyRunStatus(status, []types.SchedulerRun{laterSuccess})
		require.Empty(t, status.LastError)
	}

	cloneErr := errors.New("failed to clone repository: network is unreachable")
	outcome, runErr := classifyOutcome("gitops-sync:project", types.SchedulerOutcome{}, cloneErr)
	require.ErrorIs(t, runErr, cloneErr)
	require.Equal(t, types.RunStatusFailed, outcome.Status)
	outcome, runErr = classifyOutcome("environment-health:0", types.SchedulerOutcome{}, context.DeadlineExceeded)
	require.ErrorIs(t, runErr, context.DeadlineExceeded)
	require.Equal(t, types.RunStatusRetrying, outcome.Status)
	outcome, runErr = classifyOutcome("auto-update", types.SchedulerOutcome{Status: types.RunStatusPartial}, cloneErr)
	require.ErrorIs(t, runErr, cloneErr)
	require.Equal(t, types.RunStatusPartial, outcome.Status)
}

func TestJobService_ListJobs_ImageUpdateWatcherIsContinuousAndRespectsEnabled(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "pollingEnabled", false))

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	jobs, err := jobSvc.ListJobs(ctx)
	require.NoError(t, err)

	watcher := findJobStatusByID(t, jobs.Jobs, "image-polling")
	require.Equal(t, "Image Update Watcher", watcher.Name)
	require.Equal(t, "0 0 * * * *", watcher.Schedule)
	require.Equal(t, "pollingInterval", watcher.SettingsKey)
	require.NotNil(t, watcher.NextRun)
	require.True(t, watcher.IsContinuous)
	require.True(t, watcher.CanRunManually)
	require.False(t, watcher.Enabled)
}

func TestJobService_UpdateJobSchedules_ReschedulesChangedJob(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	fakeScheduler := newFakeJobScheduler("auto-update")
	jobSvc.SetScheduler(ctx, fakeScheduler)

	_, err = jobSvc.UpdateJobSchedules(ctx, types.JobScheduleConfig{
		AutoUpdateInterval: "0 */10 * * * *",
	}, map[string]bool{"autoUpdateInterval": true})
	require.NoError(t, err)

	require.Equal(t, []string{"auto-update"}, fakeScheduler.rescheduled)
}

func TestJobService_UpdateJobSchedules_DeprecatedPollingIntervalDoesNotReschedule(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	fakeScheduler := newFakeJobScheduler("auto-update")
	jobSvc.SetScheduler(ctx, fakeScheduler)

	updated, err := jobSvc.UpdateJobSchedules(ctx, types.JobScheduleConfig{
		PollingInterval: "0 */10 * * * *",
	}, map[string]bool{"pollingInterval": true})
	require.NoError(t, err)
	require.Equal(t, "0 */10 * * * *", updated.PollingInterval)
	require.Empty(t, fakeScheduler.rescheduled)
}

func TestJobService_UpdateJobSchedules_UsesLifecycleContextForReschedule(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	type lifecycleContextKey struct{}
	lifecycleCtx := context.WithValue(t.Context(), lifecycleContextKey{}, true)
	requestCtx, cancelRequest := context.WithCancel(t.Context())

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	fakeScheduler := newFakeJobScheduler("auto-update")
	jobSvc.SetScheduler(lifecycleCtx, fakeScheduler)

	_, err = jobSvc.UpdateJobSchedules(requestCtx, types.JobScheduleConfig{
		AutoUpdateInterval: "0 */10 * * * *",
	}, map[string]bool{"autoUpdateInterval": true})
	require.NoError(t, err)

	cancelRequest()

	require.Len(t, fakeScheduler.rescheduleContexts, 1)
	require.NoError(t, fakeScheduler.rescheduleContexts[0].Err())
	require.Equal(t, true, fakeScheduler.rescheduleContexts[0].Value(lifecycleContextKey{}))
}

func TestJobService_UpdateJobSchedules_RejectsInvalidCronWithoutChangingSetting(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.LoadDatabaseSettings(ctx))

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	fakeScheduler := newFakeJobScheduler()
	jobSvc.SetScheduler(ctx, fakeScheduler)

	_, err = jobSvc.UpdateJobSchedules(ctx, types.JobScheduleConfig{
		PollingInterval: "not a cron expression",
	}, map[string]bool{"pollingInterval": true})
	require.ErrorContains(t, err, "invalid cron expression for pollingInterval")
	require.Equal(t, "0 0 * * * *", settingsSvc.GetStringSetting(ctx, "pollingInterval"))
	require.Empty(t, fakeScheduler.rescheduled)
}

func TestJobService_UpdateJobSchedules_UnchangedScheduleDoesNotReschedule(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	updated, err := jobSvc.UpdateJobSchedules(ctx, types.JobScheduleConfig{
		PollingInterval: "0 0 * * * *",
	}, map[string]bool{"pollingInterval": true})
	require.NoError(t, err)
	require.Equal(t, "0 0 * * * *", updated.PollingInterval)
}

func TestJobService_UpdateJobSchedules_RestoresPreviousScheduleWhenRescheduleFails(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.LoadDatabaseSettings(ctx))

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	fakeScheduler := &fakeJobScheduler{
		jobs:          map[string]types.SchedulerJob{"auto-update": fakeJob{name: "auto-update"}},
		runtimeStates: map[string]types.JobRuntimeState{},
		rescheduleErr: errors.New("scheduler unavailable"),
	}
	jobSvc.SetScheduler(ctx, fakeScheduler)

	_, err = jobSvc.UpdateJobSchedules(ctx, types.JobScheduleConfig{
		AutoUpdateInterval: "0 0 8 * * *",
	}, map[string]bool{"autoUpdateInterval": true})
	require.ErrorContains(t, err, "scheduler unavailable")
	require.Equal(t, "0 0 0 * * *", settingsSvc.GetStringSetting(ctx, "autoUpdateInterval"))

	var persisted settings.SettingVariable
	require.NoError(t, db.WithContext(ctx).First(&persisted, "key = ?", "autoUpdateInterval").Error)
	require.Equal(t, "0 0 0 * * *", persisted.Value)
}

func TestJobService_UpdateJobSchedules_SkipsManagerOnlyJobsInAgentMode(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{AgentMode: true})
	fakeScheduler := newFakeJobScheduler("environment-health")
	jobSvc.SetScheduler(ctx, fakeScheduler)

	_, err = jobSvc.UpdateJobSchedules(ctx, types.JobScheduleConfig{
		EnvironmentHealthInterval: "0 */5 * * * *",
	}, map[string]bool{"environmentHealthInterval": true})
	require.NoError(t, err)

	require.Empty(t, fakeScheduler.rescheduled)
}

func TestJobService_UpdateJobSchedules_DelegatesEnvironmentHealthReschedule(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)

	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	fakeScheduler := newFakeJobScheduler()
	jobSvc.SetScheduler(ctx, fakeScheduler)

	rescheduled := 0
	jobSvc.OnEnvironmentHealthReschedule = func(context.Context) { // keep: bootstrap wires this callback after construction
		rescheduled++
	}

	_, err = jobSvc.UpdateJobSchedules(ctx, types.JobScheduleConfig{
		EnvironmentHealthInterval: "0 */5 * * * *",
	}, map[string]bool{"environmentHealthInterval": true})
	require.NoError(t, err)
	require.Equal(t, 1, rescheduled)
	require.Empty(t, fakeScheduler.rescheduled)
}

func TestJobService_Submit_PersistsImageUpdateWatcherRun(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	settingsSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	fakeScheduler := newFakeJobScheduler()
	jobSvc := newJobServiceForTest(t, db, settingsSvc, &config.Config{})
	jobSvc.SetScheduler(ctx, fakeScheduler)
	require.NoError(t, db.AutoMigrate(&userdomain.User{}, &role.Role{}, &role.RoleBinding{}, &group.Group{}, &group.GroupMember{}, &audit.AuditLog{}))
	require.NoError(t, db.Create(&userdomain.User{Username: "operator", BaseModel: database.BaseModel{ID: "operator"}}).Error)
	require.NoError(t, db.Create(&role.Role{
		ID:          "jobs-manager",
		Name:        "Jobs manager",
		Permissions: database.StringSlice{authz.PermJobsManage},
	}).Error)
	require.NoError(t, db.Create(&role.RoleBinding{UserID: new("operator"), RoleID: "jobs-manager", Source: role.BindingSourceManual}).Error)
	run, err := jobSvc.Submit(ctx, types.SchedulerRequest{
		JobID:         "image-polling",
		EnvironmentID: "0",
		Trigger:       "manual",
		RequestedBy:   "operator",
	})
	require.NoError(t, err)
	require.Equal(t, types.RunStatusQueued, run.Status)
	persisted, err := jobSvc.runs.Get(ctx, "0", "image-polling", run.ID)
	require.NoError(t, err)
	require.Equal(t, run.ID, persisted.ID)
	require.Empty(t, fakeScheduler.busWatcherRuns)
}

func findJobStatusByID(t *testing.T, jobs []types.JobStatus, id string) types.JobStatus {
	t.Helper()

	for _, job := range jobs {
		if job.ID == id {
			return job
		}
	}
	require.FailNowf(t, "unexpected failure", "job %q not found", id)
	return types.JobStatus{}
}

func newFakeJobScheduler(jobIDs ...string) *fakeJobScheduler {
	jobs := make(map[string]types.SchedulerJob, len(jobIDs))
	for _, jobID := range jobIDs {
		jobs[jobID] = fakeJob{name: jobID}
	}

	return &fakeJobScheduler{
		jobs:          jobs,
		runtimeStates: make(map[string]types.JobRuntimeState),
	}
}

func (s *fakeJobScheduler) GetJob(jobID string) (types.SchedulerJob, bool) {
	job, ok := s.jobs[jobID]
	return job, ok
}

func (s *fakeJobScheduler) GetJobRuntimeState(jobID string) (types.JobRuntimeState, bool) {
	state, ok := s.runtimeStates[jobID]
	return state, ok
}

func (s *fakeJobScheduler) RescheduleJob(ctx context.Context, job types.SchedulerJob) error {
	s.rescheduled = append(s.rescheduled, job.Name())
	s.rescheduleContexts = append(s.rescheduleContexts, ctx)
	if s.rescheduleErr != nil {
		return s.rescheduleErr
	}

	s.runtimeStates[job.Name()] = types.JobRuntimeState{
		Schedule:  job.Schedule(ctx),
		Scheduled: true,
	}
	return nil
}

func (s *fakeJobScheduler) RunBusWatcherNow(ctx context.Context, watcherID string) error {
	s.busWatcherRuns = append(s.busWatcherRuns, watcherID)
	s.busWatcherContexts = append(s.busWatcherContexts, ctx)
	return nil
}

func (j fakeJob) Name() string {
	return j.name
}

func (j fakeJob) Schedule(context.Context) string {
	return "0 0 0 * * *"
}

func (j fakeJob) Run(context.Context) (types.SchedulerOutcome, error) {
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}

func newJobActivityTestService(t *testing.T) (*JobService, *activity.ActivityService, *database.DB) {
	t.Helper()
	db := setupSettingsTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	settingsService, err := newSettingsServiceForTest(t, t.Context(), db)
	require.NoError(t, err)
	activities := activity.NewActivityService(db, nil)
	svc := NewJobService(
		db,
		settingsService,
		&config.Config{},
		newJobCoordinatorForTest(t, db),
		role.NewRoleService(db),
		environment.NewEnvironmentService(db, http.DefaultClient, nil, nil, settingsService, nil),
		activities,
	)
	svc.SetScheduler(t.Context(), newFakeJobScheduler())
	return svc, activities, db
}

func TestJobActivityLifecycle(t *testing.T) {
	svc, activities, db := newJobActivityTestService(t)
	ctx := t.Context()
	run, err := svc.runs.Submit(ctx, types.SchedulerRequest{
		JobID:   "auto-update",
		RunID:   "b6d679c2-bdf5-4af1-985f-49c468983ba0",
		Trigger: "scheduled",
	})
	require.NoError(t, err)
	activityID, err := uuid.Parse(run.ActivityID)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil(), activityID)
	require.Equal(t, "0", run.ActivityEnvironmentID)
	check := func(status types.ActivityStatus, precise types.RunStatus) {
		t.Helper()
		detail, detailErr := activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
		require.NoError(t, detailErr)
		require.Equal(t, status, detail.Activity.Status)
		require.Equal(t, string(precise), detail.Activity.Metadata["jobStatus"])
		require.Equal(t, run.ID, *detail.Activity.BatchID)
	}
	check(types.ActivityStatusQueued, types.RunStatusQueued)
	duplicate, err := svc.runs.Submit(ctx, types.SchedulerRequest{
		JobID:   run.JobID,
		RunID:   run.ID,
		Trigger: "scheduled",
	})
	require.NoError(t, err)
	require.Equal(t, run.ActivityID, duplicate.ActivityID)
	for _, status := range []types.RunStatus{types.RunStatusRunning, types.RunStatusWaiting, types.RunStatusFailed} {
		require.NoError(t, svc.runs.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
			*current = types.SchedulerRun{ //exhaustruct:enforce
				ActivityEnvironmentID: current.ActivityEnvironmentID,
				ActivityID:            current.ActivityID,
				RequestedWithKey:      current.RequestedWithKey,
				ID:                    current.ID,
				JobID:                 current.JobID,
				EnvironmentID:         current.EnvironmentID,
				Trigger:               current.Trigger,
				RequestedBy:           current.RequestedBy,
				Status:                status,
				CreatedAt:             current.CreatedAt,
				UpdatedAt:             time.Now().UTC(),
				StartedAt:             current.StartedAt,
				FinishedAt:            current.FinishedAt,
				NextAttempt:           current.NextAttempt,
				AttemptCount:          current.AttemptCount,
				Owner:                 current.Owner,
				Outcome: types.SchedulerOutcome{ //exhaustruct:enforce
					Status:     status,
					Message:    "operation detail",
					ActivityID: "",
					Targets:    nil,
					Steps:      nil,
				},
				Attempts:                current.Attempts,
				RemoteDeliveryAttempted: current.RemoteDeliveryAttempted,
				RemoteOutcome:           current.RemoteOutcome,
				RemoteRetryRequested:    current.RemoteRetryRequested,
				RemoteRetryAttempted:    current.RemoteRetryAttempted,
				RemoteAttemptCount:      current.RemoteAttemptCount,
				RemoteAccepted:          current.RemoteAccepted,
				RemoteSettled:           current.RemoteSettled,
				LastConfirmedAt:         current.LastConfirmedAt,
			}
			return nil
		}))
	}
	check(types.ActivityStatusFailed, types.RunStatusFailed)
	failedDetail, err := activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
	require.NoError(t, err)
	require.NotNil(t, failedDetail.Activity.Error)
	require.Equal(t, "operation detail", *failedDetail.Activity.Error)
	later, err := svc.runs.Submit(ctx, types.SchedulerRequest{JobID: run.JobID, Trigger: "scheduled"})
	require.NoError(t, err)
	require.NoError(t, svc.runs.UpdateRun(ctx, later, func(current *types.SchedulerRun) error {
		*current = types.SchedulerRun{ //exhaustruct:enforce
			ActivityEnvironmentID:   current.ActivityEnvironmentID,
			ActivityID:              current.ActivityID,
			RequestedWithKey:        current.RequestedWithKey,
			ID:                      current.ID,
			JobID:                   current.JobID,
			EnvironmentID:           current.EnvironmentID,
			Trigger:                 current.Trigger,
			RequestedBy:             current.RequestedBy,
			Status:                  types.RunStatusSucceeded,
			CreatedAt:               current.CreatedAt,
			UpdatedAt:               time.Now().UTC(),
			StartedAt:               current.StartedAt,
			FinishedAt:              current.FinishedAt,
			NextAttempt:             current.NextAttempt,
			AttemptCount:            current.AttemptCount,
			Owner:                   current.Owner,
			Outcome:                 current.Outcome,
			Attempts:                current.Attempts,
			RemoteDeliveryAttempted: current.RemoteDeliveryAttempted,
			RemoteOutcome:           current.RemoteOutcome,
			RemoteRetryRequested:    current.RemoteRetryRequested,
			RemoteRetryAttempted:    current.RemoteRetryAttempted,
			RemoteAttemptCount:      current.RemoteAttemptCount,
			RemoteAccepted:          current.RemoteAccepted,
			RemoteSettled:           current.RemoteSettled,
			LastConfirmedAt:         current.LastConfirmedAt,
		}
		return nil
	}))
	failedDetail, err = activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
	require.NoError(t, err)
	require.Equal(t, types.ActivityStatusFailed, failedDetail.Activity.Status)
	require.Equal(t, "operation detail", *failedDetail.Activity.Error)
	_, err = svc.runs.Retry(ctx, "0", run.JobID, run.ID)
	require.NoError(t, err)
	check(types.ActivityStatusQueued, types.RunStatusQueued)
	_, err = activities.CancelActivity(ctx, "0", run.ActivityID, "operator")
	require.ErrorIs(t, err, activity.ErrActivityNotCancelable)
	check(types.ActivityStatusQueued, types.RunStatusQueued)
	var count int64
	require.NoError(t, db.Model(&activity.Activity{}).Count(&count).Error)
	require.Equal(t, int64(2), count)
}

func TestJobActivityVisibilityAndGrouping(t *testing.T) {
	svc, activities, _ := newJobActivityTestService(t)
	for _, test := range []struct {
		job, trigger, environment string
		visible                   bool
	}{
		{
			job:         "auto-update",
			trigger:     "scheduled",
			environment: "0",
			visible:     true,
		},
		{
			job:         "gitops-sync:project",
			trigger:     "scheduled",
			environment: "0",
			visible:     true,
		},
		{
			job:         "volume-backup:volume",
			trigger:     "recovery",
			environment: "0",
			visible:     true,
		},
		{
			job:         "system-backup:system",
			trigger:     "scheduled",
			environment: "0",
			visible:     true,
		},
		{
			job:         "image-polling",
			trigger:     "manual",
			environment: "0",
			visible:     true,
		},
		{
			job:         "auto-update",
			trigger:     "manual",
			environment: "remote",
			visible:     true,
		},
		{
			job:         "auto-update",
			trigger:     "remote",
			environment: "0",
			visible:     false,
		},
		{
			job:         "image-polling",
			trigger:     "scheduled",
			environment: "0",
			visible:     false,
		},
		{
			job:         "environment-health:0",
			trigger:     "scheduled",
			environment: "0",
			visible:     false,
		},
		{
			job:         "activity-sweep",
			trigger:     "scheduled",
			environment: "0",
			visible:     false,
		},
	} {
		t.Run(test.job+"/"+test.trigger+"/"+test.environment, func(t *testing.T) {
			run, err := svc.runs.Submit(t.Context(), types.SchedulerRequest{
				JobID:         test.job,
				Trigger:       test.trigger,
				EnvironmentID: test.environment,
			})
			require.NoError(t, err)
			require.Equal(t, test.visible, run.ActivityID != "")
			if test.visible {
				require.Equal(t, test.environment, run.ActivityEnvironmentID)
				detail, getActivityDetailErr := activities.GetActivityDetail(t.Context(), test.environment, run.ActivityID, 10)
				require.NoError(t, getActivityDetailErr)
				require.Equal(t, test.environment, detail.Activity.Metadata["environmentId"])
			}
			child, err := activities.StartActivity(svc.runs.ExecutionContext(t.Context(), run, ""), activity.StartActivityRequest{Type: types.ActivityTypeImagePull})
			require.NoError(t, err)
			require.Equal(t, run.ID, *child.BatchID)
		})
	}
}

func TestJobActivityRestartRepairsFailedProjection(t *testing.T) {
	svc, activities, db := newJobActivityTestService(t)
	ctx := t.Context()
	require.NoError(t, db.Migrator().DropTable(&activity.ActivityMessage{}, &activity.Activity{}))
	run, err := svc.runs.Submit(ctx, types.SchedulerRequest{JobID: "auto-update", Trigger: "scheduled"})
	require.NoError(t, err, "activity failure must not reject accepted work")
	require.NotEmpty(t, run.ActivityID)
	require.NoError(t, svc.runs.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
		*current = types.SchedulerRun{ //exhaustruct:enforce
			ActivityEnvironmentID: current.ActivityEnvironmentID,
			ActivityID:            current.ActivityID,
			RequestedWithKey:      current.RequestedWithKey,
			ID:                    current.ID,
			JobID:                 current.JobID,
			EnvironmentID:         current.EnvironmentID,
			Trigger:               current.Trigger,
			RequestedBy:           current.RequestedBy,
			Status:                types.RunStatusNeedsAttention,
			CreatedAt:             current.CreatedAt,
			UpdatedAt:             time.Now().UTC(),
			StartedAt:             current.StartedAt,
			FinishedAt:            current.FinishedAt,
			NextAttempt:           current.NextAttempt,
			AttemptCount:          current.AttemptCount,
			Owner:                 current.Owner,
			Outcome: types.SchedulerOutcome{ //exhaustruct:enforce
				Status:     "",
				Message:    "interrupted before applying updates",
				ActivityID: "",
				Targets:    nil,
				Steps:      nil,
			},
			Attempts:                current.Attempts,
			RemoteDeliveryAttempted: current.RemoteDeliveryAttempted,
			RemoteOutcome:           current.RemoteOutcome,
			RemoteRetryRequested:    current.RemoteRetryRequested,
			RemoteRetryAttempted:    current.RemoteRetryAttempted,
			RemoteAttemptCount:      current.RemoteAttemptCount,
			RemoteAccepted:          current.RemoteAccepted,
			RemoteSettled:           current.RemoteSettled,
			LastConfirmedAt:         current.LastConfirmedAt,
		}
		return nil
	}))
	require.NoError(t, db.AutoMigrate(&activity.Activity{}, &activity.ActivityMessage{}))
	restarted := NewJobService(
		db,
		svc.settings,
		&config.Config{},
		svc.runs,
		role.NewRoleService(db),
		environment.NewEnvironmentService(db, http.DefaultClient, nil, nil, svc.settings, nil),
		activities,
	)
	restarted.SetScheduler(ctx, newFakeJobScheduler())
	require.NoError(t, restarted.runs.Start(ctx, ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		require.NoError(t, restarted.runs.Stop(stopCtx))
	})
	require.Eventually(t, func() bool {
		detail, getErr := activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
		return getErr == nil && detail.Activity.Status == types.ActivityStatusFailed
	}, 5*time.Second, 10*time.Millisecond)
	detail, err := activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
	require.NoError(t, err)
	require.Equal(t, types.ActivityStatusFailed, detail.Activity.Status)
	require.Contains(t, detail.Activity.LatestMessage, "interrupted")
	persisted, err := restarted.runs.Get(ctx, "0", run.JobID, run.ID)
	require.NoError(t, err)
	require.Zero(t, persisted.AttemptCount, "repair must not execute interrupted work")
	require.Equal(t, types.RunStatusFailed, persisted.Status)
	require.Equal(t, "interrupted before applying updates", persisted.Outcome.Message)
	require.NotNil(t, persisted.FinishedAt)
	require.Equal(t, run.ActivityID, persisted.ActivityID)
}
