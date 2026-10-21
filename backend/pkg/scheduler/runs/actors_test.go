package runs

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/italypaleale/francis/actor"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
)

func TestLegacyImportSurvivesRestartAndDeduplicates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	store := kv.NewKVService(&database.DB{DB: db})
	legacy := types.SchedulerQueueRecord{
		JobID:         "legacy",
		EnvironmentID: "0",
		Runs: []types.SchedulerRun{{
			ID:            uuid.New().String(),
			JobID:         "legacy",
			EnvironmentID: "0",
			Trigger:       "manual",
			Status:        types.RunStatusSucceeded,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		}},
	}
	legacy.Runs[0].ActivityID = legacy.Runs[0].ID
	for _, jobID := range []string{"gitops-sync:first", "gitops-sync:second"} {
		record := types.SchedulerQueueRecord{JobID: jobID, EnvironmentID: "0", Schedule: "@every 5m", NextRun: time.Now().UTC().Add(time.Hour)}
		for i := range 1600 {
			created := time.Now().UTC().Add(-time.Duration(1600-i) * 5 * time.Minute)
			record.Runs = append(
				record.Runs,
				types.SchedulerRun{
					ID:            uuid.New().String(),
					ActivityID:    uuid.New().String(),
					JobID:         jobID,
					EnvironmentID: "0",
					Trigger:       "scheduled",
					Status:        types.RunStatusNeedsAttention,
					CreatedAt:     created,
					UpdatedAt:     created,
				},
			)
		}
		data, marshalErr := json.Marshal(record)
		require.NoError(t, marshalErr)
		require.NoError(t, store.Set(t.Context(), queuePrefix+kit.SHA256Hex("0\x00"+jobID), string(data)))
	}
	encoded, err := json.Marshal(legacy)
	require.NoError(t, err)
	key := queuePrefix + kit.SHA256Hex("0\x00"+legacy.JobID)
	require.NoError(t, store.Set(t.Context(), key, string(encoded)))
	databaseURL := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "actors.db"))
	first := francistest.New(t, databaseURL)
	q := New(store, first.Service(), time.UTC)
	require.NoError(t, q.Register(first))
	francistest.Start(t, first)
	require.NoError(t, q.importLegacy(t.Context()))
	require.NoError(t, q.importLegacy(t.Context()))
	observer := &activityObserverFake{existingOnly: true}
	observer.unavailable.Store(true)
	q.SetObserver(observer)
	_, err = q.Submit(t.Context(), types.SchedulerRequest{JobID: legacy.JobID, RunID: legacy.Runs[0].ID, Trigger: "manual"})
	require.NoError(t, err)
	require.NoError(t, q.UpdateRun(t.Context(), legacy.Runs[0], func(run *types.SchedulerRun) error {
		run.Outcome.Targets = []types.TargetOutcome{{ID: "artifact", Status: types.RunStatusSucceeded, RecoveryData: jsontext.Value(`{"backupId":"artifact"}`)}}
		return nil
	}))
	require.True(t, pendingActivitySync(t, q))
	q.SetExecutor(func(context.Context, types.SchedulerRun) (types.SchedulerOutcome, error) {
		t.Error("startup must not execute jobs before activation")
		return types.SchedulerOutcome{Status: types.RunStatusFailed}, nil
	}, nil)
	require.NoError(t, q.Start(t.Context(), t.Context()))
	require.NoError(t, q.Stop(t.Context()))
	require.NoError(t, first.Stop(t.Context()))

	second := francistest.New(t, databaseURL)
	restored := New(store, second.Service(), time.UTC)
	restored.SetObserver(observer)
	restored.SetExecutor(q.execute, nil)
	require.NoError(t, restored.Register(second))
	francistest.Start(t, second)
	require.NoError(t, restored.importLegacy(t.Context()))
	require.NoError(t, restored.Start(t.Context(), t.Context()))
	t.Cleanup(func() { require.NoError(t, restored.Stop(context.WithoutCancel(t.Context()))) })
	for _, jobID := range []string{"gitops-sync:first", "gitops-sync:second"} {
		history, listErr := restored.List(t.Context(), "0", jobID, 1, 100)
		require.NoError(t, listErr)
		require.Equal(t, 100, history.Total)
		require.Equal(t, types.RunStatusFailed, history.Runs[0].Status)
		state, stateErr := restored.ScheduleState(t.Context(), jobID)
		require.NoError(t, stateErr)
		require.Equal(t, "@every 5m", state.Schedule)
		require.True(t, state.NextRun.After(time.Now()))
	}
	duplicate, err := restored.Submit(t.Context(), types.SchedulerRequest{JobID: legacy.JobID, RunID: legacy.Runs[0].ID, Trigger: "manual"})
	require.NoError(t, err)
	require.Equal(t, types.RunStatusSucceeded, duplicate.Status)
	require.Equal(t, jsontext.Value(`{"backupId":"artifact"}`), duplicate.Outcome.Targets[0].RecoveryData)
	history, err := restored.List(t.Context(), "0", legacy.JobID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, history.Total)
	require.Equal(t, time.UTC, history.Runs[0].CreatedAt.Location())
	observer.unavailable.Store(false)
	require.False(t, restored.retryActivitySync(t.Context(), coordinatorStates(t, restored)))
	require.False(t, pendingActivitySync(t, restored))
	require.Positive(t, observer.observed.Load())
	_, exists, err := store.Get(t.Context(), key)
	require.NoError(t, err)
	require.True(t, exists)
}

