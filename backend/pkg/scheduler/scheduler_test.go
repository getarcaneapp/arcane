package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"emperror.dev/errors"
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

type testBusWatcherInternal struct {
	name    string
	started chan struct{}
	stopped chan struct{}
	ranNow  chan context.Context
}

type orderedStopBusWatcherInternal struct {
	name         string
	started      chan struct{}
	runnerExited chan struct{}
}

func (w *orderedStopBusWatcherInternal) Name() string {
	return w.name
}

func (w *orderedStopBusWatcherInternal) Start(ctx context.Context) error {
	close(w.started)
	<-ctx.Done()
	close(w.runnerExited)
	return nil
}

func (*orderedStopBusWatcherInternal) RunNow(context.Context) error {
	return nil
}

func (w *orderedStopBusWatcherInternal) Stop(context.Context) error {
	select {
	case <-w.runnerExited:
		return nil
	default:
		return errors.New("watcher stopped before its runner exited")
	}
}

func (w *testBusWatcherInternal) RunNow(ctx context.Context) error {
	if w.ranNow != nil {
		w.ranNow <- ctx
	}
	return nil
}

func (w *testBusWatcherInternal) Name() string { return w.name }

func (w *testBusWatcherInternal) Start(ctx context.Context) error {
	close(w.started)
	<-ctx.Done()
	close(w.stopped)
	return nil
}

func (j *conditionalTestSchedulerJob) ShouldSchedule(ctx context.Context) bool {
	if j.shouldSchedule == nil {
		return true
	}

	return j.shouldSchedule(ctx)
}

func TestJobScheduler_StartScheduler_SkipsDisabledConditionalJobs(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)

	job := &conditionalTestSchedulerJob{
		testSchedulerJob: &testSchedulerJob{
			name:     "test-disabled-startup",
			schedule: "*/1 * * * * *",
		},
		shouldSchedule: func(context.Context) bool { return false },
	}

	require.NoError(t, js.RegisterJob(job))
	require.NoError(t, js.StartScheduler())
	defer js.cron.Stop()

	state, ok := js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.False(t, state.Scheduled)
	require.Empty(t, js.cron.Entries())
}

func TestJobScheduler_StartScheduler_ContinuesAfterInvalidJobSchedule(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)
	invalid := &testSchedulerJob{name: "invalid-startup-job", schedule: "not a cron schedule"}
	valid := &testSchedulerJob{name: "valid-startup-job", schedule: "*/1 * * * * *"}

	require.NoError(t, js.RegisterJob(invalid))
	require.NoError(t, js.RegisterJob(valid))
	require.NoError(t, js.StartScheduler())

	invalidState, ok := js.GetJobRuntimeState(invalid.Name())
	require.True(t, ok)
	require.False(t, invalidState.Scheduled)
	validState, ok := js.GetJobRuntimeState(valid.Name())
	require.True(t, ok)
	require.True(t, validState.Scheduled)
	require.Len(t, js.cron.Entries(), 1)
}

func TestJobScheduler_StopWaitsForBusWatchers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	js := newJobSchedulerForTestInternal(t, ctx, nil)
	watcher := &testBusWatcherInternal{
		name:    "test-bus-watcher",
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		ranNow:  make(chan context.Context, 1),
	}

	require.NoError(t, js.RegisterBusWatcher(watcher, true))
	require.NoError(t, js.StartScheduler())

	select {
	case <-watcher.started:
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for bus watcher to start")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- js.Stop(context.Background()) }()
	cancel()

	select {
	case <-watcher.stopped:
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for bus watcher to stop")
	}
	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "scheduler did not stop after bus watcher finished")
	}
}

func TestJobScheduler_StopJoinsRunnerBeforeStoppingWatcher(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)
	watcher := &orderedStopBusWatcherInternal{
		name:         "ordered-stop-watcher",
		started:      make(chan struct{}),
		runnerExited: make(chan struct{}),
	}

	require.NoError(t, js.RegisterBusWatcher(watcher, false))
	select {
	case <-watcher.started:
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for bus watcher to start")
	}
	require.NoError(t, js.Stop(t.Context()))
}

