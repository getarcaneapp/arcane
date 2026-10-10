package flow_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow/flowtest"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
)

type activitiesFake struct {
	mu        sync.Mutex
	statuses  map[string]activitytypes.Status
	completed map[string]int
	tracked   map[string]context.CancelCauseFunc
}

type hostFixture struct {
	harness *flowtest.Harness
	job     *flow.Job
	cancel  context.CancelFunc
}

func TestRestartBetweenStepsReattachesSQLite(t *testing.T) {
	testRestartBetweenStepsInternal(t, "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "actors.db")))
}

func TestRestartBetweenStepsReattachesPostgres(t *testing.T) {
	databaseURL := os.Getenv("ARCANE_TEST_POSTGRES_DSN")
	if databaseURL == "" {
		t.Skip("ARCANE_TEST_POSTGRES_DSN is not set")
	}
	testRestartBetweenStepsInternal(t, databaseURL)
}

func testRestartBetweenStepsInternal(t *testing.T, databaseURL string) {
	jobID := fmt.Sprintf("flow-restart-%d", time.Now().UnixNano())
	activities := newActivitiesFake()
	var firstRuns atomic.Int32
	blocked := make(chan struct{})
	first := startHostInternal(t, databaseURL, jobID, 1, activities, &firstRuns, func(ctx context.Context, _ flow.Task) (any, error) {
		close(blocked)
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil)
	run, err := first.harness.Coordinator.Submit(t.Context(), scheduler.Request{JobID: jobID, EnvironmentID: "0", Trigger: "manual"})
	require.NoError(t, err)
	select {
	case <-blocked:
	case <-time.After(20 * time.Second):
		t.Fatal("second step never started")
	}
	started, err := first.harness.Coordinator.Get(t.Context(), "0", jobID, run.ID)
	require.NoError(t, err)
	instanceID := workflowTargetInternal(t, started).ID
	first.stop(t)

	second := startHostInternal(t, databaseURL, jobID, 1, activities, &firstRuns, func(context.Context, flow.Task) (any, error) {
		return nil, nil
	}, nil)
	finished := waitTerminalInternal(t, second, jobID, run.ID)

	require.Equal(t, scheduler.Succeeded, finished.Status)
	require.Equal(t, int32(1), firstRuns.Load(), "completed steps must not run again")
	target := workflowTargetInternal(t, finished)
	require.Equal(t, instanceID, target.ID, "reconcile must re-attach to the same instance")
	require.Equal(t, scheduler.Succeeded, target.Status)
	require.Len(t, finished.Outcome.Steps, 3)
	for _, step := range finished.Outcome.Steps {
		require.Equal(t, scheduler.Succeeded, step.Status, step.Name)
	}
	require.Eventually(t, func() bool { return activities.status(instanceID) == activitytypes.StatusSuccess }, 10*time.Second, 50*time.Millisecond)
}

func TestVersionBumpFallsBack(t *testing.T) {
	databaseURL := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "actors.db"))
	jobID := fmt.Sprintf("flow-version-%d", time.Now().UnixNano())
	activities := newActivitiesFake()
	var firstRuns atomic.Int32
	blocked := make(chan struct{})
	first := startHostInternal(t, databaseURL, jobID, 1, activities, &firstRuns, func(ctx context.Context, _ flow.Task) (any, error) {
		close(blocked)
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil)
	run, err := first.harness.Coordinator.Submit(t.Context(), scheduler.Request{JobID: jobID, EnvironmentID: "0", Trigger: "manual"})
	require.NoError(t, err)
	select {
	case <-blocked:
	case <-time.After(20 * time.Second):
		t.Fatal("second step never started")
	}
	first.stop(t)

	var fallbacks atomic.Int32
	second := startHostInternal(t, databaseURL, jobID, 2, activities, &firstRuns, func(context.Context, flow.Task) (any, error) {
		return nil, nil
	}, func(context.Context, scheduler.Run) (scheduler.Outcome, error) {
		fallbacks.Add(1)
		return scheduler.Outcome{Status: scheduler.Succeeded, Message: "recovered"}, nil
	})
	finished := waitTerminalInternal(t, second, jobID, run.ID)

	require.Equal(t, scheduler.Succeeded, finished.Status)
	require.Equal(t, "recovered", finished.Outcome.Message)
	require.Equal(t, int32(1), fallbacks.Load())
	target := workflowTargetInternal(t, finished)
	require.Equal(t, scheduler.Failed, target.Status, "the unserved instance must be retired")
}

func TestChildInheritsRunBinding(t *testing.T) {
	jobID := fmt.Sprintf("flow-child-%d", time.Now().UnixNano())
	harness := flowtest.New(t, newActivitiesFake())
	engine := harness.Engine
	childRuns := make(chan string, 1)
	child, err := engine.Define(flow.Definition{
		Name: "flow-child", Version: 1, Concurrency: 1, Timeout: time.Minute,
		Steps: []workflow.StepSpec{
			workflow.Step("work", engine.Handler(func(ctx context.Context, t flow.Task) (any, error) {
				var label string
				if err := t.Payload(&label); err != nil {
					return nil, err
				}
				run, _ := jobcontext.Run(ctx)
				childRuns <- run.ID
				return scheduler.Outcome{Status: scheduler.Partial, Message: "child " + label}, nil
			})),
		},
	})
	require.NoError(t, err)
	parent, err := engine.Define(flow.Definition{
		Name: "flow-parent", Version: 1, Concurrency: 1, Timeout: time.Minute,
		Steps: []workflow.StepSpec{
			workflow.Step("prepare", engine.Handler(func(_ context.Context, t flow.Task) (any, error) {
				return t.ChildInput("payload")
			})),
			workflow.Child("child", workflow.WithDefinition(child.Francis())),
			workflow.Step("finalize", engine.Handler(func(_ context.Context, t flow.Task) (any, error) {
				var childOutcome scheduler.Outcome
				if decodeErr := t.DecodeOutput("child", &childOutcome); decodeErr != nil {
					return nil, decodeErr
				}
				return scheduler.Outcome{Status: scheduler.Succeeded, Message: childOutcome.Message}, nil
			})),
		},
	})
	require.NoError(t, err)
	fixture := startJobInternal(t, harness, &flow.Job{Engine: engine, Workflow: parent, JobName: jobID, ScheduleFn: func(context.Context) string { return "" }})

	run, err := harness.Coordinator.Submit(t.Context(), scheduler.Request{JobID: jobID, EnvironmentID: "0", Trigger: "manual"})
	require.NoError(t, err)
	finished := waitTerminalInternal(t, fixture, jobID, run.ID)

	require.Equal(t, scheduler.Succeeded, finished.Status)
	require.Equal(t, "child payload", finished.Outcome.Message)
	require.Equal(t, run.ID, <-childRuns, "child tasks must run inside the parent's run")
}

func TestActivityCancelInterruptsRunningStep(t *testing.T) {
	activities := newActivitiesFake()
	harness := flowtest.New(t, activities)
	engine := harness.Engine
	started := make(chan string, 1)
	causes := make(chan error, 1)
	wf, err := engine.Define(flow.Definition{
		Name: "flow-cancel", Version: 1, Concurrency: 1, Timeout: time.Minute,
		Activity: activitylib.StartRequest{Type: activitytypes.TypeAutoUpdate},
		Steps: []workflow.StepSpec{
			workflow.Step("block", engine.Handler(func(ctx context.Context, t flow.Task) (any, error) {
				started <- t.ActivityID()
				<-ctx.Done()
				causes <- context.Cause(ctx)
				return nil, ctx.Err()
			})),
		},
	})
	require.NoError(t, err)
	harness.Start(t)

	outcomes := make(chan scheduler.Outcome, 1)
	go func() {
		outcome, _ := engine.Run(t.Context(), wf, nil, activitylib.StartRequest{}, nil)
		outcomes <- outcome
	}()
	activityID := <-started
	activities.requestCancel(activityID)

	select {
	case cause := <-causes:
		require.ErrorIs(t, cause, activitylib.ErrCanceled, "the running step must stop when the user cancels")
	case <-time.After(10 * time.Second):
		t.Fatal("the running step was not interrupted")
	}
	select {
	case outcome := <-outcomes:
		require.Equal(t, scheduler.Canceled, outcome.Status)
	case <-time.After(20 * time.Second):
		t.Fatal("the workflow did not finish after cancel")
	}
	require.Equal(t, activitytypes.StatusCancelled, activities.status(activityID))
}

func TestStartFinishesSubmittedInstanceWithItsProgress(t *testing.T) {
	databaseURL := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "actors.db"))
	activities := newActivitiesFake()
	started := make(chan struct{})
	define := func(harness *flowtest.Harness, work func(context.Context, flow.Task) (any, error)) *flow.Workflow {
		wf, err := harness.Engine.Define(flow.Definition{
			Name: "flow-orphan", Version: 1, Concurrency: 1, Timeout: time.Minute,
			Activity: activitylib.StartRequest{Type: activitytypes.TypeAutoUpdate},
			Steps:    []workflow.StepSpec{workflow.Step("work", harness.Engine.Handler(work))},
		})
		require.NoError(t, err)
		return wf
	}

	first := flowtest.New(t, activities, databaseURL)
	wf := define(first, func(ctx context.Context, _ flow.Task) (any, error) {
		if err := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: "backup", Status: scheduler.Running, RecoveryData: []byte(`{"backupId":"frozen"}`)}); err != nil {
			return nil, err
		}
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	first.Start(t)
	activityID, err := first.Engine.Submit(t.Context(), wf, nil, activitylib.StartRequest{})
	require.NoError(t, err)
	<-started
	standalone, err := first.Engine.Standalone(t.Context())
	require.NoError(t, err)
	require.Len(t, standalone, 1)
	require.Equal(t, activityID, standalone[0].ActivityID)
	require.JSONEq(t, `{"backupId":"frozen"}`, string(standalone[0].Outcome.Targets[0].RecoveryData))
	(&hostFixture{harness: first, cancel: func() {}}).stop(t)

	// No caller awaits the instance on the new host; Start alone must finish it, and the
	// redelivered task must see the progress the interrupted one recorded.
	second := flowtest.New(t, activities, databaseURL)
	define(second, func(ctx context.Context, _ flow.Task) (any, error) {
		previous, _ := jobcontext.Run(ctx)
		if len(previous.Outcome.Targets) != 1 || string(previous.Outcome.Targets[0].RecoveryData) != `{"backupId":"frozen"}` {
			return nil, errors.New("interrupted progress was lost")
		}
		return scheduler.Outcome{Status: scheduler.Succeeded}, nil
	})
	second.Start(t)
	require.Eventually(t, func() bool { return activities.status(activityID) == activitytypes.StatusSuccess }, 20*time.Second, 50*time.Millisecond)
	standalone, err = second.Engine.Standalone(t.Context())
	require.NoError(t, err)
	require.Empty(t, standalone)
}

