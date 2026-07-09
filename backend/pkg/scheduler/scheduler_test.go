package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
)

type testSchedulerJob struct {
	name     string
	schedule string
	run      func(context.Context)
}

func (j *testSchedulerJob) Name() string { return j.name }

func (j *testSchedulerJob) Schedule(context.Context) string { return j.schedule }

func (j *testSchedulerJob) Run(ctx context.Context) {
	if j.run != nil {
		j.run(ctx)
	}
}

type conditionalTestSchedulerJob struct {
	*testSchedulerJob
	shouldSchedule func(context.Context) bool
}

func (j *conditionalTestSchedulerJob) ShouldSchedule(ctx context.Context) bool {
	if j.shouldSchedule == nil {
		return true
	}

	return j.shouldSchedule(ctx)
}

func TestJobScheduler_StartScheduler_SkipsDisabledConditionalJobs(t *testing.T) {
	js := NewJobScheduler(context.Background(), nil)

	job := &conditionalTestSchedulerJob{
		testSchedulerJob: &testSchedulerJob{
			name:     "test-disabled-startup",
			schedule: "*/1 * * * * *",
		},
		shouldSchedule: func(context.Context) bool { return false },
	}

	js.RegisterJob(job)
	js.StartScheduler()
	defer js.cron.Stop()

	require.NotContains(t, js.entryIDs, job.Name())
	require.Empty(t, js.cron.Entries())
}

func TestJobScheduler_RescheduleJob_RemovesEntryWhenDisabled(t *testing.T) {
	js := NewJobScheduler(context.Background(), nil)
	enabled := true

	job := &conditionalTestSchedulerJob{
		testSchedulerJob: &testSchedulerJob{
			name:     "test-disabled-reschedule",
			schedule: "*/1 * * * * *",
		},
		shouldSchedule: func(context.Context) bool { return enabled },
	}

	require.NoError(t, js.RescheduleJob(context.Background(), job))
	require.Contains(t, js.entryIDs, job.Name())

	enabled = false

	require.NoError(t, js.RescheduleJob(context.Background(), job))
	require.NotContains(t, js.entryIDs, job.Name())
	require.Empty(t, js.cron.Entries())
}

func TestJobScheduler_RescheduleJob_AddsEntryWhenEnabled(t *testing.T) {
	js := NewJobScheduler(context.Background(), nil)
	enabled := false

	job := &conditionalTestSchedulerJob{
		testSchedulerJob: &testSchedulerJob{
			name:     "test-enabled-reschedule",
			schedule: "*/1 * * * * *",
		},
		shouldSchedule: func(context.Context) bool { return enabled },
	}

	require.NoError(t, js.RescheduleJob(context.Background(), job))
	require.NotContains(t, js.entryIDs, job.Name())

	enabled = true

	require.NoError(t, js.RescheduleJob(context.Background(), job))
	require.Contains(t, js.entryIDs, job.Name())
	require.Len(t, js.cron.Entries(), 1)
}

func TestJobScheduler_StartScheduler_SchedulesNonConditionalJobs(t *testing.T) {
	js := NewJobScheduler(context.Background(), nil)

	job := &testSchedulerJob{
		name:     "test-non-conditional-startup",
		schedule: "*/1 * * * * *",
	}

	js.RegisterJob(job)
	js.StartScheduler()
	defer js.cron.Stop()

	require.Contains(t, js.entryIDs, job.Name())
	require.Len(t, js.cron.Entries(), 1)
}

func TestJobScheduler_RescheduleJob_UsesProvidedContext(t *testing.T) {
	js := NewJobScheduler(context.Background(), nil)

	var once sync.Once
	runErrCh := make(chan error, 1)
	runCtx := t.Context()

	job := &testSchedulerJob{
		name:     "test-reschedule-provided-context",
		schedule: "*/1 * * * * *",
		run: func(ctx context.Context) {
			once.Do(func() { runErrCh <- ctx.Err() })
		},
	}

	require.NoError(t, js.RescheduleJob(runCtx, job))
	js.cron.Start()
	defer js.cron.Stop()

	select {
	case err := <-runErrCh:
		require.NoError(t, err)
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("timed out waiting for scheduled run")
	}
}

