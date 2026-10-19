package runs

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"sort"
	"time"
	"uuid"

	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/italypaleale/francis/actor"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
)

const queuePrefix = "jobs."

func (q *Coordinator) mutate(ctx context.Context, environmentID, jobID string, change func(*st.QueueRecord) error) error {
	id := kit.SHA256Hex(environmentID + "\x00" + jobID)
	q.runtime.NameActor(id, jobID+"@"+environmentID)
	for attempt := range 20 {
		if attempt > 0 {
			delay := 5 * time.Millisecond << min(attempt-1, 5)
			timer := time.NewTimer(delay/2 + rand.N(delay/2))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		var state st.CoordinatorState
		err := q.service.GetState(ctx, coordinatorType, id, &state)
		if err != nil && !errors.Is(err, actor.ErrStateNotFound) {
			return err
		}
		if prepareMutationErr := q.prepareMutation(&state, environmentID, jobID, change); prepareMutationErr != nil {
			return prepareMutationErr
		}
		result, err := q.service.Invoke(ctx, coordinatorType, id, "replace", state)
		if err != nil {
			return err
		}
		var swapped bool
		if decodeErr := result.Decode(&swapped); decodeErr != nil {
			return decodeErr
		}
		if swapped {
			return nil
		}
	}
	return common.Classify(common.ErrUnavailable, errors.New("job coordinator state is busy; retry mutation"))
}

func (q *Coordinator) prepareMutation(state *st.CoordinatorState, environmentID, jobID string, change func(*st.QueueRecord) error) error {
	normalizeRecordTimes(&state.Record)
	previousRuns := slices.Clone(state.Record.Runs)
	previous, versions, err := snapshotRuns(state.Record)
	if err != nil {
		return err
	}
	if state.Record.JobID == "" {
		state.Record = st.QueueRecord{JobID: jobID, EnvironmentID: environmentID}
	}
	if changeErr := change(&state.Record); changeErr != nil {
		return changeErr
	}
	preserveUnsyncedRuns(state, previousRuns)
	for i := range state.Record.Runs {
		q.associateActivity(&state.Record.Runs[i])
		if markChangedActivityErr := markChangedActivity(state, &state.Record.Runs[i], previous, versions); markChangedActivityErr != nil {
			return markChangedActivityErr
		}
	}
	for id, run := range state.Record.Receipts {
		q.associateActivity(&run)
		if markChangedActivityErr2 := markChangedActivity(state, &run, previous, versions); markChangedActivityErr2 != nil {
			return markChangedActivityErr2
		}
		state.Record.Receipts[id] = run
	}
	pruneDispatches(state)
	return nil
}

func snapshotRuns(record st.QueueRecord) (map[string][]byte, map[string]time.Time, error) {
	encoded := make(map[string][]byte, len(record.Runs)+len(record.Receipts))
	versions := make(map[string]time.Time, len(record.Runs)+len(record.Receipts))
	for _, run := range append(slices.Clone(record.Runs), receiptRuns(record.Receipts)...) {
		data, err := json.Marshal(run)
		if err != nil {
			return nil, nil, err
		}
		encoded[run.ID] = data
		versions[run.ID] = run.UpdatedAt
	}
	return encoded, versions, nil
}

func preserveUnsyncedRuns(state *st.CoordinatorState, previous []st.Run) {
	retained := make(map[string]st.Run, len(state.Record.Runs))
	for _, run := range state.Record.Runs {
		retained[run.ID] = run
	}
	restore := false
	for _, run := range previous {
		_, exists := retained[run.ID]
		if _, pending := state.PendingActivitySync[run.ID]; pending && !exists {
			// Activity projection must not extend completed run retention.
			if run.Status.Terminal() {
				if _, archived := state.Record.Receipts[run.ID]; !archived {
					delete(state.PendingActivitySync, run.ID)
				}
				continue
			}
			retained[run.ID] = run
			delete(state.Record.Receipts, run.ID)
			restore = true
		}
	}
	if !restore {
		return
	}
	// Keep history chronological when pending activity updates prevent pruning.
	runs := make([]st.Run, 0, len(retained))
	for _, run := range previous {
		if current, exists := retained[run.ID]; exists {
			runs = append(runs, current)
			delete(retained, run.ID)
		}
	}
	for _, run := range state.Record.Runs {
		if _, exists := retained[run.ID]; exists {
			runs = append(runs, run)
		}
	}
	state.Record.Runs = runs
}

func pruneDispatches(state *st.CoordinatorState) {
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
	states, err := q.states(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]st.QueueRecord, 0, len(states))
	for _, state := range states {
		records = append(records, state.Record)
	}
	return records, nil
}