func TestRetryable(t *testing.T) {
	var netErr net.Error = &net.DNSError{IsTimeout: true}
	cases := map[string]struct {
		err  error
		want bool
	}{
		"unavailable":  {common.Classify(common.ErrUnavailable, errors.New("503")), true},
		"timeout":      {common.Classify(common.ErrTimeout, errors.New("slow")), true},
		"deadline":     {fmt.Errorf("lookup: %w", context.DeadlineExceeded), true},
		"network":      {netErr, true},
		"tls":          {&url.Error{Op: "Get", URL: "https://registry", Err: errors.New("x509: certificate signed by unknown authority")}, false},
		"unauthorized": {common.Classify(common.ErrUnauthorized, errors.New("denied")), false},
		"plain":        {errors.New("boom"), false},
	}
	for name, tc := range cases {
		require.Equal(t, tc.want, flow.Retryable(tc.err), name)
	}
}

func startHostInternal(
	t *testing.T,
	databaseURL, jobID string,
	version int,
	activities *activitiesFake,
	firstRuns *atomic.Int32,
	second func(context.Context, flow.Task) (any, error),
	fallback func(context.Context, scheduler.Run) (scheduler.Outcome, error),
) *hostFixture {
	t.Helper()
	harness := flowtest.New(t, activities, databaseURL)
	engine := harness.Engine
	steps := []workflow.StepSpec{
		workflow.Step("first", engine.Handler(func(context.Context, flow.Task) (any, error) {
			firstRuns.Add(1)
			return nil, nil
		})),
		workflow.Step("second", engine.Handler(second), workflow.WithMaxAttempts(1)),
		workflow.Step("finalize", engine.Handler(func(context.Context, flow.Task) (any, error) {
			return scheduler.Outcome{Status: scheduler.Succeeded, Message: "done"}, nil
		})),
	}
	if version > 1 {
		steps = append(steps[:2], workflow.Step("extra", engine.Handler(func(context.Context, flow.Task) (any, error) { return nil, nil })), steps[2])
	}
	wf, err := engine.Define(flow.Definition{
		Name:        "flow-test",
		Version:     version,
		Concurrency: 2,
		Timeout:     time.Minute,
		Activity:    activitylib.StartRequest{Type: activitytypes.TypeAutoUpdate},
		Steps:       steps,
	})
	require.NoError(t, err)
	job := &flow.Job{Engine: engine, Workflow: wf, JobName: jobID, ScheduleFn: func(context.Context) string { return "" }, FallbackFn: fallback}
	return startJobInternal(t, harness, job)
}

