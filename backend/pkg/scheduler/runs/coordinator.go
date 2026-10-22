// Package runs coordinates durable scheduler execution and recovery.
package runs

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/italypaleale/francis/actor"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis"
)

// Coordinator persists work before dispatching it to the owning domain.
type Coordinator struct {
	store        *kv.KVService
	service      *actor.Service
	runtime      *francis.Runtime
	owner        string
	mu           sync.Mutex
	checkpointed map[string]bool
	stopping     bool
	started      bool
	cancel       context.CancelFunc
	lifecycleCtx context.Context
	done         chan struct{}
	wake         chan struct{}
	enabled      atomic.Bool
	execute      func(context.Context, types.SchedulerRun) (types.SchedulerOutcome, error)
	reconcile    func(context.Context, types.SchedulerRun) (types.SchedulerOutcome, error)
	observer     types.RunObserver
	observers    utils.KeyedMutex
	location     *time.Location
}

// Submit persists acceptance before waking the dispatcher. Run IDs deduplicate
// requests within a job and environment. Manual and remote requests stay separate;
// other triggers coalesce pending work.
// Cancellation of ctx affects admission only, not an accepted run's lifetime.
func (q *Coordinator) Submit(ctx context.Context, request types.SchedulerRequest) (types.SchedulerRun, error) {
	q.mu.Lock()
	stopping := q.stopping
	q.mu.Unlock()
	if stopping {
		return types.SchedulerRun{}, errors.New("job queue is stopping")
	}
	request.EnvironmentID = cmp.Or(request.EnvironmentID, "0")
	if request.JobID == "" {
		return types.SchedulerRun{}, errors.New("job ID required")
	}
	request.Trigger = cmp.Or(request.Trigger, "manual")
	request.RunID = cmp.Or(request.RunID, uuid.New().String())
	if _, err := uuid.Parse(request.RunID); err != nil {
		return types.SchedulerRun{}, errors.New("invalid run ID")
	}
	now := time.Now().UTC()
	result := types.SchedulerRun{
		ID:               request.RunID,
		JobID:            request.JobID,
		EnvironmentID:    request.EnvironmentID,
		Trigger:          request.Trigger,
		RequestedBy:      request.RequestedBy,
		RequestedWithKey: request.RequestedWithKey,
		Status:           types.RunStatusQueued,
		CreatedAt:        now,
		UpdatedAt:        now,
		RemoteAccepted:   request.Trigger == "remote",
	}
	q.associateActivity(&result)
	fresh := result
	err := q.mutate(ctx, request.EnvironmentID, request.JobID, func(record *types.SchedulerQueueRecord) error {
		result = fresh
		if request.Trigger == "scheduled" || request.Trigger == "recovery" {
			record.LastEnqueuedAt = now
		}
		for _, existing := range record.Runs {
			// Manual and remote work never joins another run; other triggers join runs still waiting to start.
			coalesces := !separateTrigger(request.Trigger) && !separateTrigger(existing.Trigger) &&
				(existing.Status == types.RunStatusQueued || existing.Status == types.RunStatusWaiting || existing.Status == types.RunStatusRetrying)
			if existing.ID == request.RunID || coalesces {
				result = existing
				return nil
			}
		}
		if existing, ok := record.Receipts[request.RunID]; ok {
			result = existing
			return nil
		}
		pruneRuns(record, now)
		record.Runs = append(record.Runs, result)
		return nil
	})
	if err == nil {
		q.observeRun(ctx, result)
		q.signal()
	}
	return result, err
}

func (q *Coordinator) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func separateTrigger(trigger string) bool {
	switch trigger {
	case "manual", "remote":
		return true
	default:
		return false
	}
}