func (q *Coordinator) states(ctx context.Context) ([]st.CoordinatorState, error) {
	states := []st.CoordinatorState{}
	cursor := ""
	for {
		page, err := q.service.ListStates(ctx, coordinatorType, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			return nil, fmt.Errorf("list job coordinator states: %w", err)
		}
		for _, entry := range page.States {
			var state st.CoordinatorState
			if decodeErr := entry.Data.Decode(&state); decodeErr != nil {
				return nil, fmt.Errorf("decode job coordinator %q: %w", entry.ActorID, decodeErr)
			}
			normalizeRecordTimes(&state.Record)
			q.runtime.NameActor(entry.ActorID, state.Record.JobID+"@"+state.Record.EnvironmentID)
			states = append(states, state)
		}
		cursor = page.AfterID()
		if cursor == "" {
			return states, nil
		}
	}
}

// Get retrieves a run by environment, job, and run ID, including compacted
// idempotency receipts. It returns ErrRunNotFound when no matching record exists.
func (q *Coordinator) Get(ctx context.Context, environmentID, jobID, runID string) (st.Run, error) {
	var state st.CoordinatorState
	err := q.service.GetState(ctx, coordinatorType, kit.SHA256Hex(environmentID+"\x00"+jobID), &state)
	if errors.Is(err, actor.ErrStateNotFound) {
		return st.Run{}, ErrRunNotFound
	}
	if err != nil {
		return st.Run{}, err
	}
	normalizeRecordTimes(&state.Record)
	record := state.Record
	for _, run := range record.Runs {
		if run.ID == runID {
			q.associateActivity(&run)
			return run, nil
		}
	}
	if run, ok := record.Receipts[runID]; ok {
		q.associateActivity(&run)
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
	err := q.mutate(ctx, run.EnvironmentID, run.JobID, func(record *st.QueueRecord) error {
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
		q.signal()
	}
	if err == nil {
		q.observeRun(ctx, run)
	}
	return err
}

func (q *Coordinator) prune(ctx context.Context, states []st.CoordinatorState) error {
	now := time.Now().UTC()
	for _, state := range states {
		record := st.QueueRecord{Runs: slices.Clone(state.Record.Runs), Receipts: maps.Clone(state.Record.Receipts)}
		if pruneRuns(&record, now) {
			if mutateErr := q.mutate(ctx, state.Record.EnvironmentID, state.Record.JobID, func(current *st.QueueRecord) error {
				pruneRuns(current, now)
				return nil
			}); mutateErr != nil {
				return fmt.Errorf("prune job %q in environment %q: %w", state.Record.JobID, state.Record.EnvironmentID, mutateErr)
			}
		}
		if state.Record.Schedule != "" || len(state.PendingActivitySync) > 0 || len(record.Runs) > 0 || len(record.Receipts) > 0 {
			continue
		}
		if _, removeErr := q.service.Invoke(ctx, coordinatorType, kit.SHA256Hex(state.Record.EnvironmentID+"\x00"+state.Record.JobID), "remove", nil); removeErr != nil {
			return fmt.Errorf("remove job %q in environment %q: %w", state.Record.JobID, state.Record.EnvironmentID, removeErr)
		}
	}
	return nil
}

func pruneRuns(record *st.QueueRecord, now time.Time) bool {
	changed := false
	terminal := 0
	for index, run := range slices.Backward(record.Runs) {
		if !run.Status.Terminal() || ((run.RemoteDeliveryAttempted || run.RemoteAccepted) && !run.RemoteSettled) {
			continue
		}
		terminal++
		if terminal <= 100 && now.Sub(run.UpdatedAt) < 7*24*time.Hour {
			continue
		}
		if separateTrigger(run.Trigger) {
			if record.Receipts == nil {
				record.Receipts = make(map[string]st.Run)
			}
			run.Attempts = nil
			run.Outcome.Targets = nil
			record.Receipts[run.ID] = run
		}
		record.Runs = append(record.Runs[:index], record.Runs[index+1:]...)
		changed = true
	}
	return changed
}

// ErrRunNotFound indicates that neither a run nor its receipt exists.
var ErrRunNotFound = errors.New("job run not found")

// ErrRunConflict indicates that execution ownership or state changed.
var ErrRunConflict = errors.New("job run state changed")

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
	err := q.service.GetState(ctx, coordinatorType, kit.SHA256Hex("0\x00"+jobID), &state)
	normalizeRecordTimes(&state.Record)
	return state.Record, err
}

func (q *Coordinator) Activate() { q.enabled.Store(true); q.signal() }

func receiptRuns(receipts map[string]st.Run) []st.Run {
	runs := make([]st.Run, 0, len(receipts))
	for _, run := range receipts {
		runs = append(runs, run)
	}
	return runs
}

func markChangedActivity(state *st.CoordinatorState, run *st.Run, previous map[string][]byte, previousUpdated map[string]time.Time) error {
	normalizeRunTimes(run)
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

func markActivitySync(state *st.CoordinatorState, run st.Run) {
	if run.ActivityID == "" {
		return
	}
	if state.PendingActivitySync == nil {
		state.PendingActivitySync = make(map[string]time.Time)
	}
	state.PendingActivitySync[run.ID] = run.UpdatedAt
}

func normalizeRecordTimes(record *st.QueueRecord) {
	record.LastEnqueuedAt = record.LastEnqueuedAt.UTC()
	record.NextRun = record.NextRun.UTC()
	for i := range record.Runs {
		normalizeRunTimes(&record.Runs[i])
	}
	for id, run := range record.Receipts {
		normalizeRunTimes(&run)
		record.Receipts[id] = run
	}
}

func normalizeRunTimes(run *st.Run) {
	run.CreatedAt = run.CreatedAt.UTC()
	run.UpdatedAt = run.UpdatedAt.UTC()
	normalizeTimePointer(run.StartedAt)
	normalizeTimePointer(run.FinishedAt)
	normalizeTimePointer(run.NextAttempt)
	normalizeTimePointer(run.LastConfirmedAt)
	if run.Resolution != nil {
		run.Resolution.ResolvedAt = run.Resolution.ResolvedAt.UTC()
	}
	for i := range run.Attempts {
		run.Attempts[i].StartedAt = run.Attempts[i].StartedAt.UTC()
		normalizeTimePointer(run.Attempts[i].FinishedAt)
	}
}

func normalizeLegacyRun(run *st.Run) bool {
	automaticResolution := run.Status == st.Canceled && run.Resolution != nil && run.Resolution.ResolvedBy == user.SystemUser.Username
	if run.Status != st.NeedsAttention && !automaticResolution {
		return false
	}
	run.Owner = ""
	run.NextAttempt = nil
	if run.EnvironmentID != "0" && (run.RemoteDeliveryAttempted || run.RemoteAccepted) && !run.RemoteSettled {
		run.Status = st.Waiting
		run.FinishedAt = nil
	} else {
		run.Status = st.Failed
		run.Outcome.Status = st.Failed
		if run.FinishedAt == nil {
			run.FinishedAt = new(run.UpdatedAt)
		}
	}
	run.UpdatedAt = time.Now().UTC()
	return true
}

func normalizeTimePointer(value *time.Time) {
	if value != nil {
		*value = value.UTC()
	}
}