func TestJobScheduler_RegisterBusWatcherManualRunOption(t *testing.T) {
	ctx := t.Context()

	js := newJobSchedulerForTestInternal(t, ctx, nil)
	manualWatcher := &testBusWatcherInternal{
		name:    "manual-bus-watcher",
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		ranNow:  make(chan context.Context, 1),
	}
	automaticOnlyWatcher := &testBusWatcherInternal{
		name:    "automatic-only-bus-watcher",
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}

	require.NoError(t, js.RegisterBusWatcher(manualWatcher, true))
	require.NoError(t, js.RegisterBusWatcher(automaticOnlyWatcher, false))

	runCtx := context.Background()
	require.NoError(t, js.RunBusWatcherNow(runCtx, manualWatcher.Name()))
	require.Equal(t, runCtx, <-manualWatcher.ranNow)
	require.Error(t, js.RunBusWatcherNow(runCtx, automaticOnlyWatcher.Name()))
}

func TestJobScheduler_RescheduleJob_RemovesEntryWhenDisabled(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)
	enabled := true

	job := &conditionalTestSchedulerJob{
		testSchedulerJob: &testSchedulerJob{
			name:     "test-disabled-reschedule",
			schedule: "*/1 * * * * *",
		},
		shouldSchedule: func(context.Context) bool { return enabled },
	}

	require.NoError(t, js.RescheduleJob(context.Background(), job))
	state, ok := js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.True(t, state.Scheduled)

	enabled = false

	require.NoError(t, js.RescheduleJob(context.Background(), job))
	state, ok = js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.False(t, state.Scheduled)
	require.Empty(t, js.cron.Entries())
}

func TestJobScheduler_RescheduleJob_AddsEntryWhenEnabled(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)
	enabled := false

	job := &conditionalTestSchedulerJob{
		testSchedulerJob: &testSchedulerJob{
			name:     "test-enabled-reschedule",
			schedule: "*/1 * * * * *",
		},
		shouldSchedule: func(context.Context) bool { return enabled },
	}

	require.NoError(t, js.RescheduleJob(context.Background(), job))
	state, ok := js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.False(t, state.Scheduled)

	enabled = true

	require.NoError(t, js.RescheduleJob(context.Background(), job))
	state, ok = js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.True(t, state.Scheduled)
	require.Len(t, js.cron.Entries(), 1)
}

func TestJobScheduler_StartScheduler_SchedulesNonConditionalJobs(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)

	job := &testSchedulerJob{
		name:     "test-non-conditional-startup",
		schedule: "*/1 * * * * *",
	}

	require.NoError(t, js.RegisterJob(job))
	require.NoError(t, js.StartScheduler())
	defer js.cron.Stop()

	state, ok := js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.True(t, state.Scheduled)
	require.Len(t, js.cron.Entries(), 1)
}

func TestJobScheduler_RescheduleJob_UsesProvidedContext(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)

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
		require.FailNow(t, "timed out waiting for scheduled run")
	}
}

func TestJobScheduler_RescheduleJob_UsesLifecycleContextForShutdown(t *testing.T) {
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	js := newJobSchedulerForTestInternal(t, lifecycleCtx, nil)

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
		require.FailNow(t, "timed out waiting for scheduled run")
	}

	cancelLifecycle()

	select {
	case <-stoppedCh:
	case <-time.After(1500 * time.Millisecond):
		require.FailNow(t, "scheduled job did not observe lifecycle cancellation")
	}
}

func TestJobScheduler_AddJob_UpsertReplacesEntryWithoutLeaking(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)

	job := &testSchedulerJob{name: "dyn-upsert", schedule: "*/5 * * * * *"}
	require.NoError(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Len(t, js.cron.Entries(), 1)
	firstEntry := js.cron.Entries()[0].ID

	// Re-adding with a changed schedule (e.g. a new sync interval) must replace the
	// existing cron entry, not leak a second one that keeps firing forever.
	job.schedule = "*/10 * * * * *"
	require.NoError(t, js.AddJob(context.Background(), job))
	require.Len(t, js.cron.Entries(), 1)
	require.NotEqual(t, firstEntry, js.cron.Entries()[0].ID)

	state, ok := js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.True(t, state.Scheduled)
	require.Equal(t, "*/10 * * * * *", state.Schedule)
	require.NotNil(t, state.NextRun)
}

