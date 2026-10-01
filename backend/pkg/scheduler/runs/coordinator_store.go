package runs

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"slices"
	"sort"
	"time"
	"uuid"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/italypaleale/francis/actor"
	kit "go.getarcane.app/kit/pkg"
)

const queuePrefixInternal = "jobs."

func (q *Coordinator) mutateInternal(ctx context.Context, environmentID, jobID string, change func(*st.QueueRecord) error) error {
	id := kit.SHA256Hex(environmentID + "\x00" + jobID)
	for range 20 {
		var state st.CoordinatorState
		err := q.service.GetState(ctx, coordinatorTypeInternal, id, &state)
		if err != nil && !errors.Is(err, actor.ErrStateNotFound) {
			return err
		}
		if err := q.prepareMutationInternal(&state, environmentID, jobID, change); err != nil {
			return err
		}
		result, err := q.service.Invoke(ctx, coordinatorTypeInternal, id, "replace", state)
		if err != nil {
			return err
		}
		var swapped bool
		if err = result.Decode(&swapped); err != nil {
			return err
		}
		if swapped {
			return nil
		}
	}
	return errors.New("job coordinator state is busy; retry mutation")
}

func (q *Coordinator) prepareMutationInternal(state *st.CoordinatorState, environmentID, jobID string, change func(*st.QueueRecord) error) error {
	normalizeRecordTimesInternal(&state.Record)
	previousRuns := slices.Clone(state.Record.Runs)
	previous, versions, err := snapshotRunsInternal(state.Record)
	if err != nil {
		return err
	}
	if state.Record.JobID == "" {
		state.Record = st.QueueRecord{JobID: jobID, EnvironmentID: environmentID}
	}
	if err := change(&state.Record); err != nil {
		return err
	}
	preserveUnsyncedRunsInternal(state, previousRuns)
	for i := range state.Record.Runs {
		q.associateActivityInternal(&state.Record.Runs[i])
		if err := markChangedActivityInternal(state, &state.Record.Runs[i], previous, versions); err != nil {
			return err
		}
	}
	for id, run := range state.Record.Receipts {
		q.associateActivityInternal(&run)
		if err := markChangedActivityInternal(state, &run, previous, versions); err != nil {
			return err
		}
		state.Record.Receipts[id] = run
	}
	pruneDispatchesInternal(state)
	return nil
}

func snapshotRunsInternal(record st.QueueRecord) (map[string][]byte, map[string]time.Time, error) {
	encoded := make(map[string][]byte, len(record.Runs)+len(record.Receipts))
	versions := make(map[string]time.Time, len(record.Runs)+len(record.Receipts))
	for _, run := range append(slices.Clone(record.Runs), receiptRunsInternal(record.Receipts)...) {
		data, err := json.Marshal(run)
		if err != nil {
			return nil, nil, err
		}
		encoded[run.ID] = data
		versions[run.ID] = run.UpdatedAt
	}
	return encoded, versions, nil
}

func preserveUnsyncedRunsInternal(state *st.CoordinatorState, previous []st.Run) {
	retained := make(map[string]bool, len(state.Record.Runs))
	for _, run := range state.Record.Runs {
		retained[run.ID] = true
	}
	for _, run := range previous {
		if _, pending := state.PendingActivitySync[run.ID]; pending && !retained[run.ID] {
			state.Record.Runs = append(state.Record.Runs, run)
			delete(state.Record.Receipts, run.ID)
		}
	}
}

func pruneDispatchesInternal(state *st.CoordinatorState) {
	attempts := make(map[string]int, len(state.Record.Runs))
	for _, run := range state.Record.Runs {
		attempts[run.ID] = run.AttemptCount
	}
	for key, intent := range state.Dispatches {
		attempt, retained := attempts[intent.RunID]
		if !retained || intent.Attempt < attempt-99 {
			delete(state.Dispatches, key)
		}
	}
}

