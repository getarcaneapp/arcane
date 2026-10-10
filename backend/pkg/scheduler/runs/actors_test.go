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

	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
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
	legacy := st.QueueRecord{
		JobID:         "legacy",
		EnvironmentID: "0",
		Runs: []st.Run{{
			ID:            uuid.New().String(),
			JobID:         "legacy",
			EnvironmentID: "0",
			Trigger:       "manual",
			Status:        st.Succeeded,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		}},
	}
	legacy.Runs[0].ActivityID = legacy.Runs[0].ID
	for _, jobID := range []string{"gitops-sync:first", "gitops-sync:second"} {
		record := st.QueueRecord{JobID: jobID, EnvironmentID: "0", Schedule: "@every 5m", NextRun: time.Now().UTC().Add(time.Hour)}
		for i := range 1600 {
			created := time.Now().UTC().Add(-time.Duration(1600-i) * 5 * time.Minute)
			record.Runs = append(
				record.Runs,
				st.Run{
					ID:            uuid.New().String(),
					ActivityID:    uuid.New().String(),
					JobID:         jobID,
					EnvironmentID: "0",
					Trigger:       "scheduled",
					Status:        st.NeedsAttention,
					CreatedAt:     created,
					UpdatedAt:     created,
				},
			)
		}
		data, marshalErr := json.Marshal(record)
		require.NoError(t, marshalErr)
		require.NoError(t, store.Set(t.Context(), queuePrefixInternal+kit.SHA256Hex("0\x00"+jobID), string(data)))
	}
	encoded, err := json.Marshal(legacy)
	require.NoError(t, err)
	key := queuePrefixInternal + kit.SHA256Hex("0\x00"+legacy.JobID)
	require.NoError(t, store.Set(t.Context(), key, string(encoded)))
	databaseURL := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "actors.db"))
	first := francistest.New(t, databaseURL)
	q := New(store, first.Service(), time.UTC)
	require.NoError(t, q.Register(first))
	francistest.Start(t, first)
	require.NoError(t, q.importLegacyInternal(t.Context()))
	require.NoError(t, q.importLegacyInternal(t.Context()))
	observer := &activityObserverFakeInternal{existingOnly: true}
	observer.unavailable.Store(true)
	q.SetObserver(observer)
	_, err = q.Submit(t.Context(), st.Request{JobID: legacy.JobID, RunID: legacy.Runs[0].ID, Trigger: "manual"})
	require.NoError(t, err)
	require.NoError(t, q.UpdateRun(t.Context(), legacy.Runs[0], func(run *st.Run) error {
		run.Outcome.Targets = []st.TargetOutcome{{ID: "artifact", Status: st.Succeeded, RecoveryData: jsontext.Value(`{"backupId":"artifact"}`)}}
		return nil
	}))
	require.True(t, q.hasPendingActivitySyncInternal(t.Context()))
	q.SetExecutor(func(context.Context, st.Run) (st.Outcome, error) {
		t.Error("startup must not execute jobs before activation")
		return st.Outcome{Status: st.Failed}, nil
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
	require.NoError(t, restored.importLegacyInternal(t.Context()))
	require.NoError(t, restored.Start(t.Context(), t.Context()))
	t.Cleanup(func() { require.NoError(t, restored.Stop(context.WithoutCancel(t.Context()))) })
	for _, jobID := range []string{"gitops-sync:first", "gitops-sync:second"} {
		history, listErr := restored.List(t.Context(), "0", jobID, 1, 100)
		require.NoError(t, listErr)
		require.Equal(t, 100, history.Total)
		require.Equal(t, st.Failed, history.Runs[0].Status)
		state, stateErr := restored.ScheduleState(t.Context(), jobID)
		require.NoError(t, stateErr)
		require.Equal(t, "@every 5m", state.Schedule)
		require.True(t, state.NextRun.After(time.Now()))
	}
	duplicate, err := restored.Submit(t.Context(), st.Request{JobID: legacy.JobID, RunID: legacy.Runs[0].ID, Trigger: "manual"})
	require.NoError(t, err)
	require.Equal(t, st.Succeeded, duplicate.Status)
	require.Equal(t, jsontext.Value(`{"backupId":"artifact"}`), duplicate.Outcome.Targets[0].RecoveryData)
	history, err := restored.List(t.Context(), "0", legacy.JobID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, history.Total)
	require.Equal(t, time.UTC, history.Runs[0].CreatedAt.Location())
	observer.unavailable.Store(false)
	restored.retryActivitySyncInternal(t.Context())
	require.False(t, restored.hasPendingActivitySyncInternal(t.Context()))
	require.Positive(t, observer.observed.Load())
	_, exists, err := store.Get(t.Context(), key)
	require.NoError(t, err)
	require.True(t, exists)
}

type occurrenceEnvelopeInternal struct{ occurrence st.ScheduleOccurrence }

func (e occurrenceEnvelopeInternal) Decode(value any) error {
	*value.(*st.ScheduleOccurrence) = e.occurrence
	return nil
}

func TestAlarmIgnoresStaleGeneration(t *testing.T) {
	q := newResolutionQueueInternal(t)
	next := time.Now().UTC().Add(time.Hour)
	require.NoError(t, q.Checkpoint(t.Context(), "schedule", "0 * * * * *", next))
	before, err := q.ScheduleState(t.Context(), "schedule")
	require.NoError(t, err)
	q.enabled.Store(true)
	defer q.enabled.Store(false)
	coordinator := &coordinatorActorInternal{id: kit.SHA256Hex("0\x00schedule"), q: q}
	require.NoError(t, coordinator.Alarm(t.Context(), "old", occurrenceEnvelopeInternal{st.ScheduleOccurrence{Generation: before.Generation - 1, Sequence: before.Sequence}}))
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
			q := newResolutionQueueInternal(t)
			var executed atomic.Int32
			q.SetExecutor(func(context.Context, st.Run) (st.Outcome, error) {
				executed.Add(1)
				return st.Outcome{Status: st.NeedsAttention, Message: "Completion could not be confirmed", Targets: []st.TargetOutcome{{ID: "target", Status: st.NeedsAttention}}}, nil
			}, nil)
			run, err := q.Submit(t.Context(), st.Request{JobID: "dispatch", Trigger: "manual"})
			require.NoError(t, err)
			id := kit.SHA256Hex("0\x00" + run.JobID)
			command := st.ExecutionCommand{EnvironmentID: "0", JobID: run.JobID, RunID: run.ID, Attempt: 1}
			jobID, created, err := q.service.Dispatch(t.Context(), executorTypeInternal, id, "execute", command, actor.WithIdempotencyKey(run.ID+"-1"))
			require.NoError(t, err)
			require.True(t, created)
			duplicateID, created, err := q.service.Dispatch(t.Context(), executorTypeInternal, id, "execute", command, actor.WithIdempotencyKey(run.ID+"-1"))
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
				require.Equal(t, st.Canceled, persisted.Status)
				require.Zero(t, executed.Load())
				require.Zero(t, persisted.AttemptCount)
			} else {
				require.Equal(t, st.Failed, persisted.Status)
				require.Equal(t, "Completion could not be confirmed", persisted.Outcome.Message)
				require.Equal(t, st.NeedsAttention, persisted.Outcome.Targets[0].Status)
				require.NotNil(t, persisted.FinishedAt)
				require.Equal(t, int32(1), executed.Load())
				require.Equal(t, 1, persisted.AttemptCount)
			}
		})
	}
}