type occurrenceEnvelope struct{ occurrence types.ScheduleOccurrence }

func (e occurrenceEnvelope) Decode(value any) error {
	*value.(*types.ScheduleOccurrence) = e.occurrence
	return nil
}

func TestAlarmIgnoresStaleGeneration(t *testing.T) {
	q := newTestCoordinator(t)
	next := time.Now().UTC().Add(time.Hour)
	require.NoError(t, q.Checkpoint(t.Context(), "schedule", "0 * * * * *", next))
	before, err := q.ScheduleState(t.Context(), "schedule")
	require.NoError(t, err)
	q.enabled.Store(true)
	defer q.enabled.Store(false)
	coordinator := &coordinatorActor{id: kit.SHA256Hex("0\x00schedule"), q: q}
	require.NoError(t, coordinator.Alarm(t.Context(), "old", occurrenceEnvelope{types.ScheduleOccurrence{Generation: before.Generation - 1, Sequence: before.Sequence}}))
	after, err := q.ScheduleState(t.Context(), "schedule")
	require.NoError(t, err)
	require.Equal(t, before.Generation, after.Generation)
	require.Equal(t, before.Sequence, after.Sequence)
	require.True(t, before.NextRun.Equal(after.NextRun))
	require.Empty(t, after.Runs)
}

func TestDuplicateDispatchAndCanceledDelivery(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		name := "duplicate"
		if cancel {
			name = "canceled"
		}
		t.Run(name, func(t *testing.T) {
			q := newTestCoordinator(t)
			var executed atomic.Int32
			q.SetExecutor(func(context.Context, types.SchedulerRun) (types.SchedulerOutcome, error) {
				executed.Add(1)
				return types.SchedulerOutcome{
					Status:  types.RunStatusNeedsAttention,
					Message: "Completion could not be confirmed",
					Targets: []types.TargetOutcome{{ID: "target", Status: types.RunStatusNeedsAttention}},
				}, nil
			}, nil)
			run, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "dispatch", Trigger: "manual"})
			require.NoError(t, err)
			id := kit.SHA256Hex("0\x00" + run.JobID)
			command := types.SchedulerExecutionCommand{EnvironmentID: "0", JobID: run.JobID, RunID: run.ID, Attempt: 1}
			jobID, created, err := q.service.Dispatch(t.Context(), executorType, id, "execute", command, actor.WithIdempotencyKey(run.ID+"-1"))
			require.NoError(t, err)
			require.True(t, created)
			duplicateID, created, err := q.service.Dispatch(t.Context(), executorType, id, "execute", command, actor.WithIdempotencyKey(run.ID+"-1"))
			require.NoError(t, err)
			require.False(t, created)
			require.Equal(t, jobID, duplicateID)
			if cancel {
				_, err = q.Cancel(t.Context(), "0", run.JobID, run.ID)
				require.NoError(t, err)
			}
			require.NoError(t, q.Start(t.Context(), t.Context()))
			q.Activate()
			t.Cleanup(func() { require.NoError(t, q.Stop(context.WithoutCancel(t.Context()))) })
			require.Eventually(t, func() bool {
				info, getJobErr := q.service.GetJob(t.Context(), jobID)
				return getJobErr == nil && info.Status.IsTerminal() || cancel && errors.Is(getJobErr, actor.ErrJobNotFound)
			}, 5*time.Second, 10*time.Millisecond)
			persisted, err := q.Get(t.Context(), "0", run.JobID, run.ID)
			require.NoError(t, err)
			if cancel {
				require.Equal(t, types.RunStatusCanceled, persisted.Status)
				require.Zero(t, executed.Load())
				require.Zero(t, persisted.AttemptCount)
			} else {
				require.Equal(t, types.RunStatusFailed, persisted.Status)
				require.Equal(t, "Completion could not be confirmed", persisted.Outcome.Message)
				require.Equal(t, types.RunStatusNeedsAttention, persisted.Outcome.Targets[0].Status)
				require.NotNil(t, persisted.FinishedAt)
				require.Equal(t, int32(1), executed.Load())
				require.Equal(t, 1, persisted.AttemptCount)
			}
		})
	}
}

