package runs

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/host/local"
	"github.com/robfig/cron/v3"
	kit "go.getarcane.app/kit/pkg"

	scheduleutil "github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
)

const (
	coordinatorType     = "job-coordinator"
	executorType        = "job-executor"
	healthExecutorType  = "job-health-executor"
	orphanedDispatchAge = 15 * time.Minute
	outcomePersistGrace = 30 * time.Second
	coordinatorIdleWake = 2 * time.Minute
)

type coordinatorActor struct {
	id string
	q  *Coordinator
}
type executorActor struct {
	q *Coordinator
}

func (q *Coordinator) Register(runtime *francis.Runtime) error {
	q.runtime = runtime
	if err := runtime.RegisterActor(coordinatorType, func(id string, _ *actor.Service) actor.Actor {
		return &coordinatorActor{id: id, q: q}
	}); err != nil {
		return err
	}
	executorFactory := func(_ string, _ *actor.Service) actor.Actor {
		return &executorActor{q: q}
	}
	if err := runtime.RegisterActor(
		executorType, executorFactory,
		local.WithCapacityGroup("jobs", 4),
		local.WithCompletedJobRetention(7*24*time.Hour),
	); err != nil {
		return err
	}
	return runtime.RegisterActor(
		healthExecutorType, executorFactory,
		local.WithCapacityGroup("jobs-health", 2),
		local.WithCompletedJobRetention(7*24*time.Hour),
	)
}

func (a *coordinatorActor) Invoke(ctx context.Context, method string, data actor.Envelope) (any, error) {
	var state types.SchedulerCoordinatorState
	if err := a.q.service.GetState(ctx, coordinatorType, a.id, &state); err != nil && !errors.Is(err, actor.ErrStateNotFound) {
		return nil, err
	}
	normalizeRecordTimes(&state.Record)
	switch method {
	case "replace":
		var next types.SchedulerCoordinatorState
		if err := data.Decode(&next); err != nil {
			return nil, err
		}
		if next.Revision != state.Revision {
			return false, nil
		}
		next.Revision++
		if err := a.q.service.SetState(ctx, coordinatorType, a.id, next, nil); err != nil {
			return nil, err
		}
		// Acceptance is persisted; dispatch failures are retried.
		if err := a.flush(ctx, &next); err != nil {
			slog.WarnContext(ctx, "Job dispatch requires reconciliation", "jobId", next.Record.JobID, "environmentId", next.Record.EnvironmentID, "error", err)
			a.q.signal()
		}
		return true, nil
	case "activity-synced":
		var command types.SchedulerActivitySyncCommand
		if err := data.Decode(&command); err != nil {
			return nil, err
		}
		version, pending := state.PendingActivitySync[command.RunID]
		if !pending || !version.Equal(command.UpdatedAt) {
			return nil, nil
		}
		delete(state.PendingActivitySync, command.RunID)
		state.Revision++
		return nil, a.q.service.SetState(ctx, coordinatorType, a.id, state, nil)
	case "repair":
		return nil, a.flush(ctx, &state)
	case "remove":
		if state.Record.Schedule != "" || len(state.PendingActivitySync) > 0 || len(state.Record.Runs) > 0 || len(state.Record.Receipts) > 0 {
			return false, nil
		}
		if err := a.q.service.DeleteState(ctx, coordinatorType, a.id); err != nil && !errors.Is(err, actor.ErrStateNotFound) {
			return nil, err
		}
		return true, nil
	case "import":
		var command types.SchedulerCoordinatorImport
		if err := data.Decode(&command); err != nil {
			return nil, err
		}
		if state.Imported {
			if state.ImportHash != command.SourceHash {
				return nil, errors.New("legacy job record changed during import")
			}
			return true, nil
		}
		if state.Revision != 0 {
			return nil, errors.New("native job state exists before legacy import")
		}
		state.ImportHash = command.SourceHash
		state.Record = command.Record
		for _, run := range state.Record.Runs {
			markActivitySync(&state, run)
		}
		state.Imported = true
		state.Revision = 1
		return true, a.q.service.SetState(ctx, coordinatorType, a.id, state, nil)
	default:
		return nil, fmt.Errorf("unknown coordinator command: %s", method)
	}
}