func TestCapacityGroupsReserveHealthSlots(t *testing.T) {
	q := newResolutionQueueInternal(t)
	var ordinary, health atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	q.SetExecutor(func(ctx context.Context, run st.Run) (st.Outcome, error) {
		counter := &ordinary
		if strings.HasPrefix(run.JobID, "environment-health") {
			counter = &health
		}
		counter.Add(1)
		defer counter.Add(-1)
		select {
		case <-ctx.Done():
			return st.Outcome{}, ctx.Err()
		case <-release:
			return st.Outcome{Status: st.Succeeded}, nil
		}
	}, nil)
	for _, job := range []string{"one", "two", "three", "four", "five", "six", "environment-health-one", "environment-health-two"} {
		_, err := q.Submit(t.Context(), st.Request{JobID: job, Trigger: "manual"})
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
			if run.Status != st.Succeeded {
				return false
			}
		}
		return history.Total == 8
	}, 5*time.Second, 10*time.Millisecond)
}

func TestReconciliationRepairsMissingAlarm(t *testing.T) {
	q := newResolutionQueueInternal(t)
	require.NoError(t, q.Checkpoint(t.Context(), "repair", "0 * * * * *", time.Now().UTC().Add(time.Hour)))
	q.enabled.Store(true)
	defer q.enabled.Store(false)
	record, err := q.ScheduleState(t.Context(), "repair")
	require.NoError(t, err)
	id := kit.SHA256Hex("0\x00repair")
	err = q.repairInternal(t.Context(), true)
	require.NoError(t, err)
	require.NoError(t, q.service.DeleteAlarm(t.Context(), coordinatorTypeInternal, id, fmt.Sprintf("schedule-%d-%d", record.Generation, record.Sequence)))
	err = q.repairInternal(t.Context(), true)
	require.NoError(t, err)
	require.NoError(t, q.service.DeleteAlarm(t.Context(), coordinatorTypeInternal, id, fmt.Sprintf("schedule-%d-%d", record.Generation, record.Sequence)))
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
	key := st.AdmissionKey{Scope: "volume-backup", ID: "shared"}
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

	q := newResolutionQueueInternal(t)
	run, err := q.Submit(t.Context(), st.Request{JobID: "tokens"})
	require.NoError(t, err)
	require.NoError(t, q.UpdateRun(t.Context(), run, func(current *st.Run) error { current.Status = st.Running; current.Owner = "new-token"; return nil }))
	run.Owner = "stale-token"
	q.persistOutcomeInternal(t.Context(), run, st.Outcome{Status: st.Succeeded})
	persisted, err := q.Get(t.Context(), "0", run.JobID, run.ID)
	require.NoError(t, err)
	require.Equal(t, st.Running, persisted.Status)
	require.Equal(t, "new-token", persisted.Owner)
}

