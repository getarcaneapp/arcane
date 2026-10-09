package flow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"

	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
)

const (
	minPollInterval = 100 * time.Millisecond
	maxPollInterval = time.Second
)

type (
	// instanceMonitor is the single poller for one root instance. It owns the
	// instance's activity and broadcasts step changes and the final outcome.
	instanceMonitor struct {
		mu      sync.Mutex
		steps   []scheduler.StepOutcome
		outcome *scheduler.Outcome
		changed chan struct{}
		cancel  context.CancelFunc
		// children caches the workflow serving each child instance.
		children map[string]*Workflow
	}

	// activityProjection remembers what was last written to the activity.
	activityProjection struct {
		progress int
		step     string
		statuses map[string]scheduler.RunStatus
	}
)

func (e *Engine) monitor(wf *Workflow, instanceID string) *instanceMonitor {
	e.mu.Lock()
	defer e.mu.Unlock()
	if monitor := e.monitors[instanceID]; monitor != nil {
		return monitor
	}
	ctx, cancel := context.WithCancel(e.ctx)
	monitor := &instanceMonitor{changed: make(chan struct{}), cancel: cancel, children: map[string]*Workflow{}}
	e.monitors[instanceID] = monitor
	go e.poll(ctx, wf, instanceID, monitor)
	return monitor
}

func (e *Engine) poll(ctx context.Context, wf *Workflow, instanceID string, monitor *instanceMonitor) {
	activityID := wf.activityFor(instanceID)
	var canceled <-chan struct{}
	trackedCtx := ctx
	if activityID != "" {
		// A starting instance's activity is already tracked; one found on Start is tracked here.
		e.mu.Lock()
		trackedCtx = e.tracked[activityID]
		delete(e.tracked, activityID)
		e.mu.Unlock()
		if trackedCtx == nil {
			trackedCtx = e.activities.Track(ctx, activityID)
		}
		canceled = trackedCtx.Done()
	}
	userCanceled := false
	projection := activityProjection{progress: -1, statuses: map[string]scheduler.RunStatus{}}
	delay := minPollInterval
	for {
		status, err := wf.svc.GetStatus(ctx, instanceID)
		switch {
		case err == nil:
			steps := e.projectSteps(ctx, monitor, status)
			if status.Status.IsTerminal() {
				outcome := decodeOutcome(status, steps, activityID)
				// The interrupted task may fail the instance before the cancel turn lands.
				if userCanceled && outcome.Status == scheduler.Failed {
					outcome.Status, outcome.Message = scheduler.Canceled, activitylib.ErrCanceled.Error()
				}
				e.finish(ctx, wf, instanceID, monitor, outcome)
				return
			}
			if monitor.publish(steps, nil) {
				e.projectActivity(ctx, wf, activityID, &projection, steps)
				delay = minPollInterval
			}
		case errors.Is(err, workflow.ErrInstanceNotFound):
			e.finish(ctx, wf, instanceID, monitor, scheduler.Outcome{Status: scheduler.Failed, Message: "Workflow instance no longer exists", ActivityID: activityID})
			return
		case ctx.Err() == nil:
			slog.DebugContext(ctx, "Failed to read workflow status", "instanceId", instanceID, "error", err)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-canceled:
			canceled = nil
			if errors.Is(context.Cause(trackedCtx), activitylib.ErrCanceled) {
				userCanceled = true
				if cancelErr := wf.svc.Cancel(ctx, instanceID, activitylib.ErrCanceled.Error()); cancelErr != nil {
					slog.WarnContext(ctx, "Failed to cancel workflow", "instanceId", instanceID, "error", cancelErr)
				}
				e.cancelRoot(instanceID, activitylib.ErrCanceled)
			}
		case <-timer.C:
		}
		timer.Stop()
		delay = min(delay*2, maxPollInterval)
	}
}