func (a *coordinatorActor) flush(ctx context.Context, state *types.SchedulerCoordinatorState) error {
	if !a.q.enabled.Load() {
		return nil
	}
	record := &state.Record
	if record.Schedule != "" && !record.NextRun.IsZero() {
		name := fmt.Sprintf("schedule-%d-%d", record.Generation, record.Sequence)
		properties := actor.AlarmProperties{
			DueTime: record.NextRun,
			Data:    types.ScheduleOccurrence{Generation: record.Generation, Sequence: record.Sequence},
		}
		if err := a.q.service.SetAlarm(ctx, coordinatorType, a.id, name, properties); err != nil {
			return err
		}
	}
	var selected *types.SchedulerRun
	for i := range record.Runs {
		if record.Runs[i].Status == types.RunStatusRunning {
			selected = &record.Runs[i]
			break
		}
	}
	var retryAt time.Time
	if selected == nil {
		selected, retryAt = a.q.nextRun(record.Runs)
	}
	if selected == nil {
		if retryAt.IsZero() {
			return nil
		}
		name := fmt.Sprintf("retry-%d", retryAt.UnixNano())
		return a.q.service.SetAlarm(ctx, coordinatorType, a.id, name, actor.AlarmProperties{DueTime: retryAt})
	}
	if selected.Status == types.RunStatusRunning {
		active, err := a.reconcileDispatch(ctx, state, *selected)
		if err != nil {
			return err
		}
		if active {
			return nil
		}
	}
	return a.dispatchRun(ctx, state, *selected, selected.AttemptCount+1)
}

