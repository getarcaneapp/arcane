package runs

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
)

func newResolutionQueue(t *testing.T) *Coordinator {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	runtime := francistest.New(t)
	q := New(kv.NewKVService(&database.DB{DB: db}), runtime.Service(), time.UTC)
	require.NoError(t, q.Register(runtime))
	francistest.Start(t, runtime)
	return q
}

func TestResolvePreservesEvidenceWithoutBlockingSchedule(t *testing.T) {
	q := newResolutionQueue(t)
	ctx := t.Context()
	run, err := q.Submit(ctx, types.SchedulerRequest{JobID: "auto-update", Trigger: "manual"})
	require.NoError(t, err)
	evidence := types.SchedulerOutcome{Status: types.RunStatusNeedsAttention, Message: "original failure", Targets: []types.TargetOutcome{{ID: "container", Status: types.RunStatusSucceeded}}}
	require.NoError(t, q.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
		current.Status = types.RunStatusNeedsAttention
		current.Outcome = evidence
		current.AttemptCount = 1
		current.Attempts = []types.SchedulerAttempt{{Number: 1, Outcome: evidence}}
		return nil
	}))
	pending, err := q.Submit(ctx, types.SchedulerRequest{JobID: "auto-update", Trigger: "manual"})
	require.NoError(t, err)
	history, err := q.List(ctx, "0", "auto-update", 1, 20)
	require.NoError(t, err)
	selected, _ := q.nextRun(history.Runs)
	require.NotNil(t, selected)
	require.Equal(t, pending.ID, selected.ID)
	for len(q.wake) > 0 {
		<-q.wake
	}
	resolved, err := q.Resolve(ctx, "0", "auto-update", run.ID, "operator")
	require.NoError(t, err)
	require.Equal(t, types.RunStatusCanceled, resolved.Status)
	require.Equal(t, evidence, resolved.Outcome)
	require.Len(t, resolved.Attempts, 1)
	require.Equal(t, "operator", resolved.Resolution.ResolvedBy)
	require.Equal(t, "resolved_after_review", resolved.Resolution.Reason)
	require.NotZero(t, resolved.Resolution.ResolvedAt)
	select {
	case <-q.wake:
	default:
		t.Fatal("resolution did not wake pending work")
	}
	history, err = q.List(ctx, "0", "auto-update", 1, 20)
	require.NoError(t, err)
	selected, _ = q.nextRun(history.Runs)
	require.NotNil(t, selected)
	require.Equal(t, pending.ID, selected.ID)
	duplicate, err := q.Resolve(ctx, "0", "auto-update", run.ID, "different operator")
	require.NoError(t, err)
	require.Equal(t, resolved.ID, duplicate.ID)
	require.Equal(t, resolved.Status, duplicate.Status)
	require.Equal(t, resolved.Outcome, duplicate.Outcome)
	require.Equal(t, resolved.Resolution.ResolvedBy, duplicate.Resolution.ResolvedBy)
	require.True(t, resolved.Resolution.ResolvedAt.Equal(duplicate.Resolution.ResolvedAt))

	// Startup closes legacy local failures and reconciles unsettled remote work.
	localLegacy, err := q.Submit(ctx, types.SchedulerRequest{JobID: "gitops-sync:project", Trigger: "scheduled"})
	require.NoError(t, err)
	remoteLegacy, err := q.Submit(ctx, types.SchedulerRequest{JobID: "gitops-sync:project", EnvironmentID: "remote", Trigger: "scheduled"})
	require.NoError(t, err)
	for _, legacy := range []types.SchedulerRun{localLegacy, remoteLegacy} {
		require.NoError(t, q.UpdateRun(ctx, legacy, func(current *types.SchedulerRun) error {
			current.Status = types.RunStatusNeedsAttention
			current.Outcome = evidence
			current.Attempts = []types.SchedulerAttempt{{Number: 1, Outcome: evidence}}
			current.RemoteDeliveryAttempted = current.EnvironmentID != "0"
			return nil
		}))
	}
	q.SetExecutor(func(context.Context, types.SchedulerRun) (types.SchedulerOutcome, error) {
		t.Error("startup must not repeat legacy work")
		return types.SchedulerOutcome{Status: types.RunStatusFailed}, nil
	}, nil)
	startCtx, cancelStart := context.WithCancel(ctx)
	cancelStart()
	require.ErrorIs(t, q.Start(startCtx, ctx), context.Canceled)
	startCtx, cancelStart = context.WithCancel(ctx)
	defer cancelStart()
	require.NoError(t, q.Start(startCtx, ctx))
	cancelStart()
	require.NoError(t, q.lifecycleCtx.Err())
	t.Cleanup(func() { require.NoError(t, q.Stop(context.WithoutCancel(ctx))) })
	localLegacy, err = q.Get(ctx, "0", localLegacy.JobID, localLegacy.ID)
	require.NoError(t, err)
	require.Equal(t, types.RunStatusFailed, localLegacy.Status)
	require.Equal(t, evidence.Message, localLegacy.Outcome.Message)
	require.Equal(t, evidence.Targets, localLegacy.Outcome.Targets)
	require.Equal(t, evidence, localLegacy.Attempts[0].Outcome)
	require.NotNil(t, localLegacy.FinishedAt)
	remoteLegacy, err = q.Get(ctx, "remote", remoteLegacy.JobID, remoteLegacy.ID)
	require.NoError(t, err)
	require.Equal(t, types.RunStatusWaiting, remoteLegacy.Status)
	require.Equal(t, evidence, remoteLegacy.Outcome)
	require.False(t, remoteLegacy.RemoteSettled)
	require.Nil(t, remoteLegacy.FinishedAt)
	future, err := q.Submit(ctx, types.SchedulerRequest{JobID: localLegacy.JobID, Trigger: "scheduled"})
	require.NoError(t, err)
	require.NotEqual(t, localLegacy.ID, future.ID)
}

func TestResolveRejectsUnsafeStates(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      types.RunStatus
		active      bool
		environment string
		delivered   bool
	}{
		{name: "queued", status: types.RunStatusQueued, environment: "0"},
		{name: "running", status: types.RunStatusRunning, environment: "0"},
		{name: "active worker", status: types.RunStatusRunning, active: true, environment: "0"},
		{name: "uncertain remote", status: types.RunStatusNeedsAttention, environment: "remote", delivered: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			q := newResolutionQueue(t)
			run, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "auto-update", EnvironmentID: test.environment})
			require.NoError(t, err)
			require.NoError(t, q.UpdateRun(t.Context(), run, func(current *types.SchedulerRun) error {
				current.Status = test.status
				current.RemoteDeliveryAttempted = test.delivered
				if test.active {
					current.Owner = "active-worker"
				}
				return nil
			}))
			_, err = q.Resolve(t.Context(), test.environment, "auto-update", run.ID, "operator")
			require.Error(t, err)
			stored, err := q.Get(t.Context(), test.environment, "auto-update", run.ID)
			require.NoError(t, err)
			require.Equal(t, test.status, stored.Status)
			require.Nil(t, stored.Resolution)
		})
	}
}
