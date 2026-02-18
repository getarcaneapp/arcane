package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

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