// reconcileDispatch recovers a lost job ID before considering redelivery.
func (a *coordinatorActor) reconcileDispatch(ctx context.Context, state *types.SchedulerCoordinatorState, run types.SchedulerRun) (bool, error) {
	key := fmt.Sprintf("%s-%d", run.ID, run.AttemptCount)
	previous, exists := state.Dispatches[key]
	if !exists {
		return false, nil
	}
	actorType := kit.Ternary(
		strings.HasPrefix(run.JobID, "environment-health") || run.JobID == "docker-client-refresh" || run.JobID == "activity-sweep",
		healthExecutorType,
		executorType,
	)
	if previous.JobID == "" {
		command := types.SchedulerExecutionCommand{
			EnvironmentID: run.EnvironmentID,
			JobID:         run.JobID,
			RunID:         run.ID,
			Attempt:       run.AttemptCount,
		}
		id, _, err := a.q.service.Dispatch(ctx, actorType, a.id, "execute", command, actor.WithIdempotencyKey(key))
		if err != nil {
			return false, err
		}
		previous.JobID = id
		state.Dispatches[key] = previous
		state.Revision++
		if setStateErr := a.q.service.SetState(ctx, coordinatorType, a.id, *state, nil); setStateErr != nil {
			return false, setStateErr
		}
	}
	job, err := a.q.service.GetJob(ctx, previous.JobID)
	if errors.Is(err, actor.ErrJobNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if job.Status != actor.JobStatusPending || time.Since(job.DueTime) < orphanedDispatchAge {
		return !job.Status.IsTerminal(), nil
	}
	slog.WarnContext(ctx, "Deleting orphaned job dispatch", "runId", run.ID, "jobId", job.JobID, "dueTime", job.DueTime)
	if deleteErr := a.q.service.DeleteJob(ctx, actorType, a.id, job.JobID, actor.WithLiveJobsOnly()); deleteErr != nil && !errors.Is(deleteErr, actor.ErrJobNotFound) {
		return false, deleteErr
	}
	return false, nil
}

func (a *coordinatorActor) dispatchRun(ctx context.Context, state *types.SchedulerCoordinatorState, run types.SchedulerRun, attempt int) error {
	key := fmt.Sprintf("%s-%d", run.ID, attempt)
	if state.Dispatches == nil {
		state.Dispatches = make(map[string]types.SchedulerDispatchIntent)
	}
	actorType := kit.Ternary(
		strings.HasPrefix(run.JobID, "environment-health") || run.JobID == "docker-client-refresh" || run.JobID == "activity-sweep",
		healthExecutorType,
		executorType,
	)
	intent := state.Dispatches[key]
	if intent.JobID != "" {
		job, err := a.q.service.GetJob(ctx, intent.JobID)
		if err == nil && !job.Status.IsTerminal() && (job.Status != actor.JobStatusPending || time.Since(job.DueTime) < orphanedDispatchAge) {
			return nil
		}
		if err != nil && !errors.Is(err, actor.ErrJobNotFound) {
			return err
		}
		if err == nil {
			if deleteJobErr := a.q.service.DeleteJob(ctx, actorType, a.id, intent.JobID); deleteJobErr != nil && !errors.Is(deleteJobErr, actor.ErrJobNotFound) {
				return deleteJobErr
			}
		}
	}
	state.Dispatches[key] = types.SchedulerDispatchIntent{RunID: run.ID, Attempt: attempt}
	state.Revision++
	if err := a.q.service.SetState(ctx, coordinatorType, a.id, *state, nil); err != nil {
		return err
	}
	options := []actor.JobOption{actor.WithIdempotencyKey(key)}
	if run.NextAttempt != nil && run.NextAttempt.After(time.Now()) {
		options = append(options, actor.WithJobDueTime(*run.NextAttempt))
	}
	command := types.SchedulerExecutionCommand{
		EnvironmentID: run.EnvironmentID,
		JobID:         run.JobID,
		RunID:         run.ID,
		Attempt:       attempt,
	}
	id, _, err := a.q.service.Dispatch(ctx, actorType, a.id, "execute", command, options...)
	if err != nil {
		return err
	}
	job, err := a.q.service.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if job.Status.IsTerminal() {
		if deleteJobErr2 := a.q.service.DeleteJob(ctx, actorType, a.id, id); deleteJobErr2 != nil && !errors.Is(deleteJobErr2, actor.ErrJobNotFound) {
			return deleteJobErr2
		}
		id, _, err = a.q.service.Dispatch(ctx, actorType, a.id, "execute", command, options...)
		if err != nil {
			return err
		}
	}
	state.Dispatches[key] = types.SchedulerDispatchIntent{RunID: run.ID, Attempt: attempt, JobID: id}
	state.Revision++
	return a.q.service.SetState(ctx, coordinatorType, a.id, *state, nil)
}

func (a *coordinatorActor) Alarm(ctx context.Context, name string, data actor.Envelope) error {
	if !a.q.enabled.Load() {
		return nil
	}
	var state types.SchedulerCoordinatorState
	if err := a.q.service.GetState(ctx, coordinatorType, a.id, &state); err != nil && !errors.Is(err, actor.ErrStateNotFound) {
		return err
	}
	normalizeRecordTimes(&state.Record)
	if strings.HasPrefix(name, "retry-") {
		return a.flush(ctx, &state)
	}
	var occurrence types.ScheduleOccurrence
	if err := data.Decode(&occurrence); err != nil {
		return err
	}
	record := &state.Record
	if record.Schedule == "" {
		return nil
	}
	if occurrence.Generation != record.Generation || occurrence.Sequence != record.Sequence {
		return nil
	}
	now := time.Now().UTC()
	schedule, err := scheduleutil.Parser().Parse(record.Schedule)
	if err != nil {
		return err
	}
	if spec, ok := schedule.(*cron.SpecSchedule); ok {
		spec.Location = a.q.location
	}
	trigger := "scheduled"
	nextOccurrence := schedule.Next(record.NextRun.In(a.q.location))
	if !nextOccurrence.After(now) {
		trigger = "recovery"
	}
	pending := slices.ContainsFunc(record.Runs, func(run types.SchedulerRun) bool {
		return !separateTrigger(run.Trigger) && !run.Status.Terminal() && run.Status != types.RunStatusNeedsAttention
	})
	if !pending {
		run := types.SchedulerRun{
			ID:            uuid.New().String(),
			EnvironmentID: record.EnvironmentID,
			JobID:         record.JobID,
			Trigger:       trigger,
			Status:        types.RunStatusQueued,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		a.q.associateActivity(&run)
		record.Runs = append(record.Runs, run)
		markActivitySync(&state, run)
	}
	record.LastEnqueuedAt = now
	record.NextRun = schedule.Next(now.In(a.q.location))
	record.Sequence++
	state.Revision++
	if setStateErr := a.q.service.SetState(ctx, coordinatorType, a.id, state, nil); setStateErr != nil {
		return setStateErr
	}
	if flushErr := a.flush(ctx, &state); flushErr != nil {
		return flushErr
	}
	a.q.signal()
	return nil
}

// repair flushes coordinators so schedule alarms and dispatches are restored.
// Unless all is set, it skips records without unfinished runs so idle coordinators can deactivate.
func (q *Coordinator) repair(ctx context.Context, states []types.SchedulerCoordinatorState, all bool) (busy bool, err error) {
	unfinished := func(state types.SchedulerCoordinatorState) bool {
		return slices.ContainsFunc(state.Record.Runs, func(run types.SchedulerRun) bool {
			return !run.Status.Terminal() && run.Status != types.RunStatusNeedsAttention
		})
	}
	for _, state := range states {
		if !all && !unfinished(state) {
			continue
		}
		if _, invokeErr := q.service.Invoke(ctx, coordinatorType, kit.SHA256Hex(state.Record.EnvironmentID+"\x00"+state.Record.JobID), "repair", nil); invokeErr != nil {
			return false, invokeErr
		}
	}
	return slices.ContainsFunc(states, unfinished), nil
}

func (q *Coordinator) importLegacy(ctx context.Context) error {
	entries, err := q.store.ListByPrefix(ctx, queuePrefix)
	if err != nil {
		return fmt.Errorf("list legacy job records: %w", err)
	}
	for _, entry := range entries {
		marker := "francis-import/" + entry.Key
		_, done, getErr := q.store.Get(ctx, marker)
		if getErr != nil {
			return fmt.Errorf("import legacy job record %q: %w", entry.Key, getErr)
		}
		if done {
			continue
		}
		var record types.SchedulerQueueRecord
		if unmarshalErr := json.Unmarshal([]byte(entry.Value), &record); unmarshalErr != nil {
			return fmt.Errorf("decode legacy job record %q: %w", entry.Key, unmarshalErr)
		}
		id := kit.SHA256Hex(record.EnvironmentID + "\x00" + record.JobID)
		if entry.Key != queuePrefix+id {
			return fmt.Errorf("invalid legacy job key %q for job %q in environment %q", entry.Key, record.JobID, record.EnvironmentID)
		}
		sourceHash := kit.SHA256Hex(entry.Value)
		command := types.SchedulerCoordinatorImport{Record: record, SourceHash: sourceHash}
		if _, invokeErr := q.service.Invoke(ctx, coordinatorType, id, "import", command); invokeErr != nil {
			return fmt.Errorf("import legacy job record %q: %w", entry.Key, invokeErr)
		}
		var imported types.SchedulerCoordinatorState
		if getStateErr := q.service.GetState(ctx, coordinatorType, id, &imported); getStateErr != nil {
			return fmt.Errorf("import legacy job record %q: %w", entry.Key, getStateErr)
		}
		if !imported.Imported || imported.ImportHash != sourceHash {
			return fmt.Errorf("legacy import verification failed for job %q in environment %q", record.JobID, record.EnvironmentID)
		}
		if imported.Record.EnvironmentID != record.EnvironmentID || imported.Record.JobID != record.JobID {
			return fmt.Errorf("legacy import identity mismatch for record %q", entry.Key)
		}
		if setErr := q.store.Set(ctx, marker, "complete"); setErr != nil {
			return fmt.Errorf("import legacy job record %q: %w", entry.Key, setErr)
		}
	}
	return nil
}

func (a *executorActor) Job(ctx context.Context, _ string, data actor.Envelope) error {
	q := a.q
	q.mu.Lock()
	lifetime := q.lifecycleCtx
	q.mu.Unlock()
	if lifetime != nil {
		workCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(lifetime, cancel) //nolint:contextcheck // Application shutdown cancels work before the host drains its jobs.
		defer stop()
		defer cancel()
		ctx = workCtx
		if lifetime.Err() != nil {
			cancel()
		}
	}
	if !q.enabled.Load() {
		return actor.ErrJobRejected
	}
	var command types.SchedulerExecutionCommand
	if err := data.Decode(&command); err != nil {
		return errors.Join(err, actor.ErrJobPermanentFailure)
	}
	claim, err := q.claimExecution(ctx, command)
	if err != nil {
		return err
	}
	if claim.Deferred {
		return actor.ErrJobRejected
	}
	if !claim.Claimed {
		return nil
	}
	outcome, runErr := func() (outcome types.SchedulerOutcome, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				outcome = types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "Job panicked; inspect logs before retrying"}
				err = fmt.Errorf("job panic: %v", recovered)
				slog.ErrorContext(ctx, "Job panicked", "runId", claim.Run.ID, "error", err)
			}
		}()
		if claim.Reconcile && q.reconcile != nil {
			return q.reconcile(ctx, claim.Run)
		}
		return q.execute(ctx, claim.Run)
	}()
	// Domain recovery may still report uncertainty; job runs finish as failures.
	status := cmp.Or(outcome.Status, kit.Ternary(runErr != nil, types.RunStatusFailed, types.RunStatusSucceeded))
	outcome = types.SchedulerOutcome{
		Status:     kit.Ternary(status == types.RunStatusNeedsAttention, types.RunStatusFailed, status),
		Message:    cmp.Or(outcome.Message, kit.Ternary(runErr != nil, fmt.Sprint(runErr), "")),
		ActivityID: outcome.ActivityID,
		Targets:    outcome.Targets,
		Steps:      outcome.Steps,
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return cmp.Or(q.persistOutcome(ctx, claim.Run, outcome), ctx.Err())
	}
	persistCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	defer context.AfterFunc(ctx, func() { time.AfterFunc(outcomePersistGrace, cancel) })()
	return cmp.Or(q.persistOutcome(persistCtx, claim.Run, outcome), ctx.Err())
}

