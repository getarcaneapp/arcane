package backup

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
	bt "github.com/getarcaneapp/arcane/types/v2/backup"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
)

func TestDurableBackupRestartKeepsCompletionInternal(t *testing.T) {
	url := "file:" + filepath.Join(t.TempDir(), "actors.db")
	var calls atomic.Int32
	runtime := francistest.New(t, url)
	engine := NewEngine(t.Context(), nil, nil)
	engine.RegisterRunKind("test", func(ctx context.Context, _ string, _ []byte, interrupted bool) error {
		calls.Add(1)
		require.False(t, interrupted)
		return jobcontext.Progress(ctx, st.TargetOutcome{ID: "snapshot", Status: st.Succeeded})
	})
	require.NoError(t, engine.Register(runtime))
	francistest.Start(t, runtime)
	command := bt.DurableRunCommand{Kind: "test", RunID: "run", ActivityID: "activity"}
	require.NoError(t, engine.SubmitDurableRun(t.Context(), command, nil))
	require.Eventually(t, func() bool {
		states, err := engine.activeRunsInternal(t.Context())
		return err == nil && len(states) == 0 && calls.Load() == 1
	}, 10*time.Second, 20*time.Millisecond)
	stopCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, runtime.Stop(stopCtx))
	restarted := francistest.New(t, url)
	next := NewEngine(t.Context(), nil, nil)
	next.RegisterRunKind("test", func(context.Context, string, []byte, bool) error { calls.Add(1); return nil })
	require.NoError(t, next.Register(restarted))
	francistest.Start(t, restarted)
	require.NoError(t, next.SubmitDurableRun(t.Context(), command, nil))
	require.NoError(t, next.ReconcileDispatches(t.Context()))
	require.EqualValues(t, 1, calls.Load())
}

func TestDurableBackupShutdownKeepsInterruptedEvidenceInternal(t *testing.T) {
	url := "file:" + filepath.Join(t.TempDir(), "actors.db")
	runtime := francistest.New(t, url)
	admission := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, admission.Register(runtime))
	engine := NewEngine(t.Context(), admission, nil)
	entered := make(chan struct{})
	drain := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-drain:
		default:
			close(drain)
		}
	})
	engine.RegisterRunKind("test", func(ctx context.Context, _ string, _ []byte, _ bool) error {
		if err := jobcontext.Progress(ctx, st.TargetOutcome{ID: "snapshot", Status: st.Running, RecoveryData: []byte(`{"backupId":"frozen"}`)}); err != nil {
			return err
		}
		close(entered)
		<-ctx.Done()
		<-drain
		return ctx.Err()
	})
	require.NoError(t, engine.Register(runtime))
	francistest.Start(t, runtime)
	lease, acquired, err := engine.TryAcquireRun(t.Context(), "backup", "data")
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, engine.SubmitDurableRun(t.Context(), bt.DurableRunCommand{Kind: "test", RunID: "interrupted"}, lease))
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("backup did not start")
	}
	stopCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- engine.Stop(stopCtx) }()
	require.Eventually(t, func() bool { return engine.lifecycleCtx.Err() != nil }, time.Second, time.Millisecond)
	_, acquired, err = engine.TryAcquireRun(t.Context(), "backup", "data")
	require.NoError(t, err)
	require.False(t, acquired)
	close(drain)
	require.NoError(t, <-stopped)
	lease, acquired, err = engine.TryAcquireRun(t.Context(), "backup", "data")
	require.NoError(t, err)
	require.True(t, acquired)
	lease.Release(t.Context())
	require.NoError(t, runtime.Stop(stopCtx))
	restarted := francistest.New(t, url)
	next := NewEngine(t.Context(), nil, nil)
	resumed := make(chan struct{})
	next.RegisterRunKind("test", func(ctx context.Context, _ string, _ []byte, interrupted bool) error {
		require.True(t, interrupted)
		previous, ok := jobcontext.Run(ctx)
		require.True(t, ok)
		require.Len(t, previous.Outcome.Targets, 1)
		require.Equal(t, `{"backupId":"frozen"}`, string(previous.Outcome.Targets[0].RecoveryData))
		close(resumed)
		return nil
	})
	require.NoError(t, next.Register(restarted))
	francistest.Start(t, restarted)
	require.NoError(t, next.ReconcileDispatches(t.Context()))
	select {
	case <-resumed:
	case <-time.After(15 * time.Second):
		t.Fatal("durable backup did not resume")
	}
}
