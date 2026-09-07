package queue

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newResolutionQueueInternal(t *testing.T) *Queue {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return New(kv.NewKVService(&database.DB{DB: db}), nil, nil)
}

func TestResolvePreservesEvidenceAndUnblocksSchedule(t *testing.T) {
	q := newResolutionQueueInternal(t)
	ctx := t.Context()
	run, err := q.Submit(ctx, st.Request{JobID: "auto-update", Trigger: "manual"})
	require.NoError(t, err)
	evidence := st.Outcome{Status: st.NeedsAttention, Message: "original failure", Targets: []st.TargetOutcome{{ID: "container", Status: st.Succeeded}}}
	require.NoError(t, q.UpdateRun(ctx, run, func(current *st.Run) error {
		current.Status = st.NeedsAttention
		current.Outcome = evidence
		current.AttemptCount = 1
		current.Attempts = []st.Attempt{{Number: 1, Outcome: evidence}}
		return nil
	}))
	pending, err := q.Submit(ctx, st.Request{JobID: "auto-update", Trigger: "manual"})
	require.NoError(t, err)
	history, err := q.List(ctx, "0", "auto-update", 1, 20)
	require.NoError(t, err)
	selected, _ := q.nextRunInternal(history.Runs)
	require.Nil(t, selected)
	for len(q.wake) > 0 {
		<-q.wake
	}
	resolved, err := q.Resolve(ctx, "0", "auto-update", run.ID, "operator")
	require.NoError(t, err)
	require.Equal(t, st.Canceled, resolved.Status)
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
	selected, _ = q.nextRunInternal(history.Runs)
	require.NotNil(t, selected)
	require.Equal(t, pending.ID, selected.ID)
	duplicate, err := q.Resolve(ctx, "0", "auto-update", run.ID, "different operator")
	require.NoError(t, err)
	require.Equal(t, resolved, duplicate)
}

func TestResolveRejectsUnsafeStates(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      st.RunStatus
		active      bool
		environment string
		delivered   bool
	}{
		{name: "queued", status: st.Queued, environment: "0"},
		{name: "running", status: st.Running, environment: "0"},
		{name: "active worker", status: st.NeedsAttention, active: true, environment: "0"},
		{name: "uncertain remote", status: st.NeedsAttention, environment: "remote", delivered: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			q := newResolutionQueueInternal(t)
			run, err := q.Submit(t.Context(), st.Request{JobID: "auto-update", EnvironmentID: test.environment})
			require.NoError(t, err)
			require.NoError(t, q.UpdateRun(t.Context(), run, func(current *st.Run) error {
				current.Status = test.status
				current.RemoteDeliveryAttempted = test.delivered
				return nil
			}))
			q.active[queueKeyInternal(test.environment, "auto-update")] = test.active
			_, err = q.Resolve(context.Background(), test.environment, "auto-update", run.ID, "operator")
			require.Error(t, err)
			stored, err := q.Get(t.Context(), test.environment, "auto-update", run.ID)
			require.NoError(t, err)
			require.Equal(t, test.status, stored.Status)
			require.Nil(t, stored.Resolution)
		})
	}
}