func (q *Coordinator) Records(ctx context.Context) ([]st.QueueRecord, error) {
	records := []st.QueueRecord{}
	cursor := ""
	for {
		page, err := q.service.ListStates(ctx, coordinatorTypeInternal, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, entry := range page.States {
			var state st.CoordinatorState
			if err := entry.Data.Decode(&state); err != nil {
				return nil, err
			}
			normalizeRecordTimesInternal(&state.Record)
			records = append(records, state.Record)
		}
		cursor = page.AfterID()
		if cursor == "" {
			return records, nil
		}
	}
}

// Get retrieves a run by environment, job, and run ID, including compacted
// idempotency receipts. It returns ErrRunNotFound when no matching record exists.
func (q *Coordinator) Get(ctx context.Context, environmentID, jobID, runID string) (st.Run, error) {
	var state st.CoordinatorState
	err := q.service.GetState(ctx, coordinatorTypeInternal, kit.SHA256Hex(environmentID+"\x00"+jobID), &state)
	if errors.Is(err, actor.ErrStateNotFound) {
		return st.Run{}, ErrRunNotFound
	}
	if err != nil {
		return st.Run{}, err
	}
	normalizeRecordTimesInternal(&state.Record)
	record := state.Record
	for _, run := range record.Runs {
		if run.ID == runID {
			q.associateActivityInternal(&run)
			return run, nil
		}
	}
	if run, ok := record.Receipts[runID]; ok {
		q.associateActivityInternal(&run)
		return run, nil
	}
	return st.Run{}, ErrRunNotFound
}

// List returns retained run history newest first, excluding compacted receipts.
// An empty job ID includes every job in the environment; pages are one-based.
func (q *Coordinator) List(ctx context.Context, environmentID, jobID string, page, limit int) (st.RunList, error) {
	records, err := q.Records(ctx)
	if err != nil {
		return st.RunList{}, err
	}
	runs := []st.Run{}
	for _, record := range records {
		if record.EnvironmentID == environmentID && (jobID == "" || record.JobID == jobID) {
			runs = append(runs, record.Runs...)
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	page = max(1, page)
	limit = min(100, max(1, limit))
	start := len(runs)
	if page-1 <= len(runs)/limit {
		start = min(len(runs), (page-1)*limit)
	}
	end := min(len(runs), start+limit)
	return st.RunList{Runs: runs[start:end], Total: len(runs), Page: page, Limit: limit}, nil
}

// UpdateRun atomically changes a persisted run or terminal receipt using compare
// and swap. The callback may run more than once and must not perform external side effects.
func (q *Coordinator) UpdateRun(ctx context.Context, run st.Run, change func(*st.Run) error) error {
	wake := false
	err := q.mutateInternal(ctx, run.EnvironmentID, run.JobID, func(record *st.QueueRecord) error {
		for index := range record.Runs {
			if record.Runs[index].ID == run.ID {
				previous := record.Runs[index].Status
				if err := change(&record.Runs[index]); err != nil {
					return err
				}
				wake = previous != st.Queued && record.Runs[index].Status == st.Queued
				return nil
			}
		}
		if receipt, found := record.Receipts[run.ID]; found {
			if err := change(&receipt); err != nil {
				return err
			}
			if !receipt.Status.Terminal() {
				return errors.New("archived runs cannot be restarted")
			}
			record.Receipts[run.ID] = receipt
			return nil
		}
		return ErrRunNotFound
	})
	if err == nil && wake {
		q.signalInternal()
	}
	if err == nil {
		q.observeRunInternal(ctx, run)
	}
	return err
}

func (q *Coordinator) pruneInternal(ctx context.Context) error {
	records, err := q.Records(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, record := range records {
		if err := q.mutateInternal(ctx, record.EnvironmentID, record.JobID, func(current *st.QueueRecord) error {
			pruneRunsInternal(current, now)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func pruneRunsInternal(record *st.QueueRecord, now time.Time) {
	terminal := 0
	for index, run := range slices.Backward(record.Runs) {
		if !run.Status.Terminal() || ((run.RemoteDeliveryAttempted || run.RemoteAccepted) && !run.RemoteSettled) {
			continue
		}
		terminal++
		if terminal <= 100 && now.Sub(run.UpdatedAt) < 7*24*time.Hour {
			continue
		}
		if run.Trigger == "manual" || run.Trigger == "remote" {
			if record.Receipts == nil {
				record.Receipts = make(map[string]st.Run)
			}
			run.Attempts = nil
			run.Outcome.Targets = nil
			record.Receipts[run.ID] = run
		}
		record.Runs = append(record.Runs[:index], record.Runs[index+1:]...)
	}
}

// ErrRunNotFound indicates that neither a run nor its receipt exists.
var ErrRunNotFound = errors.Sentinel("job run not found")

// ErrRunConflict indicates that execution ownership or state changed.
var ErrRunConflict = errors.Sentinel("job run state changed")

func New(store *kv.KVService, service *actor.Service, location *time.Location) *Coordinator {
	if location == nil {
		location = time.UTC
	}
	return &Coordinator{store: store, service: service, location: location, owner: uuid.New().String(), checkpointed: make(map[string]bool), wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func (q *Coordinator) SetExecutor(execute, reconcile func(context.Context, st.Run) (st.Outcome, error)) {
	q.execute = execute
	q.reconcile = reconcile
}

func (q *Coordinator) ScheduleState(ctx context.Context, jobID string) (st.QueueRecord, error) {
	var state st.CoordinatorState
	err := q.service.GetState(ctx, coordinatorTypeInternal, kit.SHA256Hex("0"+"\x00"+jobID), &state)
	normalizeRecordTimesInternal(&state.Record)
	return state.Record, err
}

func (q *Coordinator) Activate() { q.enabled.Store(true); q.signalInternal() }

func receiptRunsInternal(receipts map[string]st.Run) []st.Run {
	runs := make([]st.Run, 0, len(receipts))
	for _, run := range receipts {
		runs = append(runs, run)
	}
	return runs
}

func markChangedActivityInternal(state *st.CoordinatorState, run *st.Run, previous map[string][]byte, previousUpdated map[string]time.Time) error {
	normalizeRunTimesInternal(run)
	if run.ActivityID == "" {
		return nil
	}
	encoded, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if bytes.Equal(previous[run.ID], encoded) {
		return nil
	}
	if run.UpdatedAt.IsZero() || run.UpdatedAt.Equal(previousUpdated[run.ID]) {
		run.UpdatedAt = time.Now().UTC()
	}
	if state.PendingActivitySync == nil {
		state.PendingActivitySync = make(map[string]time.Time)
	}
	state.PendingActivitySync[run.ID] = run.UpdatedAt
	return nil
}

func markActivitySyncInternal(state *st.CoordinatorState, run st.Run) {
	if run.ActivityID == "" {
		return
	}
	if state.PendingActivitySync == nil {
		state.PendingActivitySync = make(map[string]time.Time)
	}
	state.PendingActivitySync[run.ID] = run.UpdatedAt
}

func normalizeRecordTimesInternal(record *st.QueueRecord) {
	record.LastEnqueuedAt = record.LastEnqueuedAt.UTC()
	record.NextRun = record.NextRun.UTC()
	for i := range record.Runs {
		normalizeRunTimesInternal(&record.Runs[i])
	}
	for id, run := range record.Receipts {
		normalizeRunTimesInternal(&run)
		record.Receipts[id] = run
	}
}

func normalizeRunTimesInternal(run *st.Run) {
	run.CreatedAt = run.CreatedAt.UTC()
	run.UpdatedAt = run.UpdatedAt.UTC()
	normalizeTimePointerInternal(run.StartedAt)
	normalizeTimePointerInternal(run.FinishedAt)
	normalizeTimePointerInternal(run.NextAttempt)
	normalizeTimePointerInternal(run.LastConfirmedAt)
	if run.Resolution != nil {
		run.Resolution.ResolvedAt = run.Resolution.ResolvedAt.UTC()
	}
	for i := range run.Attempts {
		run.Attempts[i].StartedAt = run.Attempts[i].StartedAt.UTC()
		normalizeTimePointerInternal(run.Attempts[i].FinishedAt)
	}
}

func normalizeTimePointerInternal(value *time.Time) {
	if value != nil {
		*value = value.UTC()
	}
}
