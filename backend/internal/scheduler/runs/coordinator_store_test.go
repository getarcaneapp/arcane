package runs

import (
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"kit.getarcane.app"

	"getarcane.dev/types/v3"
)

func benchmarkRun(index int, created time.Time) types.SchedulerRun {
	attempts := make([]types.SchedulerAttempt, 0, 20)
	targets := make([]types.TargetOutcome, 0, 10)
	for target := range 10 {
		targets = append(targets, types.TargetOutcome{
			ResourceType: "container",
			ID:           fmt.Sprintf("target-%d-%d", index, target),
			Status:       types.RunStatusSucceeded,
			Message:      "updated",
		})
	}
	for attempt := range 20 {
		attempts = append(attempts, types.SchedulerAttempt{
			StartedAt:  created,
			FinishedAt: new(created),
			Outcome: types.SchedulerOutcome{
				Status:  types.RunStatusSucceeded,
				Message: "attempt finished",
				Targets: targets,
			},
			Number: attempt + 1,
		})
	}
	id := fmt.Sprintf("run-%03d", index)
	return types.SchedulerRun{
		CreatedAt:             created,
		UpdatedAt:             created,
		FinishedAt:            new(created),
		ActivityEnvironmentID: "0",
		ActivityID:            id,
		ID:                    id,
		JobID:                 "bench",
		EnvironmentID:         "0",
		Trigger:               "scheduled",
		Status:                types.RunStatusSucceeded,
		Outcome: types.SchedulerOutcome{
			Status:  types.RunStatusSucceeded,
			Targets: targets,
		},
		Attempts:     attempts,
		AttemptCount: 20,
	}
}

func benchmarkState(runs, receipts int) types.SchedulerCoordinatorState {
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	record := types.SchedulerQueueRecord{
		Receipts:      make(map[string]types.SchedulerRun, receipts),
		JobID:         "bench",
		EnvironmentID: "0",
		Schedule:      "@every 5m",
	}
	for index := range runs {
		record.Runs = append(record.Runs, benchmarkRun(index, created))
	}
	for index := range receipts {
		run := benchmarkRun(runs+index, created)
		record.Receipts[run.ID] = types.SchedulerRun{
			CreatedAt:             run.CreatedAt,
			UpdatedAt:             run.UpdatedAt,
			ActivityEnvironmentID: run.ActivityEnvironmentID,
			ActivityID:            run.ActivityID,
			ID:                    run.ID,
			JobID:                 run.JobID,
			EnvironmentID:         run.EnvironmentID,
			Trigger:               "manual",
			Status:                run.Status,
		}
	}
	return types.SchedulerCoordinatorState{Record: record, Revision: 1}
}

func TestPrepareMutationMarksOnlyChangedActivity(t *testing.T) {
	q := &Coordinator{}
	state := benchmarkState(3, 2)
	state.Record.Runs[1].Status = types.RunStatusRunning
	unchanged := state.Record.Runs[0].UpdatedAt
	finished := unchanged.Add(time.Minute)
	explicit := unchanged.Add(2 * time.Minute)
	require.NoError(t, q.prepareMutation(&state, "0", "bench", func(record *types.SchedulerQueueRecord) error {
		record.Runs[1].Attempts[19].FinishedAt = &finished
		record.Runs[2].Status = types.RunStatusFailed
		record.Runs[2].UpdatedAt = explicit
		return nil
	}))
	require.Equal(t, []string{"run-001", "run-002"}, slices.Sorted(maps.Keys(state.PendingActivitySync)))
	require.True(t, state.Record.Runs[1].UpdatedAt.After(unchanged))
	require.Equal(t, state.Record.Runs[1].UpdatedAt, state.PendingActivitySync["run-001"])
	require.Equal(t, explicit, state.Record.Runs[2].UpdatedAt)
	require.Equal(t, explicit, state.PendingActivitySync["run-002"])
	require.Equal(t, unchanged, state.Record.Runs[0].UpdatedAt)
}

func TestPrepareMutationMarksNewAndReassociatedRuns(t *testing.T) {
	q := &Coordinator{observer: &activityObserverFake{}}
	state := benchmarkState(1, 0)
	state.Record.Runs[0].ActivityID = ""
	created := time.Now().UTC()
	require.NoError(t, q.prepareMutation(&state, "0", "bench", func(record *types.SchedulerQueueRecord) error {
		record.Runs = append(record.Runs, types.SchedulerRun{
			ID:            "fresh",
			JobID:         "bench",
			EnvironmentID: "0",
			Status:        types.RunStatusQueued,
			CreatedAt:     created,
			UpdatedAt:     created,
		})
		return nil
	}))
	require.Equal(t, []string{"fresh", "run-000"}, slices.Sorted(maps.Keys(state.PendingActivitySync)))
	require.Equal(t, created, state.PendingActivitySync["fresh"])
}

func TestPrepareMutationRestoresPendingRuns(t *testing.T) {
	q := &Coordinator{}
	state := benchmarkState(3, 0)
	state.Record.Runs[0].Status = types.RunStatusRunning
	state.PendingActivitySync = map[string]time.Time{
		"run-000": state.Record.Runs[0].UpdatedAt,
		"run-001": state.Record.Runs[1].UpdatedAt,
	}
	require.NoError(t, q.prepareMutation(&state, "0", "bench", func(record *types.SchedulerQueueRecord) error {
		record.Runs[0].Status = types.RunStatusSucceeded
		record.Runs = record.Runs[2:]
		return nil
	}))
	require.Len(t, state.Record.Runs, 2)
	require.Equal(t, "run-000", state.Record.Runs[0].ID)
	require.Equal(t, types.RunStatusRunning, state.Record.Runs[0].Status)
	require.Equal(t, "run-002", state.Record.Runs[1].ID)
	require.Equal(t, []string{"run-000"}, slices.Sorted(maps.Keys(state.PendingActivitySync)))
}

