// Package queue persists scheduler runs and coordinates their execution and recovery.
package queue

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/google/uuid"
)

// Queue persists work before dispatching it to the owning domain.
type Queue struct {
	store        *kv.KVService
	owner        string
	mu           sync.Mutex
	active       map[string]bool
	checkpointed map[string]bool
	ordinary     int
	health       int
	stopping     bool
	started      bool
	cancel       context.CancelFunc
	done         chan struct{}
	wake         chan struct{}
	workers      sync.WaitGroup
	execute      func(context.Context, st.Run) (st.Outcome, error)
	reconcile    func(context.Context, st.Run) (st.Outcome, error)
}

// Submit persists acceptance before waking the dispatcher. Run IDs deduplicate
// requests within a job and environment. Manual and remote requests stay separate;
// other triggers coalesce pending work.
// Cancellation of ctx affects admission only, not an accepted run's lifetime.
func (q *Queue) Submit(ctx context.Context, request st.Request) (st.Run, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping {
		return st.Run{}, errors.New("job queue is stopping")
	}
	if request.EnvironmentID == "" {
		request.EnvironmentID = "0"
	}
	if request.JobID == "" {
		return st.Run{}, errors.New("job ID required")
	}
	if request.Trigger == "" {
		request.Trigger = "manual"
	}
	if request.RunID == "" {
		request.RunID = uuid.NewString()
	}
	if _, err := uuid.Parse(request.RunID); err != nil {
		return st.Run{}, errors.New("invalid run ID")
	}
	now := time.Now().UTC()
	result := st.Run{ID: request.RunID, JobID: request.JobID, EnvironmentID: request.EnvironmentID, Trigger: request.Trigger, RequestedBy: request.RequestedBy, RequestedWithKey: request.RequestedWithKey, RemoteAccepted: request.Trigger == "remote", Status: st.Queued, CreatedAt: now, UpdatedAt: now}
	err := q.mutateInternal(ctx, request.EnvironmentID, request.JobID, func(record *st.QueueRecord) error {
		if request.Trigger == "scheduled" || request.Trigger == "recovery" {
			record.LastEnqueuedAt = now
		}
		for _, existing := range record.Runs {
			if existing.ID == request.RunID {
				result = existing
				return nil
			}
			if request.Trigger != "manual" && request.Trigger != "remote" && existing.Trigger != "manual" && existing.Trigger != "remote" && !existing.Status.Terminal() && existing.Status != st.Running {
				result = existing
				return nil
			}
		}
		if existing, ok := record.Receipts[request.RunID]; ok {
			result = existing
			return nil
		}
		pruneRunsInternal(record, now)
		record.Runs = append(record.Runs, result)
		return nil
	})
	if err == nil {
		q.signalInternal()
	}
	return result, err
}

func (q *Queue) signalInternal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// Checkpoint records the next due occurrence atomically with overdue recovery.
func (q *Queue) Checkpoint(ctx context.Context, jobID, schedule string, next time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	first := !q.checkpointed[jobID]
	recovered := false
	err := q.mutateInternal(ctx, "0", jobID, func(record *st.QueueRecord) error {
		now := time.Now().UTC()
		if first && record.Schedule == schedule && !record.NextRun.IsZero() && !record.NextRun.After(now) && record.LastEnqueuedAt.Before(record.NextRun) {
			pending := false
			for _, run := range record.Runs {
				if !run.Status.Terminal() {
					pending = true
					break
				}
			}
			if !pending {
				recovered = true
				record.LastEnqueuedAt = now
				record.Runs = append(record.Runs, st.Run{ID: uuid.NewString(), JobID: jobID, EnvironmentID: "0", Trigger: "recovery", Status: st.Queued, CreatedAt: now, UpdatedAt: now})
			}
		}
		record.Schedule = schedule
		record.NextRun = next
		return nil
	})
	if err == nil {
		q.checkpointed[jobID] = true
		if recovered {
			q.signalInternal()
		}
	}
	return err
}

// Start validates persisted state and starts dispatching on ctx. Repeated calls
// are harmless, but a stopped queue cannot be restarted; create a new instance.
func (q *Queue) Start(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started {
		return nil
	}
	if q.execute == nil {
		return errors.New("job executor unavailable")
	}
	// Fail startup if persisted state cannot be read. Never replace it with empty state.
	if _, err := q.Records(ctx); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	q.cancel = cancel
	q.started = true
	go q.dispatchInternal(runCtx)
	return nil
}