func TestLostDispatchAcknowledgementRetainsOneExecution(t *testing.T) {
	q := newResolutionQueueInternal(t)
	entered := make(chan struct{})
	finish := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(finish) }) })
	q.SetExecutor(func(ctx context.Context, _ st.Run) (st.Outcome, error) {
		close(entered)
		select {
		case <-ctx.Done():
			return st.Outcome{}, ctx.Err()
		case <-finish:
			return st.Outcome{Status: st.Succeeded}, nil
		}
	}, nil)
	run, err := q.Submit(t.Context(), st.Request{JobID: "lost-ack"})
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
	var state st.CoordinatorState
	require.NoError(t, q.service.GetState(t.Context(), coordinatorTypeInternal, id, &state))
	key := run.ID + "-1"
	original := state.Dispatches[key].JobID
	require.NotEmpty(t, original)
	intent := state.Dispatches[key]
	intent.JobID = ""
	state.Dispatches[key] = intent
	state.Revision++
	require.NoError(t, q.service.SetState(t.Context(), coordinatorTypeInternal, id, state, nil))
	err = q.repairInternal(t.Context(), false)
	require.NoError(t, err)
	require.NoError(t, q.service.GetState(t.Context(), coordinatorTypeInternal, id, &state))
	require.Equal(t, original, state.Dispatches[key].JobID)
	deliveries, err := q.service.ListJobs(t.Context(), executorTypeInternal, id)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	once.Do(func() { close(finish) })
	require.Eventually(t, func() bool {
		stored, getErr := q.Get(t.Context(), "0", run.JobID, run.ID)
		return getErr == nil && stored.Status == st.Succeeded
	}, 5*time.Second, 10*time.Millisecond)
}