func TestPrepareMutationDropsRunsWithoutPendingActivity(t *testing.T) {
	q := &Coordinator{}
	state := benchmarkState(3, 0)
	state.Record.Runs[0].Status = types.RunStatusRunning
	require.NoError(t, q.prepareMutation(&state, "0", "bench", func(record *types.SchedulerQueueRecord) error {
		record.Runs = record.Runs[1:]
		return nil
	}))
	require.Equal(t, []string{"run-001", "run-002"}, []string{state.Record.Runs[0].ID, state.Record.Runs[1].ID})
	require.Empty(t, state.PendingActivitySync)
}

func BenchmarkPrepareMutation(b *testing.B) {
	q := &Coordinator{}
	state := benchmarkState(100, 50)
	b.ReportAllocs()
	for b.Loop() {
		clear(state.PendingActivitySync)
		if err := q.prepareMutation(&state, "0", "bench", func(record *types.SchedulerQueueRecord) error {
			record.Runs[99].Outcome.Message = time.Now().String()
			return nil
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUpdateRun(b *testing.B) {
	q := newTestCoordinator(b)
	q.SetObserver(&activityObserverFake{existingOnly: true})
	state := benchmarkState(100, 50)
	require.NoError(b, q.service.SetState(b.Context(), coordinatorType, kit.SHA256Hex("0\x00bench"), state, nil))
	run := state.Record.Runs[99]
	b.ReportAllocs()
	for b.Loop() {
		if err := q.UpdateRun(b.Context(), run, func(current *types.SchedulerRun) error {
			current.Outcome.Message = time.Now().String()
			return nil
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSummariesMatchFullStates(t *testing.T) {
	q := newTestCoordinator(t)
	observer := &activityObserverFake{}
	observer.unavailable.Store(true)
	q.SetObserver(observer)
	_, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "local-job", Trigger: "manual"})
	require.NoError(t, err)
	_, err = q.Submit(t.Context(), types.SchedulerRequest{JobID: "remote-job", EnvironmentID: "e1", Trigger: "scheduled"})
	require.NoError(t, err)

	states := coordinatorStates(t, q)
	want := make([]coordinatorSummary, 0, len(states))
	for _, state := range states {
		runs := make([]runSummary, 0, len(state.Record.Runs))
		for _, run := range state.Record.Runs {
			runs = append(runs, runSummary{Status: run.Status})
		}
		want = append(want, coordinatorSummary{
			PendingActivitySync: state.PendingActivitySync,
			Record: recordSummary{
				EnvironmentID: state.Record.EnvironmentID,
				JobID:         state.Record.JobID,
				Runs:          runs,
			},
		})
	}
	require.Len(t, want, 2)
	require.True(t, slices.ContainsFunc(want, func(summary coordinatorSummary) bool { return len(summary.PendingActivitySync) > 0 }))
	require.ElementsMatch(t, want, coordinatorSummaries(t, q))
}

func TestRecordReadsOnlyRequestedJob(t *testing.T) {
	q := newTestCoordinator(t)
	local, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "shared-job", Trigger: "manual"})
	require.NoError(t, err)
	_, err = q.Submit(t.Context(), types.SchedulerRequest{JobID: "shared-job", EnvironmentID: "e1", Trigger: "manual"})
	require.NoError(t, err)
	_, err = q.Submit(t.Context(), types.SchedulerRequest{JobID: "other-job", Trigger: "manual"})
	require.NoError(t, err)

	record, err := q.Record(t.Context(), "0", "shared-job")
	require.NoError(t, err)
	require.Equal(t, "0", record.EnvironmentID)
	require.Len(t, record.Runs, 1)
	require.Equal(t, local.ID, record.Runs[0].ID)

	missing, err := q.Record(t.Context(), "e2", "shared-job")
	require.NoError(t, err)
	require.Empty(t, missing.Runs)

	list, err := q.List(t.Context(), "0", "shared-job", 1, 100)
	require.NoError(t, err)
	require.Equal(t, 1, list.Total)
	require.Equal(t, local.ID, list.Runs[0].ID)

	all, err := q.List(t.Context(), "0", "", 1, 100)
	require.NoError(t, err)
	require.Equal(t, 2, all.Total)
}

func TestPruneRunsCompactsOlderSucceededRuns(t *testing.T) {
	q := &Coordinator{}
	state := benchmarkState(retainedRuns+5, 0)
	failed := retainedRuns - detailedRuns - 2
	state.Record.Runs[failed].Status = types.RunStatusFailed
	require.NoError(t, q.prepareMutation(&state, "0", "bench", func(record *types.SchedulerQueueRecord) error {
		require.True(t, pruneRuns(record, time.Now().UTC()))
		return nil
	}))
	runs := state.Record.Runs
	require.Len(t, runs, retainedRuns)
	for index, run := range runs {
		detailed := index >= len(runs)-detailedRuns || run.Status == types.RunStatusFailed
		require.Equal(t, detailed, run.Attempts != nil, run.ID)
		require.Equal(t, detailed, run.Outcome.Targets != nil, run.ID)
	}
	require.Empty(t, state.PendingActivitySync)
}