func TestJobScheduler_RescheduleJob_UsesLifecycleContextForShutdown(t *testing.T) {
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	js := NewJobScheduler(lifecycleCtx, nil)

	startedCh := make(chan struct{}, 1)
	stoppedCh := make(chan struct{}, 1)
	job := &testSchedulerJob{
		name:     "test-reschedule-lifecycle-shutdown",
		schedule: "*/1 * * * * *",
		run: func(ctx context.Context) {
			select {
			case startedCh <- struct{}{}:
			default:
			}
			<-ctx.Done()
			select {
			case stoppedCh <- struct{}{}:
			default:
			}
		},
	}

	require.NoError(t, js.RescheduleJob(lifecycleCtx, job))
	js.cron.Start()
	defer js.cron.Stop()

	select {
	case <-startedCh:
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("timed out waiting for scheduled run")
	}

	cancelLifecycle()

	select {
	case <-stoppedCh:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("scheduled job did not observe lifecycle cancellation")
	}
}

func TestJobScheduler_AddJob_UpsertReplacesEntryWithoutLeaking(t *testing.T) {
	js := NewJobScheduler(context.Background(), nil)

	job := &testSchedulerJob{name: "dyn-upsert", schedule: "*/5 * * * * *"}
	require.NoError(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Len(t, js.cron.Entries(), 1)
	firstEntry := js.entryIDs[job.Name()]

	// Re-adding with a changed schedule (e.g. a new sync interval) must replace the
	// existing cron entry, not leak a second one that keeps firing forever.
	job.schedule = "*/10 * * * * *"
	require.NoError(t, js.AddJob(context.Background(), job))
	require.Len(t, js.cron.Entries(), 1)
	require.NotEqual(t, firstEntry, js.entryIDs[job.Name()])

	state, ok := js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.True(t, state.Scheduled)
	require.Equal(t, "*/10 * * * * *", state.Schedule)
	require.NotNil(t, state.NextRun)
}

func TestJobScheduler_AddJob_InvalidRescheduleKeepsExistingEntry(t *testing.T) {
	js := NewJobScheduler(context.Background(), nil)

	job := &testSchedulerJob{name: "dyn-invalid-reschedule", schedule: "*/5 * * * * *"}
	require.NoError(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Len(t, js.cron.Entries(), 1)
	firstEntry := js.entryIDs[job.Name()]

	job.schedule = "not a cron schedule"
	require.Error(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Equal(t, firstEntry, js.entryIDs[job.Name()])
	require.Len(t, js.cron.Entries(), 1)

	state, ok := js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.True(t, state.Scheduled)
	require.Equal(t, "*/5 * * * * *", state.Schedule)
}

func TestJobScheduler_RemoveJob_RemovesEntryAndIsNoopWhenAbsent(t *testing.T) {
	js := NewJobScheduler(context.Background(), nil)

	// Removing an unknown job must be a safe no-op (e.g. deleting a sync that never
	// had auto-sync enabled).
	js.RemoveJob(context.Background(), "never-registered")

	job := &testSchedulerJob{name: "dyn-remove", schedule: "*/5 * * * * *"}
	require.NoError(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Len(t, js.cron.Entries(), 1)

	js.RemoveJob(context.Background(), job.Name())
	require.False(t, js.HasJob(job.Name()))
	require.NotContains(t, js.entryIDs, job.Name())
	require.Empty(t, js.cron.Entries())
}

func TestJobScheduler_AddJob_GenericJobWithoutShouldRunIsScheduled(t *testing.T) {
	js := NewJobScheduler(context.Background(), nil)

	job := &schedulertypes.GenericJob{
		JobName:    "generic-dyn",
		ScheduleFn: func(context.Context) string { return "@every 1m" },
		RunFn:      func(context.Context) {},
	}
	require.NoError(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Len(t, js.cron.Entries(), 1)
}