// Checkpoint records the next due occurrence atomically with overdue recovery.
func (q *Coordinator) Checkpoint(ctx context.Context, jobID, schedule string, next time.Time) error {
	q.mu.Lock()
	first := !q.checkpointed[jobID]
	q.mu.Unlock()
	recovered := false
	var recoveredRun types.SchedulerRun
	err := q.mutate(ctx, "0", jobID, func(record *types.SchedulerQueueRecord) error {
		recovered = false
		recoveredRun = types.SchedulerRun{}
		now := time.Now().UTC()
		if first && record.Schedule == schedule && !record.NextRun.IsZero() && !record.NextRun.After(now) && record.LastEnqueuedAt.Before(record.NextRun) {
			if !slices.ContainsFunc(record.Runs, func(run types.SchedulerRun) bool {
				return !run.Status.Terminal() && run.Status != types.RunStatusNeedsAttention
			}) {
				recovered = true
				record.LastEnqueuedAt = now
				recoveredRun = types.SchedulerRun{
					ID:            uuid.New().String(),
					JobID:         jobID,
					EnvironmentID: "0",
					Trigger:       "recovery",
					Status:        types.RunStatusQueued,
					CreatedAt:     now,
					UpdatedAt:     now,
				}
				record.Runs = append(record.Runs, recoveredRun)
			}
		}
		if record.Schedule != schedule || record.NextRun.IsZero() || (first && !record.NextRun.After(now)) {
			record.Generation++
			record.Sequence++
			record.Schedule = schedule
			record.NextRun = next
		}
		return nil
	})
	if err == nil {
		q.mu.Lock()
		q.checkpointed[jobID] = true
		q.mu.Unlock()
		if recovered {
			q.observeRun(ctx, recoveredRun)
			q.signal()
		}
	}
	return err
}

func (q *Coordinator) Remove(ctx context.Context, jobID string) error {
	if err := q.Checkpoint(ctx, jobID, "", time.Time{}); err != nil {
		return err
	}
	q.mu.Lock()
	delete(q.checkpointed, jobID)
	q.mu.Unlock()
	_, err := q.service.Invoke(ctx, coordinatorType, kit.SHA256Hex("0\x00"+jobID), "remove", nil)
	return err
}

// Start validates persisted state on ctx and starts dispatching on appCtx. Repeated calls
// are harmless, but a stopped queue cannot be restarted; create a new instance.
func (q *Coordinator) Start(ctx, appCtx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("initialize job coordinator: %w", err)
	}
	if q.stopping {
		return errors.New("job queue is stopping")
	}
	if q.execute == nil {
		return errors.New("job executor unavailable")
	}
	if err := q.importLegacy(ctx); err != nil {
		return fmt.Errorf("import legacy job history: %w", err)
	}
	// Fail startup if persisted state cannot be read. Never replace it with empty state.
	states, err := q.states(ctx)
	if err != nil {
		return fmt.Errorf("read startup job history: %w", err)
	}
	for index := range states {
		record := &states[index].Record
		changed := false
		for i := range record.Runs {
			changed = normalizeLegacyRun(&record.Runs[i]) || changed
		}
		if !changed {
			continue
		}
		if mutateErr := q.mutate(ctx, record.EnvironmentID, record.JobID, func(current *types.SchedulerQueueRecord) error {
			for i := range current.Runs {
				normalizeLegacyRun(&current.Runs[i])
			}
			return nil
		}); mutateErr != nil {
			return fmt.Errorf("normalize startup job %q in environment %q: %w", record.JobID, record.EnvironmentID, mutateErr)
		}
	}
	if pruneErr := q.prune(ctx, states); pruneErr != nil {
		return fmt.Errorf("prune startup job history: %w", pruneErr)
	}
	if errErr := ctx.Err(); errErr != nil {
		return fmt.Errorf("initialize job coordinator: %w", errErr)
	}
	if errErr2 := appCtx.Err(); errErr2 != nil {
		return fmt.Errorf("start job dispatcher: %w", errErr2)
	}

	runCtx, cancel := context.WithCancel(appCtx)
	q.cancel = cancel
	q.lifecycleCtx = runCtx
	q.started = true
	go q.dispatch(runCtx)
	return nil
}