// startJobInternal starts the host and a coordinator that executes job.
func startJobInternal(t *testing.T, harness *flowtest.Harness, job *flow.Job) *hostFixture {
	t.Helper()
	coordinator := harness.Coordinator
	coordinator.SetExecutor(
		func(ctx context.Context, run scheduler.Run) (scheduler.Outcome, error) {
			return job.Run(coordinator.ExecutionContext(ctx, run, ""))
		},
		func(ctx context.Context, run scheduler.Run) (scheduler.Outcome, error) {
			return job.Reconcile(coordinator.ExecutionContext(ctx, run, ""), run)
		},
	)
	harness.Start(t)
	appCtx, cancel := context.WithCancel(t.Context())
	require.NoError(t, coordinator.Start(t.Context(), appCtx))
	coordinator.Activate()
	fixture := &hostFixture{harness: harness, job: job, cancel: cancel}
	t.Cleanup(func() { fixture.stop(t) })
	return fixture
}

// stop simulates a process exit between steps.
func (f *hostFixture) stop(t *testing.T) {
	t.Helper()
	if f.cancel == nil {
		return
	}
	f.cancel()
	f.cancel = nil
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 15*time.Second)
	defer cancel()
	_ = f.harness.Coordinator.Stop(ctx)
	_ = f.harness.Engine.Stop(ctx)
	require.NoError(t, f.harness.Runtime.Stop(ctx))
}