func (e *Engine) finish(ctx context.Context, wf *Workflow, instanceID string, monitor *instanceMonitor, outcome scheduler.Outcome) {
	e.mu.Lock()
	if e.monitors[instanceID] == monitor {
		delete(e.monitors, instanceID)
	}
	e.mu.Unlock()
	e.releaseRoot(instanceID)
	if !isInstanceOf(instanceID, wf.def.Name) {
		if err := e.runtime.Service().DeleteState(context.WithoutCancel(ctx), evidenceActorType, instanceID); err != nil && !errors.Is(err, actor.ErrStateNotFound) {
			slog.WarnContext(ctx, "Failed to delete workflow progress", "instanceId", instanceID, "error", err)
		}
	}
	activityID := wf.activityFor(instanceID)
	if activityID != "" {
		status := activitytypes.StatusSuccess
		var errMessage *string
		switch outcome.Status {
		case scheduler.Canceled:
			status = activitytypes.StatusCancelled
		case scheduler.Failed, scheduler.Partial, scheduler.NeedsAttention:
			status = activitytypes.StatusFailed
			errMessage = new(outcome.Message)
		case scheduler.Queued, scheduler.Waiting, scheduler.Running, scheduler.Retrying, scheduler.Succeeded, scheduler.Skipped:
		}
		if _, err := e.activities.CompleteActivity(context.WithoutCancel(ctx), activityID, status, outcome.Message, errMessage); err != nil {
			slog.WarnContext(ctx, "Failed to complete workflow activity", "instanceId", instanceID, "error", err)
		}
	}
	monitor.publish(outcome.Steps, &outcome)
	monitor.cancel()
}

// stop ends a monitor whose instance was superseded.
func (m *instanceMonitor) stop() {
	m.cancel()
	m.publish(nil, &scheduler.Outcome{Status: scheduler.Failed, Message: "Workflow instance was superseded"})
}

// publish records a change and wakes waiters; it reports whether anything changed.
func (m *instanceMonitor) publish(steps []scheduler.StepOutcome, outcome *scheduler.Outcome) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.outcome != nil || (outcome == nil && stepsEqual(m.steps, steps)) {
		return false
	}
	m.steps = steps
	m.outcome = outcome
	close(m.changed)
	m.changed = make(chan struct{})
	return true
}

func (m *instanceMonitor) snapshot() ([]scheduler.StepOutcome, *scheduler.Outcome, <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.steps, m.outcome, m.changed
}

// projectSteps maps Francis steps onto run steps; a child step reports its child's fan-out totals.
func (e *Engine) projectSteps(ctx context.Context, monitor *instanceMonitor, status workflow.InstanceStatus) []scheduler.StepOutcome {
	steps := make([]scheduler.StepOutcome, 0, len(status.Steps))
	for _, view := range status.Steps {
		step := scheduler.StepOutcome{
			Name:      view.Name,
			Total:     view.Tasks,
			Completed: view.Completed,
			Failed:    view.Failed,
			Attempts:  view.Attempts,
			Message:   view.Error,
		}
		if view.Kind == workflow.KindChild && len(view.ChildIDs) == 1 {
			if child, ok := e.childStatus(ctx, monitor, view.ChildIDs[0]); ok {
				for _, childStep := range child.Steps {
					if childStep.Kind == workflow.KindForEach {
						step.Total, step.Completed, step.Failed = childStep.Tasks, childStep.Completed, childStep.Failed
					}
				}
			}
		}
		step.Status = stepStatus(view.Status, step.Failed)
		if !view.StartedAt.IsZero() {
			step.StartedAt = new(view.StartedAt)
		}
		if !view.CompletedAt.IsZero() {
			step.FinishedAt = new(view.CompletedAt)
		}
		steps = append(steps, step)
	}
	return steps
}

// childStatus reads a child instance, resolving its workflow once per monitor.
func (e *Engine) childStatus(ctx context.Context, monitor *instanceMonitor, childID string) (workflow.InstanceStatus, bool) {
	if wf := monitor.children[childID]; wf != nil {
		status, err := wf.svc.GetStatus(ctx, childID)
		return status, err == nil
	}
	for _, wf := range e.Workflows() {
		if status, err := wf.svc.GetStatus(ctx, childID); err == nil {
			monitor.children[childID] = wf
			return status, true
		}
	}
	return workflow.InstanceStatus{}, false
}

func stepStatus(status workflow.StepStatus, failed int) scheduler.RunStatus {
	switch status {
	case workflow.StepRunning, workflow.StepCompensating:
		return scheduler.Running
	case workflow.StepCompleted:
		if failed > 0 {
			return scheduler.Partial
		}
		return scheduler.Succeeded
	case workflow.StepFailed, workflow.StepCompensationFailed:
		return scheduler.Failed
	case workflow.StepSkipped:
		return scheduler.Skipped
	case workflow.StepCompensated:
		return scheduler.Canceled
	case workflow.StepPending:
		return scheduler.Queued
	}
	return scheduler.Queued
}