func (q *Coordinator) claimExecution(ctx context.Context, command types.SchedulerExecutionCommand) (types.SchedulerExecutionClaim, error) {
	var claim types.SchedulerExecutionClaim
	err := q.mutate(ctx, command.EnvironmentID, command.JobID, func(record *types.SchedulerQueueRecord) error {
		claim = types.SchedulerExecutionClaim{}
		for i := range record.Runs {
			current := &record.Runs[i]
			if current.ID != command.RunID {
				continue
			}
			if current.Status.Terminal() || current.Status == types.RunStatusNeedsAttention {
				return nil
			}
			if current.AttemptCount > command.Attempt {
				return nil
			}
			if current.AttemptCount == command.Attempt && current.Status != types.RunStatusRunning {
				return nil
			}
			// Executor turns serialize attempts for this coordinator.
			now := time.Now().UTC()
			if current.NextAttempt != nil && current.NextAttempt.After(now) {
				claim = types.SchedulerExecutionClaim{Deferred: true}
				return nil
			}
			reconcile := current.Status == types.RunStatusRunning
			current.Status = types.RunStatusRunning
			current.Owner = q.owner + "/" + uuid.New().String()
			current.UpdatedAt = now
			// Manager claims track delivery; accepted agent runs keep execution timestamps.
			if current.EnvironmentID == "0" || !current.RemoteAccepted {
				current.StartedAt = &now
			}
			current.NextAttempt = nil
			current.AttemptCount = command.Attempt
			current.Attempts = append(current.Attempts, types.SchedulerAttempt{Number: current.AttemptCount, StartedAt: now})
			if len(current.Attempts) > 100 {
				current.Attempts = current.Attempts[len(current.Attempts)-100:]
			}
			claim = types.SchedulerExecutionClaim{
				Run:       *current,
				Claimed:   true,
				Reconcile: reconcile,
			}
			return nil
		}
		return nil
	})
	return claim, err
}