func waitTerminalInternal(t *testing.T, fixture *hostFixture, jobID, runID string) scheduler.Run {
	t.Helper()
	var run scheduler.Run
	require.Eventually(t, func() bool {
		var err error
		run, err = fixture.harness.Coordinator.Get(t.Context(), "0", jobID, runID)
		return err == nil && run.Status.Terminal()
	}, 45*time.Second, 100*time.Millisecond)
	return run
}

func workflowTargetInternal(t *testing.T, run scheduler.Run) scheduler.TargetOutcome {
	t.Helper()
	var found []scheduler.TargetOutcome
	for _, target := range run.Outcome.Targets {
		if target.ResourceType == scheduler.WorkflowTarget {
			found = append(found, target)
		}
	}
	require.Len(t, found, 1)
	return found[0]
}

func newActivitiesFake() *activitiesFake {
	return &activitiesFake{statuses: map[string]activitytypes.Status{}, completed: map[string]int{}, tracked: map[string]context.CancelCauseFunc{}}
}

func (f *activitiesFake) status(id string) activitytypes.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses[id]
}

func (f *activitiesFake) StartActivity(_ context.Context, req activitylib.StartRequest) (*activitytypes.Activity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.statuses[req.ID]; !ok {
		f.statuses[req.ID] = activitytypes.StatusRunning
	}
	return &activitytypes.Activity{ID: req.ID, Status: f.statuses[req.ID]}, nil
}

func (f *activitiesFake) CompleteActivity(_ context.Context, activityID string, status activitytypes.Status, _ string, _ *string, _ ...string) (*activitytypes.Activity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[activityID] = status
	f.completed[activityID]++
	return &activitytypes.Activity{ID: activityID, Status: status}, nil
}

func (f *activitiesFake) AppendMessage(context.Context, string, activitylib.AppendMessageRequest) (*activitytypes.Message, error) {
	return &activitytypes.Message{}, nil
}

func (f *activitiesFake) AppendMessages(context.Context, string, []activitylib.AppendMessageRequest) ([]activitytypes.Message, error) {
	return nil, nil
}

func (f *activitiesFake) Track(ctx context.Context, activityID string) context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	tracked, cancel := context.WithCancelCause(ctx)
	f.tracked[activityID] = cancel
	return tracked
}

// requestCancel cancels an activity the way the activity service does for a user.
func (f *activitiesFake) requestCancel(activityID string) {
	f.mu.Lock()
	cancel := f.tracked[activityID]
	f.mu.Unlock()
	cancel(activitylib.ErrCanceled)
}

func (f *activitiesFake) UpdateActivity(_ context.Context, activityID string, _ activitylib.UpdateRequest) (*activitytypes.Activity, error) {
	return &activitytypes.Activity{ID: activityID}, nil
}

func (f *activitiesFake) AwaitActivitySlotBounded(context.Context, string, string) error { return nil }