// Stop rejects new submissions, cancels execution, and waits for workers until
// ctx expires. Unfinished claims remain persisted for startup reconciliation.
func (q *Coordinator) Stop(ctx context.Context) error {
	q.mu.Lock()
	q.stopping = true
	q.enabled.Store(false)
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

func (q *Coordinator) dispatch(ctx context.Context) {
	defer close(q.done)

	// Flush every coordinator once while enabled to restore schedule alarms; later passes only cover unfinished work.
	fullRepair := true
	nextRetention := time.Now()
	for ctx.Err() == nil {
		enabled := q.enabled.Load()
		retain := !time.Now().Before(nextRetention)
		busy, pendingActivity, pruned, err := q.pass(ctx, fullRepair, retain)
		if retain {
			nextRetention = time.Now().Add(kit.Ternary(pruned, time.Hour, 5*time.Second))
		}
		nextWake := time.Now().Add(kit.Ternary(busy || pendingActivity, 15*time.Second, coordinatorIdleWake))
		if err != nil {
			if ctx.Err() == nil {
				slog.ErrorContext(ctx, "Job queue dispatch failed", "error", err)
			}
			nextWake = time.Now().Add(5 * time.Second)
		} else if enabled {
			fullRepair = false
		}
		if nextRetention.Before(nextWake) {
			nextWake = nextRetention
		}
		if pendingActivity {
			activityRetry := time.Now().Add(5 * time.Second)
			if activityRetry.Before(nextWake) {
				nextWake = activityRetry
			}
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

func (q *Coordinator) pass(ctx context.Context, all, retain bool) (busy, pendingActivity, pruned bool, err error) {
	states, err := q.states(ctx)
	if err != nil {
		return false, true, false, err
	}
	pendingActivity = q.retryActivitySync(ctx, states)
	if retain {
		pruneErr := q.prune(ctx, states)
		if pruneErr != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "Job retention failed", "error", pruneErr)
		}
		pruned = pruneErr == nil
	}
	busy, err = q.repair(ctx, states, all)
	return busy, pendingActivity, pruned, err
}

func (q *Coordinator) persistOutcome(ctx context.Context, run types.SchedulerRun, outcome types.SchedulerOutcome) error {
	// Keep the execution slot while retrying, but release it after a bounded backoff.
	wait := time.Second
	for attempt := 1; ; attempt++ {
		var conflictOwner string
		var conflictStatus types.RunStatus
		err := q.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
			if current.Owner != run.Owner || current.Status != types.RunStatusRunning {
				conflictOwner, conflictStatus = current.Owner, current.Status
				return ErrRunConflict
			}
			now := time.Now().UTC()
			merged := types.SchedulerOutcome{
				Status:     outcome.Status,
				Message:    outcome.Message,
				ActivityID: cmp.Or(outcome.ActivityID, current.Outcome.ActivityID),
				Targets:    slices.Clone(outcome.Targets),
				Steps:      kit.Ternary(len(outcome.Steps) == 0, current.Outcome.Steps, outcome.Steps),
			}
			for _, target := range current.Outcome.Targets {
				if !slices.ContainsFunc(outcome.Targets, func(updated types.TargetOutcome) bool { return updated.ID == target.ID }) {
					merged.Targets = append(merged.Targets, target)
				}
			}
			current.Status = outcome.Status
			current.UpdatedAt = now
			current.Outcome = merged
			if outcome.Status == types.RunStatusRetrying || outcome.Status == types.RunStatusWaiting {
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
			if outcome.Status.Terminal() && (current.EnvironmentID == "0" || current.FinishedAt == nil) {
				current.FinishedAt = &now
			}
			if len(current.Attempts) > 0 {
				last := &current.Attempts[len(current.Attempts)-1]
				last.FinishedAt = &now
				last.Outcome = merged
				last.Outcome.Steps = nil
			}
			return nil
		})
		if errors.Is(err, ErrRunConflict) {
			slog.WarnContext(ctx, "Discarding job result after ownership changed",
				"runId", run.ID,
				"expectedOwner", run.Owner,
				"currentOwner", conflictOwner,
				"currentStatus", conflictStatus,
				"outcomeStatus", outcome.Status)
			return nil
		}
		if err == nil {
			return nil
		}
		if attempt >= 10 {
			slog.ErrorContext(ctx, "Giving up persisting job result; releasing execution guard", "runId", run.ID, "attempts", attempt, "error", err)
			return fmt.Errorf("persist job result: %w", err)
		}
		slog.WarnContext(ctx, "Unable to persist job result; retaining execution guard", "runId", run.ID, "attempt", attempt, "retryIn", wait, "error", err)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("persist job result: %w", err)
		case <-timer.C:
		}
		wait = min(wait*2, 30*time.Second)
	}
}