// Stop rejects new submissions, cancels execution, and waits for workers until
// ctx expires. Unfinished claims remain persisted for startup reconciliation.
func (q *Queue) Stop(ctx context.Context) error {
	q.mu.Lock()
	q.stopping = true
	started := q.started
	if q.cancel != nil {
		q.cancel()
	}
	q.mu.Unlock()
	if !started {
		return nil
	}
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *Queue) dispatchInternal(ctx context.Context) {
	defer close(q.done)
	defer q.workers.Wait()
	nextRetention := time.Now().Add(time.Hour)
	for ctx.Err() == nil {
		if !time.Now().Before(nextRetention) {
			if err := q.pruneInternal(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "Job retention failed", "error", err)
			}
			nextRetention = time.Now().Add(time.Hour)
		}
		nextRetry, err := q.dispatchReadyInternal(ctx)
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "Job queue dispatch failed", "error", err)
			nextRetry = time.Now().Add(5 * time.Second)
		}
		nextWake := nextRetention
		if !nextRetry.IsZero() && nextRetry.Before(nextWake) {
			nextWake = nextRetry
		}
		timer := time.NewTimer(max(time.Until(nextWake), 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-q.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (q *Queue) dispatchReadyInternal(ctx context.Context) (time.Time, error) {
	records, err := q.Records(ctx)
	if err != nil {
		return time.Time{}, err
	}
	var nextRetry time.Time
	for _, record := range records {
		key := queueKeyInternal(record.EnvironmentID, record.JobID)
		reserved := strings.HasPrefix(record.JobID, "environment-health") || record.JobID == "docker-client-refresh" || record.JobID == "activity-sweep"
		q.mu.Lock()
		if q.active[key] || q.stopping || (!reserved && q.ordinary >= 4) || (reserved && q.health >= 2) {
			q.mu.Unlock()
			continue
		}
		selected, retryAt := q.nextRunInternal(record.Runs)
		if !retryAt.IsZero() && (nextRetry.IsZero() || retryAt.Before(nextRetry)) {
			nextRetry = retryAt
		}
		if selected == nil {
			q.mu.Unlock()
			continue
		}
		run := *selected
		reconcile := run.Status == st.Running
		err = q.UpdateRun(ctx, run, func(current *st.Run) error {
			if current.Status != run.Status || current.Owner != run.Owner {
				return ErrRunConflict
			}
			now := time.Now().UTC()
			current.Status = st.Running
			current.Owner = q.owner
			current.UpdatedAt = now
			current.StartedAt = &now
			current.NextAttempt = nil
			current.AttemptCount++
			current.Attempts = append(current.Attempts, st.Attempt{Number: current.AttemptCount, StartedAt: now})
			if len(current.Attempts) > 100 {
				current.Attempts = current.Attempts[len(current.Attempts)-100:]
			}
			// Preserve the previous outcome for reconciliation and completed target filtering.
			run = *current
			return nil
		})
		if err != nil {
			q.mu.Unlock()
			return time.Time{}, err
		}
		q.active[key] = true
		if reserved {
			q.health++
		} else {
			q.ordinary++
		}
		q.workers.Add(1)
		q.mu.Unlock()
		go q.executeInternal(ctx, key, run, reserved, reconcile)
	}
	return nextRetry, nil
}

func (q *Queue) executeInternal(ctx context.Context, key string, run st.Run, reserved, reconcile bool) {
	defer q.workers.Done()
	defer func() {
		q.mu.Lock()
		delete(q.active, key)
		if reserved {
			q.health--
		} else {
			q.ordinary--
		}
		q.mu.Unlock()
		q.signalInternal()
	}()
	outcome, err := q.invokeInternal(ctx, run, reconcile)
	if outcome.Status == "" {
		if err != nil {
			outcome.Status = st.Failed
		} else {
			outcome.Status = st.Succeeded
		}
	}
	if err != nil && outcome.Message == "" {
		outcome.Message = err.Error()
	}
	// Shutdown leaves a running claim for startup reconciliation, never a false success.
	if ctx.Err() != nil {
		return
	}
	q.persistOutcomeInternal(ctx, run, outcome)
}

func (q *Queue) persistOutcomeInternal(ctx context.Context, run st.Run, outcome st.Outcome) {
	// Keep the execution slot until the terminal result has been durably recorded.
	for {
		err := q.UpdateRun(ctx, run, func(current *st.Run) error {
			if current.Owner != q.owner || current.Status != st.Running {
				return ErrRunConflict
			}
			now := time.Now().UTC()
			current.Status = outcome.Status
			current.UpdatedAt = now
			outcome = mergeTargetProgressInternal(current.Outcome, outcome)
			current.Outcome = outcome
			if outcome.Status == st.Retrying || outcome.Status == st.Waiting {
				delay := 5 * time.Second * time.Duration(1<<min(run.AttemptCount-1, 6))
				delay = min(delay, 5*time.Minute)
				var jitter [1]byte
				if _, jitterErr := io.ReadFull(rand.Reader, jitter[:]); jitterErr != nil {
					// Keep the unjittered backoff when randomness is unavailable.
					slog.WarnContext(ctx, "Retry jitter unavailable; using exponential backoff", "runId", run.ID, "error", jitterErr)
				} else {
					delay = time.Duration(float64(delay) * (0.8 + float64(jitter[0])/255*0.2))
				}
				next := now.Add(delay)
				current.NextAttempt = &next
			} else {
				current.NextAttempt = nil
			}
			if outcome.Status.Terminal() {
				current.FinishedAt = &now
			}
			if len(current.Attempts) > 0 {
				last := &current.Attempts[len(current.Attempts)-1]
				last.FinishedAt = &now
				last.Outcome = outcome
			}
			return nil
		})
		if err == nil || errors.Is(err, ErrRunConflict) {
			return
		}
		slog.ErrorContext(ctx, "Unable to persist job result; retaining execution guard", "runId", run.ID, "error", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (q *Queue) invokeInternal(ctx context.Context, run st.Run, reconcile bool) (outcome st.Outcome, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = st.Outcome{Status: st.NeedsAttention, Message: "Job panicked; inspect logs before retrying"}
			err = errors.Errorf("job panic: %v", recovered)
			slog.ErrorContext(ctx, "Job panicked", "runId", run.ID, "error", err)
		}
	}()
	if reconcile && q.reconcile != nil {
		return q.reconcile(ctx, run)
	}
	return q.execute(ctx, run)
}

// Cancel marks an unstarted queued run canceled. Runs already attempted locally
// or dispatched remotely must be reconciled rather than canceled as pending work.
func (q *Queue) Cancel(ctx context.Context, environmentID, jobID, runID string) (st.Run, error) {
	run, err := q.Get(ctx, environmentID, jobID, runID)
	if err != nil {
		return run, err
	}
	err = q.UpdateRun(ctx, run, func(current *st.Run) error {
		if current.Status != st.Queued || current.AttemptCount != 0 || current.RemoteDeliveryAttempted {
			return errors.New("only unstarted pending runs can be canceled")
		}
		now := time.Now().UTC()
		current.Status = st.Canceled
		current.UpdatedAt = now
		current.FinishedAt = &now
		run = *current
		return nil
	})
	return run, err
}

// Retry requeues a failed, partial, or attention-required run without changing its
// ID or dropping target progress. Unconfirmed remote work cannot be retried here.
func (q *Queue) Retry(ctx context.Context, environmentID, jobID, runID string) (st.Run, error) {
	run, err := q.Get(ctx, environmentID, jobID, runID)
	if err != nil {
		return run, err
	}
	err = q.UpdateRun(ctx, run, func(current *st.Run) error {
		if current.Status != st.Failed && current.Status != st.NeedsAttention && current.Status != st.Partial {
			return errors.New("run is not eligible for retry")
		}
		if current.RemoteAccepted && !current.RemoteSettled {
			return errors.New("remote outcome must be confirmed before retry")
		}
		current.Status = st.Queued
		current.Owner = ""
		current.NextAttempt = nil
		current.FinishedAt = nil
		current.UpdatedAt = time.Now().UTC()
		run = *current
		return nil
	})
	if err == nil {
		q.signalInternal()
	}
	return run, err
}

func (q *Queue) nextRunInternal(runs []st.Run) (*st.Run, time.Time) {
	var interrupted *st.Run
	blocked := false
	for index := range runs {
		run := &runs[index]
		if run.Status == st.NeedsAttention || (run.Status == st.Running && run.Owner == q.owner) {
			blocked = true
		}
		if run.Status == st.Running && run.Owner != q.owner && interrupted == nil {
			interrupted = run
		}
	}
	if interrupted != nil || blocked {
		return interrupted, time.Time{}
	}
	var nextRetry time.Time
	now := time.Now()
	for index := range runs {
		run := &runs[index]
		if run.Status.Terminal() {
			continue
		}
		if run.NextAttempt == nil || !run.NextAttempt.After(now) {
			return run, time.Time{}
		}
		if nextRetry.IsZero() || run.NextAttempt.Before(nextRetry) {
			nextRetry = *run.NextAttempt
		}
	}
	return nil, nextRetry
}

func mergeTargetProgressInternal(previous, outcome st.Outcome) st.Outcome {
	for _, target := range previous.Targets {
		found := false
		for _, updated := range outcome.Targets {
			if updated.ID == target.ID {
				found = true
				break
			}
		}
		if !found {
			outcome.Targets = append(outcome.Targets, target)
		}
	}
	if outcome.ActivityID == "" {
		outcome.ActivityID = previous.ActivityID
	}
	return outcome
}