func TestCapacityGroupsReserveHealthSlots(t *testing.T) {
	q := newTestCoordinator(t)
	var ordinary, health atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	q.SetExecutor(func(ctx context.Context, run types.SchedulerRun) (types.SchedulerOutcome, error) {
		counter := &ordinary
		if strings.HasPrefix(run.JobID, "environment-health") {
			counter = &health
		}
		counter.Add(1)
		defer counter.Add(-1)
		select {
		case <-ctx.Done():
			return types.SchedulerOutcome{}, ctx.Err()
		case <-release:
			return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
		}
	}, nil)
	for _, job := range []string{"one", "two", "three", "four", "five", "six", "environment-health-one", "environment-health-two"} {
		_, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: job, Trigger: "manual"})
		require.NoError(t, err)
	}
	require.NoError(t, q.Start(t.Context(), t.Context()))
	q.Activate()
	t.Cleanup(func() {
		once.Do(func() { close(release) })
		require.NoError(t, q.Stop(context.WithoutCancel(t.Context())))
	})
	require.Eventually(t, func() bool { return ordinary.Load() == 4 && health.Load() == 2 }, 5*time.Second, 10*time.Millisecond)
	once.Do(func() { close(release) })
	require.Eventually(t, func() bool {
		history, err := q.List(t.Context(), "0", "", 1, 20)
		if err != nil {
			return false
		}
		for _, run := range history.Runs {
			if run.Status != types.RunStatusSucceeded {
				return false
			}
		}
		return history.Total == 8
	}, 5*time.Second, 10*time.Millisecond)
}

func TestReconciliationRepairsMissingAlarm(t *testing.T) {
	q := newTestCoordinator(t)
	require.NoError(t, q.Checkpoint(t.Context(), "repair", "0 * * * * *", time.Now().UTC().Add(time.Hour)))
	q.enabled.Store(true)
	defer q.enabled.Store(false)
	record, err := q.ScheduleState(t.Context(), "repair")
	require.NoError(t, err)
	id := kit.SHA256Hex("0\x00repair")
	_, err = q.repair(t.Context(), coordinatorStates(t, q), true)
	require.NoError(t, err)
	require.NoError(t, q.service.DeleteAlarm(t.Context(), coordinatorType, id, fmt.Sprintf("schedule-%d-%d", record.Generation, record.Sequence)))
	_, err = q.repair(t.Context(), coordinatorStates(t, q), true)
	require.NoError(t, err)
	require.NoError(t, q.service.DeleteAlarm(t.Context(), coordinatorType, id, fmt.Sprintf("schedule-%d-%d", record.Generation, record.Sequence)))
	after, err := q.ScheduleState(t.Context(), "repair")
	require.NoError(t, err)
	require.Equal(t, record.Generation, after.Generation)
	require.Equal(t, record.Sequence, after.Sequence)
	require.Empty(t, after.Runs)
}

