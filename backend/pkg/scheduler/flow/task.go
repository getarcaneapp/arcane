package flow

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	// maxErrorLength bounds task errors recorded in the workflow journal.
	maxErrorLength = 1024
	// slotWait bounds how long a resumed task waits for its activity slot before it is handed back.
	slotWait = 20 * time.Second
)

type (
	// Binding ties an instance, and its children, to the run that started it.
	Binding struct {
		EnvironmentID string `json:"environmentId,omitempty"`
		JobID         string `json:"jobId,omitempty"`
		RunID         string `json:"runId,omitempty"`
		RootID        string `json:"rootId"`
	}

	// envelope is every instance's input: its run binding plus the caller's payload.
	envelope struct {
		Binding Binding         `json:"binding"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}

	// Task is what an engine handler receives: the Francis task plus its binding.
	Task struct {
		workflow.Task
		binding Binding
		payload json.RawMessage
		// recovered marks a standalone task whose instance an earlier process started.
		recovered bool
	}

	// Result is one fan-out slot: the task's output, or the error that failed it.
	Result[T any] struct {
		Value T
		Err   string
	}
)

// Handler wraps a step handler. Its ctx carries the bound run exactly as a job's
// Run does, is canceled when the user cancels the root activity, and its errors
// are classified for Francis retries.
func (e *Engine) Handler(fn func(ctx context.Context, t Task) (any, error)) workflow.StepOption {
	return workflow.WithRun(func(ctx context.Context, wt workflow.Task) (output any, err error) {
		// Francis does not recover handler panics, which would take the process down.
		defer func() {
			if panicErr := utils.PanicToError(recover()); panicErr != nil {
				slog.ErrorContext(ctx, "Workflow task panicked", "workflow", wt.Workflow(), "step", wt.Step(), "error", panicErr)
				output, err = nil, fmt.Errorf("%w: task panicked: %w", actor.ErrJobPermanentFailure, panicErr)
			}
		}()
		var input envelope
		if decodeErr := wt.DecodeInput(&input); decodeErr != nil {
			return nil, fmt.Errorf("%w: decode workflow input: %w", actor.ErrJobPermanentFailure, decodeErr)
		}
		// Francis neither interrupts a running job on Cancel nor at host shutdown, so the engine does both.
		ctx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)
		root := e.rootContext(input.Binding.RootID)
		defer context.AfterFunc(root, func() { cancel(context.Cause(root)) })()
		taskCtx, err := e.taskContext(ctx, input.Binding)
		if err != nil {
			return nil, classify(ctx, err)
		}
		// A resumed instance's activity lost its slot with the old process, so its first task takes it back;
		// a held slot returns at once. While none frees up, the task is handed back without spending an attempt.
		if root := e.rootWorkflow(input.Binding.RootID); root != nil && root.def.Activity.Queue && !root.def.Activity.DeferSlot {
			environmentID := cmp.Or(root.def.Activity.EnvironmentID, input.Binding.EnvironmentID, "0")
			// The wait stays well inside any attempt timeout, so waiting for a slot never spends an attempt.
			activityID := root.activityFor(input.Binding.RootID)
			slotCtx, cancelSlot := context.WithTimeout(taskCtx, slotWait)
			unlockSlot := e.slots.Lock(activityID)
			slotErr := e.activities.AwaitActivitySlotBounded(slotCtx, activityID, environmentID)
			unlockSlot()
			cancelSlot()
			if slotErr != nil {
				if ctx.Err() != nil {
					return nil, classify(ctx, ctx.Err())
				}
				return nil, actor.ErrJobRejected
			}
		}
		e.mu.Lock()
		_, submitted := e.submitted[input.Binding.RootID]
		e.mu.Unlock()
		recovered := input.Binding.RunID == "" && !submitted
		output, err = fn(taskCtx, Task{Task: wt, binding: input.Binding, payload: input.Payload, recovered: recovered})
		// An interrupted handler may report a result its interruption caused, so its output is never kept.
		if err = cmp.Or(err, ctx.Err()); err != nil {
			return nil, classify(ctx, err)
		}
		return output, nil
	})
}

// taskContext rebuilds a task's job execution ctx, fencing run-bound tasks.
func (e *Engine) taskContext(ctx context.Context, binding Binding) (context.Context, error) {
	if binding.RunID == "" {
		return e.standaloneContext(ctx, binding)
	}
	run, err := e.coordinator.Get(ctx, binding.EnvironmentID, binding.JobID, binding.RunID)
	if errors.Is(err, runs.ErrRunNotFound) {
		return nil, fmt.Errorf("%w: run %s no longer exists", actor.ErrJobPermanentFailure, binding.RunID)
	}
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("load run %s: %w", binding.RunID, err))
	}
	if !runs.ActiveWorkflow(run, binding.RootID) {
		// A run waiting for its next attempt reattaches to this instance, so the instance's tasks wait with it.
		attempt := run
		attempt.Status = scheduler.Running
		if (run.Status == scheduler.Queued || run.Status == scheduler.Waiting || run.Status == scheduler.Retrying) && runs.ActiveWorkflow(attempt, binding.RootID) {
			return nil, actor.ErrJobRejected
		}
		return nil, fmt.Errorf("%w: run %s no longer follows instance %s", actor.ErrJobPermanentFailure, binding.RunID, binding.RootID)
	}
	// A task recovered at startup waits until reconcile re-authorizes its run and attaches to it.
	e.mu.Lock()
	_, admitted := e.admitted[binding.RunID]
	e.mu.Unlock()
	if !admitted {
		return nil, actor.ErrJobRejected
	}
	return e.coordinator.ExecutionContext(ctx, run, binding.RootID), nil
}

// standaloneContext gives a standalone task the execution a run would give it, with
// target progress kept in the engine's state for the instance.
func (e *Engine) standaloneContext(ctx context.Context, binding Binding) (context.Context, error) {
	select {
	case <-e.ready:
	default:
		// Work started before startup recovery collects protected records could be failed as interrupted.
		return nil, actor.ErrJobRejected
	}
	service := e.runtime.Service()
	run := scheduler.Run{ID: binding.RootID, EnvironmentID: cmp.Or(binding.EnvironmentID, "0"), Status: scheduler.Running}
	if err := service.GetState(ctx, evidenceActorType, binding.RootID, &run.Outcome.Targets); err != nil && !errors.Is(err, actor.ErrStateNotFound) {
		return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("load instance %s progress: %w", binding.RootID, err))
	}
	ctx = utils.WithActivityBatchID(ctx, binding.RootID)
	return jobcontext.WithExecution(ctx, run, func(target scheduler.TargetOutcome) error {
		defer e.evidence.Lock(binding.RootID)()
		var targets []scheduler.TargetOutcome
		if err := service.GetState(ctx, evidenceActorType, binding.RootID, &targets); err != nil && !errors.Is(err, actor.ErrStateNotFound) {
			return err
		}
		return service.SetState(ctx, evidenceActorType, binding.RootID, jobcontext.Merge(targets, target), nil)
	}), nil
}

// classify maps handler errors onto Francis delivery semantics.
func classify(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, actor.ErrJobRejected), errors.Is(err, actor.ErrJobPermanentFailure):
		return err
	case errors.Is(context.Cause(ctx), activitylib.ErrCanceled):
		// A user cancel must not be redelivered.
		return fmt.Errorf("%w: %w", actor.ErrJobPermanentFailure, activitylib.ErrCanceled)
	case errors.Is(ctx.Err(), context.Canceled):
		// Host shutdown: redeliver without spending an attempt.
		return actor.ErrJobRejected
	case Retryable(err):
		return errors.New(truncate(err.Error()))
	default:
		return fmt.Errorf("%w: %s", actor.ErrJobPermanentFailure, truncate(err.Error()))
	}
}

// Retryable reports whether another attempt could succeed. Sources classify
// transient failures; only network timeouts are assumed transient here.
func Retryable(err error) bool {
	var netErr net.Error
	return errors.Is(err, common.ErrUnavailable) || errors.Is(err, common.ErrTimeout) ||
		errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}

// Payload decodes the instance payload.
func (t Task) Payload(into any) error {
	if len(t.payload) == 0 {
		return nil
	}
	return json.Unmarshal(t.payload, into)
}

// Recovered reports a standalone task whose instance an earlier process started, so no caller is waiting on it
// and the request's own checks did not run in this process.
func (t Task) Recovered() bool { return t.recovered }

// ActivityID returns the activity owned by the task's root instance.
func (t Task) ActivityID() string { return t.binding.RootID }

// ChildInput returns the output for a step that precedes a Child step, so the
// child inherits this instance's run binding.
func (t Task) ChildInput(payload any) (any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return envelope{Binding: t.binding, Payload: encoded}, nil
}

// Results decodes a fan-out step's output slot by slot. Francis encodes a failed
// slot as an object whose only key is "error", so successful outputs must never
// have that shape.
func Results[T any](t Task, step string) ([]Result[T], error) {
	var slots []json.RawMessage
	if err := t.DecodeOutput(step, &slots); err != nil {
		return nil, err
	}
	results := make([]Result[T], len(slots))
	for index, slot := range slots {
		var failure map[string]json.RawMessage
		if json.Unmarshal(slot, &failure) == nil && len(failure) == 1 {
			var message string
			if raw, ok := failure["error"]; ok && json.Unmarshal(raw, &message) == nil {
				results[index].Err = strings.TrimPrefix(message, actor.ErrJobPermanentFailure.Error()+": ")
				continue
			}
		}
		if err := json.Unmarshal(slot, &results[index].Value); err != nil {
			return nil, fmt.Errorf("decode %s output %d: %w", step, index, err)
		}
	}
	return results, nil
}

// truncate shortens a message to maxErrorLength bytes on a rune boundary.
func truncate(message string) string {
	if len(message) <= maxErrorLength {
		return message
	}
	end := maxErrorLength
	for end > 0 && !utf8.RuneStart(message[end]) {
		end--
	}
	return message[:end] + "…"
}