// Cancel marks an unstarted queued run canceled. Runs already attempted locally
// or dispatched remotely must be reconciled rather than canceled as pending work.
func (q *Coordinator) Cancel(ctx context.Context, environmentID, jobID, runID string) (types.SchedulerRun, error) {
	run, err := q.Get(ctx, environmentID, jobID, runID)
	if err != nil {
		return run, err
	}
	err = q.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
		if current.Status != types.RunStatusQueued || current.AttemptCount != 0 || current.RemoteDeliveryAttempted {
			return errors.New("only unstarted pending runs can be canceled")
		}
		now := time.Now().UTC()
		current.Status = types.RunStatusCanceled
		current.UpdatedAt = now
		current.FinishedAt = &now
		run = *current
		return nil
	})
	if err == nil {
		var state types.SchedulerCoordinatorState
		id := kit.SHA256Hex(environmentID + "\x00" + jobID)
		actorType := kit.Ternary(
			strings.HasPrefix(jobID, "environment-health") || jobID == "docker-client-refresh" || jobID == "activity-sweep",
			healthExecutorType,
			executorType,
		)
		if readErr := q.service.GetState(ctx, coordinatorType, id, &state); readErr == nil {
			for _, intent := range state.Dispatches {
				if intent.RunID == runID && intent.JobID != "" {
					_ = q.service.DeleteJob(ctx, actorType, id, intent.JobID, actor.WithLiveJobsOnly())
				}
			}
		}
	}
	return run, err
}

// Retry requeues a failed, partial, or attention-required run without changing its
// ID or dropping target progress. Unconfirmed remote work cannot be retried here.
func (q *Coordinator) Retry(ctx context.Context, environmentID, jobID, runID string) (types.SchedulerRun, error) {
	run, err := q.Get(ctx, environmentID, jobID, runID)
	if err != nil {
		return run, err
	}
	err = q.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
		if current.Status != types.RunStatusFailed && current.Status != types.RunStatusNeedsAttention && current.Status != types.RunStatusPartial {
			return errors.New("run is not eligible for retry")
		}
		if current.RemoteAccepted && !current.RemoteSettled {
			return errors.New("remote outcome must be confirmed before retry")
		}
		current.Status = types.RunStatusQueued
		current.Owner = ""
		current.NextAttempt = nil
		current.FinishedAt = nil
		current.UpdatedAt = time.Now().UTC()
		run = *current
		return nil
	})
	if err == nil {
		q.signal()
	}
	return run, err
}

func (q *Coordinator) nextRun(runs []types.SchedulerRun) (*types.SchedulerRun, time.Time) {
	var interrupted *types.SchedulerRun
	blocked := false
	for index := range runs {
		run := &runs[index]
		if run.Status == types.RunStatusRunning && strings.HasPrefix(run.Owner, q.owner+"/") {
			blocked = true
		}
		if run.Status == types.RunStatusRunning && !strings.HasPrefix(run.Owner, q.owner+"/") && interrupted == nil {
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
		if run.Status.Terminal() || run.Status == types.RunStatusNeedsAttention {
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

// ExecutionContext binds durable progress for work executing a run. An empty
// instanceID fences writes by run owner; otherwise by the active workflow instance.
func (q *Coordinator) ExecutionContext(ctx context.Context, run types.SchedulerRun, instanceID string) context.Context {
	ctx = utils.WithActivityBatchID(ctx, run.ID)
	return jobcontext.WithExecution(ctx, run, func(target types.TargetOutcome) error {
		return q.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
			if !kit.Ternary(instanceID == "", current.Status == types.RunStatusRunning && current.Owner == run.Owner, ActiveWorkflow(*current, instanceID)) {
				return ErrRunConflict
			}
			current.Outcome.Targets = jobcontext.Merge(current.Outcome.Targets, target)
			return nil
		})
	})
}

// RecordSteps projects workflow step progress onto a running run, fenced like ExecutionContext.
func (q *Coordinator) RecordSteps(ctx context.Context, run types.SchedulerRun, instanceID string, steps []types.StepOutcome) error {
	return q.UpdateRun(ctx, run, func(current *types.SchedulerRun) error {
		if !kit.Ternary(instanceID == "", current.Status == types.RunStatusRunning && current.Owner == run.Owner, ActiveWorkflow(*current, instanceID)) {
			return ErrRunConflict
		}
		current.Outcome.Steps = steps
		return nil
	})
}

// ActiveWorkflow reports whether a running run still follows a workflow instance.
func ActiveWorkflow(run types.SchedulerRun, instanceID string) bool {
	return run.Status == types.RunStatusRunning && slices.ContainsFunc(run.Outcome.Targets, func(target types.TargetOutcome) bool {
		return target.ResourceType == types.SchedulerWorkflowTarget && target.ID == instanceID && !target.Status.Terminal()
	})
}

func (q *Coordinator) Active() bool { return q.enabled.Load() }