// decodeOutcome decodes a terminal instance into the run outcome contract.
func decodeOutcome(status workflow.InstanceStatus, steps []scheduler.StepOutcome, activityID string) scheduler.Outcome {
	var outcome scheduler.Outcome
	switch status.Status {
	case workflow.StatusCompleted:
		if err := status.DecodeOutput(&outcome); err != nil {
			outcome = scheduler.Outcome{Status: scheduler.Failed, Message: "Workflow returned an invalid outcome: " + err.Error()}
		}
		if outcome.Status == "" {
			outcome.Status = scheduler.Succeeded
		}
	case workflow.StatusCancelled:
		outcome = scheduler.Outcome{Status: scheduler.Canceled, Message: status.Cause}
	case workflow.StatusFailed, workflow.StatusPending, workflow.StatusRunning, workflow.StatusSuspended, workflow.StatusCompensating:
		outcome = scheduler.Outcome{Status: scheduler.Failed, Message: status.Cause}
	}
	outcome.Steps = steps
	if outcome.ActivityID == "" {
		outcome.ActivityID = activityID
	}
	return outcome
}

// projectActivity writes overall progress and step transitions to the activity.
func (e *Engine) projectActivity(ctx context.Context, wf *Workflow, activityID string, projection *activityProjection, steps []scheduler.StepOutcome) {
	if activityID == "" || len(steps) == 0 {
		return
	}
	done := 0.0
	current := ""
	for _, step := range steps {
		previous, seen := projection.statuses[step.Name]
		if step.Status != scheduler.Queued && (!seen || previous != step.Status) {
			projection.statuses[step.Name] = step.Status
			if step.Status != scheduler.Running {
				e.appendStepMessage(ctx, activityID, wf.stepLabel(step.Name), step)
			}
		}
		switch {
		case step.Status == scheduler.Running:
			current = wf.stepLabel(step.Name)
			if step.Total > 0 {
				done += float64(step.Completed+step.Failed) / float64(step.Total)
			}
		case step.Status != scheduler.Queued:
			done++
		}
	}
	progress := int(done * 100 / float64(len(steps)))
	if progress == projection.progress && current == projection.step {
		return
	}
	projection.progress, projection.step = progress, current
	if _, err := e.activities.UpdateActivity(ctx, activityID, activitylib.UpdateRequest{Progress: &progress, Step: &current}); err != nil {
		slog.DebugContext(ctx, "Failed to update workflow activity progress", "activityId", activityID, "error", err)
	}
}

func (e *Engine) appendStepMessage(ctx context.Context, activityID, label string, step scheduler.StepOutcome) {
	level := activitytypes.MessageLevelInfo
	message := fmt.Sprintf("%s: %s", label, step.Status)
	if step.Total > 1 {
		message += fmt.Sprintf(" (%d/%d", step.Completed, step.Total)
		if step.Failed > 0 {
			message += fmt.Sprintf(", %d failed", step.Failed)
		}
		message += ")"
	}
	switch step.Status {
	case scheduler.Failed:
		level = activitytypes.MessageLevelError
		if step.Message != "" {
			message += ": " + step.Message
		}
	case scheduler.Partial:
		level = activitytypes.MessageLevelWarning
	case scheduler.Queued, scheduler.Waiting, scheduler.Running, scheduler.Retrying, scheduler.Succeeded, scheduler.Skipped, scheduler.NeedsAttention, scheduler.Canceled:
	}
	if _, err := e.activities.AppendMessage(ctx, activityID, activitylib.AppendMessageRequest{Level: level, Message: message}); err != nil {
		slog.DebugContext(ctx, "Failed to append workflow step message", "activityId", activityID, "error", err)
	}
}

func stepsEqual(left, right []scheduler.StepOutcome) bool {
	return slices.EqualFunc(left, right, func(a, b scheduler.StepOutcome) bool {
		return a.Name == b.Name && a.Status == b.Status && a.Total == b.Total && a.Completed == b.Completed &&
			a.Failed == b.Failed && a.Attempts == b.Attempts && a.Message == b.Message
	})
}
