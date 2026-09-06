package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"slices"
	"sort"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/google/uuid"
)

const queuePrefixInternal = "jobs."

func queueKeyInternal(environmentID, jobID string) string {
	sum := sha256.Sum256([]byte(environmentID + "\x00" + jobID))
	return queuePrefixInternal + hex.EncodeToString(sum[:])
}

func (q *Queue) mutateInternal(ctx context.Context, environmentID, jobID string, change func(*st.QueueRecord) error) error {
	key := queueKeyInternal(environmentID, jobID)
	for range 20 {
		previous, found, err := q.store.Get(ctx, key)
		if err != nil {
			return err
		}
		record := st.QueueRecord{JobID: jobID, EnvironmentID: environmentID}
		if found {
			if err := json.Unmarshal([]byte(previous), &record); err != nil {
				return errors.WrapIf(err, "decode job queue")
			}
		}
		if err := change(&record); err != nil {
			return err
		}
		next, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if found && string(next) == previous {
			return nil
		}
		var swapped bool
		if found {
			swapped, err = q.store.CompareAndSwap(ctx, key, previous, string(next))
		} else {
			swapped, err = q.store.CreateIfAbsent(ctx, key, string(next))
		}
		if err != nil {
			return err
		}
		if swapped {
			return nil
		}
	}
	return errors.New("job queue contention; retry request with the same run ID")
}

// Records reads all persisted queue records, including run history and receipts.
// Storage or decoding errors fail the read instead of returning an incomplete snapshot.
func (q *Queue) Records(ctx context.Context) ([]st.QueueRecord, error) {
	entries, err := q.store.ListByPrefix(ctx, queuePrefixInternal)
	if err != nil {
		return nil, err
	}
	records := make([]st.QueueRecord, 0, len(entries))
	for _, entry := range entries {
		var record st.QueueRecord
		if err := json.Unmarshal([]byte(entry.Value), &record); err != nil {
			return nil, errors.WrapIff(err, "decode job queue %s", entry.Key)
		}
		records = append(records, record)
	}
	return records, nil
}

// Get retrieves a run by environment, job, and run ID, including compacted
// idempotency receipts. It returns ErrRunNotFound when no matching record exists.
func (q *Queue) Get(ctx context.Context, environmentID, jobID, runID string) (st.Run, error) {
	raw, found, err := q.store.Get(ctx, queueKeyInternal(environmentID, jobID))
	if err != nil {
		return st.Run{}, err
	}
	if !found {
		return st.Run{}, ErrRunNotFound
	}
	var record st.QueueRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return st.Run{}, err
	}
	for _, run := range record.Runs {
		if run.ID == runID {
			return run, nil
		}
	}
	if run, ok := record.Receipts[runID]; ok {
		return run, nil
	}
	return st.Run{}, ErrRunNotFound
}

// List returns retained run history newest first, excluding compacted receipts.
// An empty job ID includes every job in the environment; pages are one-based.
func (q *Queue) List(ctx context.Context, environmentID, jobID string, page, limit int) (st.RunList, error) {
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
func (q *Queue) UpdateRun(ctx context.Context, run st.Run, change func(*st.Run) error) error {
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
	return err
}

func (q *Queue) pruneInternal(ctx context.Context) error {
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
		if !run.Status.Terminal() || (run.RemoteAccepted && !run.RemoteSettled) {
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

// New uses the application's existing KV store; no in-memory admission exists.
func New(store *kv.KVService, execute, reconcile func(context.Context, st.Run) (st.Outcome, error)) *Queue {
	return &Queue{execute: execute, reconcile: reconcile, store: store, owner: uuid.NewString(), active: make(map[string]bool), checkpointed: make(map[string]bool), wake: make(chan struct{}, 1), done: make(chan struct{})}
}