func TestAdmissionAndCompletionRejectStaleTokens(t *testing.T) {
	host := francistest.New(t)
	first := NewAdmission(host.Service(), "first")
	require.NoError(t, first.Register(host))
	francistest.Start(t, host)
	key := types.SchedulerAdmissionKey{Scope: "volume-backup", ID: "shared"}
	old, admitted, err := first.TryAcquire(t.Context(), key)
	require.NoError(t, err)
	require.True(t, admitted)
	_, admitted, err = first.TryAcquire(t.Context(), key)
	require.NoError(t, err)
	require.False(t, admitted)
	restarted := NewAdmission(host.Service(), "next")
	current, admitted, err := restarted.TryAcquire(t.Context(), key)
	require.NoError(t, err)
	require.True(t, admitted)
	old.Release(t.Context())
	_, admitted, err = restarted.TryAcquire(t.Context(), key)
	require.NoError(t, err)
	require.False(t, admitted)
	current.Release(t.Context())
	replacement, admitted, err := restarted.TryAcquire(t.Context(), key)
	require.NoError(t, err)
	require.True(t, admitted)
	replacement.Release(t.Context())

	q := newTestCoordinator(t)
	run, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "tokens"})
	require.NoError(t, err)
	require.NoError(t, q.UpdateRun(t.Context(), run, func(current *types.SchedulerRun) error {
		current.Status = types.RunStatusRunning
		current.Owner = "new-token"
		return nil
	}))
	run.Owner = "stale-token"
	require.NoError(t, q.persistOutcome(t.Context(), run, types.SchedulerOutcome{Status: types.RunStatusSucceeded}))
	persisted, err := q.Get(t.Context(), "0", run.JobID, run.ID)
	require.NoError(t, err)
	require.Equal(t, types.RunStatusRunning, persisted.Status)
	require.Equal(t, "new-token", persisted.Owner)
}

func TestAdmissionAcquireWaitsForRelease(t *testing.T) {
	host := francistest.New(t)
	admission := NewAdmission(host.Service(), "epoch")
	require.NoError(t, admission.Register(host))
	francistest.Start(t, host)
	key := types.SchedulerAdmissionKey{Scope: "updater", ID: "container:solo"}
	held, admitted, err := admission.TryAcquire(t.Context(), key)
	require.NoError(t, err)
	require.True(t, admitted)

	canceledCtx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err = admission.Acquire(canceledCtx, key)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	acquired := make(chan *Lease, 1)
	go func() {
		lease, acquireErr := admission.Acquire(t.Context(), key)
		if acquireErr != nil {
			lease = nil
		}
		acquired <- lease
	}()
	select {
	case <-acquired:
		require.FailNow(t, "acquire returned while the key was held")
	case <-time.After(400 * time.Millisecond):
	}
	held.Release(t.Context())
	select {
	case lease := <-acquired:
		require.NotNil(t, lease)
		lease.Release(t.Context())
	case <-time.After(5 * time.Second):
		require.FailNow(t, "acquire was not admitted after release")
	}
}

func TestLostDispatchAcknowledgementRetainsOneExecution(t *testing.T) {
	q := newTestCoordinator(t)
	entered := make(chan struct{})
	finish := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(finish) }) })
	q.SetExecutor(func(ctx context.Context, _ types.SchedulerRun) (types.SchedulerOutcome, error) {
		close(entered)
		select {
		case <-ctx.Done():
			return types.SchedulerOutcome{}, ctx.Err()
		case <-finish:
			return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
		}
	}, nil)
	run, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "lost-ack"})
	require.NoError(t, err)
	require.NoError(t, q.Start(t.Context(), t.Context()))
	q.Activate()
	t.Cleanup(func() {
		once.Do(func() { close(finish) })
		require.NoError(t, q.Stop(context.WithoutCancel(t.Context())))
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not start")
	}
	id := kit.SHA256Hex("0\x00" + run.JobID)
	var state types.SchedulerCoordinatorState
	require.NoError(t, q.service.GetState(t.Context(), coordinatorType, id, &state))
	key := run.ID + "-1"
	original := state.Dispatches[key].JobID
	require.NotEmpty(t, original)
	intent := state.Dispatches[key]
	intent.JobID = ""
	state.Dispatches[key] = intent
	state.Revision++
	require.NoError(t, q.service.SetState(t.Context(), coordinatorType, id, state, nil))
	_, err = q.repair(t.Context(), coordinatorStates(t, q), false)
	require.NoError(t, err)
	require.NoError(t, q.service.GetState(t.Context(), coordinatorType, id, &state))
	require.Equal(t, original, state.Dispatches[key].JobID)
	deliveries, err := q.service.ListJobs(t.Context(), executorType, id)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	once.Do(func() { close(finish) })
	require.Eventually(t, func() bool {
		stored, getErr := q.Get(t.Context(), "0", run.JobID, run.ID)
		return getErr == nil && stored.Status == types.RunStatusSucceeded
	}, 5*time.Second, 10*time.Millisecond)
}