func TestJobScheduler_AddJob_InvalidRescheduleKeepsExistingEntry(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)

	job := &testSchedulerJob{name: "dyn-invalid-reschedule", schedule: "*/5 * * * * *"}
	require.NoError(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Len(t, js.cron.Entries(), 1)
	firstEntry := js.cron.Entries()[0].ID

	job.schedule = "not a cron schedule"
	require.Error(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Equal(t, firstEntry, js.cron.Entries()[0].ID)
	require.Len(t, js.cron.Entries(), 1)

	state, ok := js.GetJobRuntimeState(job.Name())
	require.True(t, ok)
	require.True(t, state.Scheduled)
	require.Equal(t, "*/5 * * * * *", state.Schedule)
}

func TestJobScheduler_RemoveJob_RemovesEntryAndIsNoopWhenAbsent(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)

	// Removing an unknown job must be a safe no-op (e.g. deleting a sync that never
	// had auto-sync enabled).
	js.RemoveJob(context.Background(), "never-registered")

	job := &testSchedulerJob{name: "dyn-remove", schedule: "*/5 * * * * *"}
	require.NoError(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Len(t, js.cron.Entries(), 1)

	js.RemoveJob(context.Background(), job.Name())
	require.False(t, js.HasJob(job.Name()))
	require.Empty(t, js.cron.Entries())
}

func TestJobScheduler_AddJob_GenericJobWithoutShouldRunIsScheduled(t *testing.T) {
	js := newJobSchedulerForTestInternal(t, context.Background(), nil)

	job := &schedulertypes.GenericJob{
		JobName:    "generic-dyn",
		ScheduleFn: func(context.Context) string { return "@every 1m" },
		RunFn:      func(context.Context) {},
	}
	require.NoError(t, js.AddJob(context.Background(), job))
	require.True(t, js.HasJob(job.Name()))
	require.Len(t, js.cron.Entries(), 1)
}

func TestJobScheduler_StopWaitsForCanceledJobToFinish(t *testing.T) {
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	jobStarted := make(chan struct{}, 1)
	cancellationObserved := make(chan struct{}, 1)
	releaseJob := make(chan struct{}, 1)
	jobFinished := make(chan struct{}, 1)
	var runOnce sync.Once

	js := newJobSchedulerForTestInternal(t, lifecycleCtx, nil)
	require.NoError(t, js.RegisterJob(&testSchedulerJob{
		name:     "test-shutdown-waits",
		schedule: "*/1 * * * * *",
		run: func(ctx context.Context) {
			runOnce.Do(func() {
				jobStarted <- struct{}{}
				<-ctx.Done()
				cancellationObserved <- struct{}{}
				<-releaseJob
				jobFinished <- struct{}{}
			})
		},
	}))
	require.NoError(t, js.StartScheduler())
	t.Cleanup(func() {
		cancelLifecycle()
		select {
		case releaseJob <- struct{}{}:
		default:
		}
	})

	select {
	case <-jobStarted:
	case <-time.After(2500 * time.Millisecond):
		require.FailNow(t, "timed out waiting for scheduled job to start")
	}

	cancelLifecycle()
	select {
	case <-cancellationObserved:
	case <-time.After(time.Second):
		require.FailNow(t, "scheduled job did not observe lifecycle cancellation")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- js.Stop(context.Background()) }()

	select {
	case err := <-stopDone:
		require.FailNowf(t, "unexpected failure", "scheduler stopped before the running job finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	releaseJob <- struct{}{}
	select {
	case <-jobFinished:
	case <-time.After(time.Second):
		require.FailNow(t, "scheduled job did not finish after release")
	}
	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "scheduler did not stop after the running job finished")
	}
}
