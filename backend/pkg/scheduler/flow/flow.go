// Package flow runs durable multi-step jobs as Francis workflows inside
// coordinator runs. The run stays the source of truth for status, the Jobs UI,
// and Activity; the workflow instance is its execution graph.
//
// Migrating a job:
//  1. Write each step as a func(ctx context.Context, t flow.Task) (any, error), inline in the definition
//     unless it is shared or too complex to inline.
//     Return common.Classify(common.ErrUnavailable or common.ErrTimeout, err) to retry; other errors fail the task.
//     Tasks run at least once, so confirm external effects before repeating them.
//  2. Keep inputs and outputs small: keys, not lists of data, since Francis ships the instance input with
//     every task. Keep data in domain tables and never put secrets in a payload.
//  3. Define the workflow with Engine.Define in a RegisterWorkflows method called from the service's DI
//     provider. The last step returns scheduler.Outcome.
//  4. Register a flow.Job in place of the old job.
//  5. Cover the definition with flowtest.AssertDefinitions. On any graph or policy change, bump Version and
//     set Fingerprint to the value the test reports.
package flow

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"

	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
)

const (
	defaultMaxPayloadSize = 256 << 10
	defaultMaxJournalSize = 4 << 20
	// stepWriteInterval bounds how often step progress rewrites the run record.
	stepWriteInterval = 500 * time.Millisecond
	// evidenceActorType stores the target progress of standalone instances, which have no run to hold it.
	evidenceActorType = "workflow-evidence"
)

var (
	// ErrNotResumable reports that a run has no workflow instance this host can serve.
	ErrNotResumable = errors.New("workflow instance cannot be resumed")
	// errInstanceFinished releases a root's cancellation once the instance ends.
	errInstanceFinished = errors.New("workflow instance finished")
)

type (
	// Definition declares a workflow. Bump Version and update Fingerprint on any
	// graph or policy change; flowtest.AssertDefinitions reports the new value.
	Definition struct {
		Name        string
		Version     int
		Fingerprint string
		Concurrency int
		Timeout     time.Duration
		// Activity is the template for the activity each root instance owns; an empty Type means none.
		Activity activitylib.StartRequest
		// Labels name steps on the activity; steps without one show their name.
		Labels map[string]string
		// Steps use Francis step builders with handlers wrapped by Engine.Handler.
		// The last step returns scheduler.Outcome.
		Steps   []workflow.StepSpec
		Options []workflow.Option
	}

	// Workflow is a defined workflow registered on the engine's host.
	Workflow struct {
		def Definition
		wf  *workflow.Workflow
		svc *workflow.WorkflowService
	}

	// Activities is the activity service surface the engine drives.
	Activities interface {
		activitylib.Service
		activitylib.MessageAppender
		activitylib.Tracker
		UpdateActivity(ctx context.Context, activityID string, req activitylib.UpdateRequest) (*activitytypes.Activity, error)
		AwaitActivitySlotBounded(ctx context.Context, activityID, environmentID string) error
	}

	// Engine defines workflows, runs them inside coordinator runs, and owns their activities.
	Engine struct {
		runtime     *francis.Runtime
		coordinator *runs.Coordinator
		activities  Activities
		ctx         context.Context
		cancel      context.CancelFunc
		mu          sync.Mutex
		workflows   []*Workflow
		monitors    map[string]*instanceMonitor
		roots       map[string]rootCancellation
		evidence    utils.KeyedMutex
		// slots serializes reacquiring a root activity's slot, so its sibling tasks never wait on the slot it holds.
		slots utils.KeyedMutex
		// tracked holds each starting activity's cancel registration until its monitor takes it over.
		tracked map[string]context.Context
		// admitted holds the runs whose executor authorized them and waits on them; other runs' tasks wait for reconcile.
		admitted map[string]struct{}
		// ready closes once startup recovery has protected standalone work; until then standalone tasks wait.
		ready chan struct{}
		// submitted holds the standalone instances this process started; tasks of others are recovered.
		submitted map[string]struct{}
		readyOnce sync.Once
	}

	// rootCancellation interrupts every running task of one root instance.
	rootCancellation struct {
		ctx    context.Context
		cancel context.CancelCauseFunc
	}
)

// New builds an engine whose monitors live until appCtx ends or Stop is called.
func New(appCtx context.Context, runtime *francis.Runtime, coordinator *runs.Coordinator, activities Activities) (*Engine, error) {
	if err := runtime.RegisterActor(evidenceActorType, func(string, *actor.Service) actor.Actor { return struct{}{} }); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(appCtx)
	return &Engine{
		runtime:     runtime,
		coordinator: coordinator,
		activities:  activities,
		ctx:         ctx,
		cancel:      cancel,
		monitors:    map[string]*instanceMonitor{},
		roots:       map[string]rootCancellation{},
		tracked:     map[string]context.Context{},
		admitted:    map[string]struct{}{},
		ready:       make(chan struct{}),
		submitted:   map[string]struct{}{},
	}, nil
}

// Define validates and registers a workflow. It must run before the host starts.
func (e *Engine) Define(def Definition) (*Workflow, error) {
	switch {
	case def.Version < 1:
		return nil, fmt.Errorf("workflow %q requires a version", def.Name)
	case def.Concurrency < 1:
		return nil, fmt.Errorf("workflow %q requires a concurrency", def.Name)
	case def.Timeout <= 0:
		return nil, fmt.Errorf("workflow %q requires a timeout", def.Name)
	case len(def.Steps) == 0:
		return nil, fmt.Errorf("workflow %q requires steps", def.Name)
	}
	options := append([]workflow.Option{
		workflow.WithVersion(def.Version),
		workflow.WithConcurrency(def.Concurrency),
		workflow.WithTimeout(def.Timeout),
		workflow.WithSteps(def.Steps...),
		workflow.WithUnknownVersionPolicy(workflow.FailUnknownVersion),
		workflow.WithMaxInputSize(defaultMaxPayloadSize),
		workflow.WithMaxOutputSize(defaultMaxPayloadSize),
		workflow.WithMaxJournalSize(defaultMaxJournalSize),
		workflow.WithLogger(slog.Default().With("scope", "workflow")),
	}, def.Options...)
	wf, err := workflow.New(def.Name, options...)
	if err != nil {
		return nil, fmt.Errorf("define workflow %q: %w", def.Name, err)
	}
	if registerErr := e.runtime.RegisterWorkflow(wf); registerErr != nil {
		return nil, registerErr
	}
	defined := &Workflow{def: def, wf: wf, svc: wf.Service(e.runtime.Service())}
	e.mu.Lock()
	e.workflows = append(e.workflows, defined)
	e.mu.Unlock()
	return defined, nil
}

// Fingerprint is the graph hash the definition declares for its version.
func (w *Workflow) Fingerprint() string { return w.def.Fingerprint }

// Francis exposes the underlying definition for use with workflow.WithChild.
func (w *Workflow) Francis() *workflow.Workflow { return w.wf }

// Workflows lists every defined workflow.
func (e *Engine) Workflows() []*Workflow {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.workflows)
}

// Start monitors every unfinished root instance so activities complete without a caller.
func (e *Engine) Start(ctx context.Context) error {
	return e.eachRoot(ctx, func(wf *Workflow, instanceID string) error {
		e.monitor(wf, instanceID)
		return nil
	})
}

// Ready lets standalone tasks run once startup recovery has protected the work Standalone reported.
func (e *Engine) Ready() {
	e.readyOnce.Do(func() { close(e.ready) })
}

// Standalone lists unfinished instances started by Submit or an unbound Run as runs of the workflow's
// name, carrying the targets their tasks recorded, so startup recovery can protect their work.
func (e *Engine) Standalone(ctx context.Context) ([]scheduler.Run, error) {
	var standalone []scheduler.Run
	err := e.eachRoot(ctx, func(wf *Workflow, instanceID string) error {
		if isInstanceOf(instanceID, wf.def.Name) {
			return nil
		}
		run := scheduler.Run{ID: instanceID, JobID: wf.def.Name, ActivityID: wf.activityFor(instanceID), Status: scheduler.Running}
		if err := e.runtime.Service().GetState(ctx, evidenceActorType, instanceID, &run.Outcome.Targets); err != nil && !errors.Is(err, actor.ErrStateNotFound) {
			return fmt.Errorf("load %s progress: %w", instanceID, err)
		}
		standalone = append(standalone, run)
		return nil
	})
	return standalone, err
}

// eachRoot visits every unfinished root instance of every defined workflow.
func (e *Engine) eachRoot(ctx context.Context, visit func(wf *Workflow, instanceID string) error) error {
	for _, wf := range e.Workflows() {
		for _, status := range []workflow.Status{workflow.StatusPending, workflow.StatusRunning, workflow.StatusSuspended, workflow.StatusCompensating} {
			after := ""
			for {
				page, err := wf.svc.List(ctx, &workflow.ListOptions{Status: status, After: after, Limit: 100})
				if err != nil {
					return fmt.Errorf("list %s workflows: %w", wf.def.Name, err)
				}
				for _, instance := range page.Instances {
					if instance.Parent != nil {
						continue
					}
					if visitErr := visit(wf, instance.InstanceID); visitErr != nil {
						return visitErr
					}
				}
				if after = page.AfterID(); after == "" {
					break
				}
			}
		}
	}
	return nil
}

// Stop ends monitoring; unfinished instances resume on the next Start.
func (e *Engine) Stop(context.Context) error {
	e.cancel()
	return nil
}

// Run starts the workflow, or attaches to the run's active instance, and waits for its outcome. A run in
// ctx (jobcontext.Run) binds the instance to that run; set fields of activity override the definition's
// activity template; a non-nil output receives the last step's data alongside its scheduler.Outcome fields.
func (e *Engine) Run(ctx context.Context, wf *Workflow, payload any, activity activitylib.StartRequest, output any) (scheduler.Outcome, error) {
	run, bound := jobcontext.Run(ctx)
	instanceID := ""
	if bound {
		defer e.admit(run.ID)()
		if target, ok := activeTarget(run, wf.def.Name, false); ok {
			status, err := wf.svc.GetStatus(ctx, target.ID)
			if err != nil && !errors.Is(err, workflow.ErrInstanceNotFound) {
				return scheduler.Outcome{}, fmt.Errorf("read %s instance: %w", wf.def.Name, err)
			}
			if err == nil && status.Version == wf.def.Version {
				instanceID = target.ID
			} else {
				e.supersede(ctx, wf, target, "Superseded by a newer workflow instance")
			}
		}
	}
	if instanceID == "" {
		var err error
		if instanceID, err = e.startInstance(ctx, wf, run, bound, payload, activity, nil); err != nil {
			return scheduler.Outcome{Status: scheduler.Failed, Message: err.Error()}, err
		}
	}
	outcome, err := e.await(ctx, wf, run, bound, instanceID)
	if err != nil || output == nil {
		return outcome, err
	}
	status, err := wf.svc.GetStatus(ctx, instanceID)
	if err != nil {
		return outcome, fmt.Errorf("read %s output: %w", wf.def.Name, err)
	}
	return outcome, status.DecodeOutput(output)
}

// Submit starts a standalone instance without waiting for it and returns its ID,
// which is also its activity's ID. Its tasks record target progress like a run's;
// evidence is recorded before the instance starts, such as the record it owns.
func (e *Engine) Submit(ctx context.Context, wf *Workflow, payload any, activity activitylib.StartRequest, evidence ...scheduler.TargetOutcome) (string, error) {
	instanceID, err := e.startInstance(ctx, wf, scheduler.Run{}, false, payload, activity, evidence)
	if err != nil {
		return "", err
	}
	e.monitor(wf, instanceID)
	return instanceID, nil
}

// Wait blocks until a submitted instance finishes, ctx ends, or the engine stops.
func (e *Engine) Wait(ctx context.Context, wf *Workflow, instanceID string) (scheduler.Outcome, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(e.ctx, cancel)()
	return e.await(ctx, wf, scheduler.Run{}, false, instanceID)
}

// Resume attaches to the run's active instance, or reads the result of one that finished before the run saved it.
// It returns ErrNotResumable when the instance is gone or belongs to a version this host no longer serves.
func (e *Engine) Resume(ctx context.Context, wf *Workflow, run scheduler.Run) (scheduler.Outcome, error) {
	target, ok := activeTarget(run, wf.def.Name, true)
	if !ok {
		return scheduler.Outcome{}, ErrNotResumable
	}
	defer e.admit(run.ID)()
	status, err := wf.svc.GetStatus(ctx, target.ID)
	if err != nil && !errors.Is(err, workflow.ErrInstanceNotFound) {
		return scheduler.Outcome{}, fmt.Errorf("read %s instance: %w", wf.def.Name, err)
	}
	if err != nil || status.Version != wf.def.Version {
		if !target.Status.Terminal() {
			e.supersede(ctx, wf, target, "Workflow instance could not be resumed")
		}
		return scheduler.Outcome{}, ErrNotResumable
	}
	return e.await(ctx, wf, run, true, target.ID)
}

// Retire cancels the run's unfinished instance without resuming it, such as for a job that was disabled. An instance
// that finished before the run saved its result reports that result instead, with finished set.
func (e *Engine) Retire(ctx context.Context, wf *Workflow, run scheduler.Run, reason string) (outcome scheduler.Outcome, finished bool, err error) {
	if target, ok := activeTarget(run, wf.def.Name, true); ok && target.Status.Terminal() {
		if outcome, err = e.Resume(ctx, wf, run); !errors.Is(err, ErrNotResumable) {
			return outcome, true, err
		}
	}
	if target, ok := activeTarget(run, wf.def.Name, false); ok {
		e.supersede(ctx, wf, target, reason)
	}
	return scheduler.Outcome{}, false, nil
}

func (e *Engine) startInstance(ctx context.Context, wf *Workflow, run scheduler.Run, bound bool, payload any, activity activitylib.StartRequest, evidence []scheduler.TargetOutcome) (string, error) {
	instanceID := wf.def.Name + "-" + uuid.NewV7().String()
	binding := Binding{RootID: instanceID}
	if bound {
		instanceID = fmt.Sprintf("%s-%s-%d", wf.def.Name, run.ID, run.AttemptCount)
		binding = Binding{EnvironmentID: run.EnvironmentID, JobID: run.JobID, RunID: run.ID, RootID: instanceID}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode %s input: %w", wf.def.Name, err)
	}
	activityID := wf.activityFor(instanceID)
	if bound {
		// Record the instance first so its tasks pass the run fence.
		target := scheduler.TargetOutcome{ResourceType: scheduler.WorkflowTarget, ID: instanceID, Status: scheduler.Running, ActivityID: activityID}
		if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
			return "", fmt.Errorf("record %s instance: %w", wf.def.Name, progressErr)
		}
	}
	if activityID != "" {
		if activityErr := e.startActivity(ctx, wf, activityID, cmp.Or(run.EnvironmentID, "0"), activity); activityErr != nil {
			return "", activityErr
		}
	}
	if !bound {
		e.mu.Lock()
		e.submitted[instanceID] = struct{}{}
		e.mu.Unlock()
	}
	startErr := error(nil)
	if len(evidence) > 0 {
		startErr = e.runtime.Service().SetState(ctx, evidenceActorType, instanceID, evidence, nil)
	}
	if startErr == nil {
		_, _, startErr = wf.svc.Start(ctx, envelope{Binding: binding, Payload: encoded}, workflow.WithInstanceID(instanceID))
	}
	if startErr != nil {
		e.releaseRoot(instanceID)
		if len(evidence) > 0 {
			_ = e.runtime.Service().DeleteState(context.WithoutCancel(ctx), evidenceActorType, instanceID)
		}
		if activityID != "" {
			e.untrack(activityID)
			message := startErr.Error()
			_, _ = e.activities.CompleteActivity(ctx, activityID, activitytypes.StatusFailed, "Failed to start workflow", &message)
		}
		return "", fmt.Errorf("start %s: %w", wf.def.Name, startErr)
	}
	return instanceID, nil
}

func (e *Engine) startActivity(ctx context.Context, wf *Workflow, activityID, environmentID string, details activitylib.StartRequest) error {
	request := wf.def.Activity
	request.ID = activityID
	request.EnvironmentID = cmp.Or(details.EnvironmentID, request.EnvironmentID, environmentID)
	request.ResourceType = cmp.Or(details.ResourceType, request.ResourceType)
	request.ResourceID = cmp.Or(details.ResourceID, request.ResourceID)
	request.ResourceName = cmp.Or(details.ResourceName, request.ResourceName)
	request.StartedBy = cmp.Or(details.StartedBy, request.StartedBy)
	request.Step = cmp.Or(details.Step, request.Step)
	request.LatestMessage = cmp.Or(details.LatestMessage, request.LatestMessage)
	if details.Metadata != nil {
		request.Metadata = details.Metadata
	}
	// The registration a user cancel reaches exists before the activity does, so no cancel is missed; the
	// monitor takes it over once the instance runs.
	tracked := e.activities.Track(e.ctx, activityID)
	e.mu.Lock()
	e.tracked[activityID] = tracked
	e.mu.Unlock()
	if _, err := e.activities.StartActivity(ctx, request); err != nil {
		e.untrack(activityID)
		// Completing drops the service's cancel registration and settles a row the failed start may have left.
		message := err.Error()
		_, _ = e.activities.CompleteActivity(context.WithoutCancel(ctx), activityID, activitytypes.StatusFailed, "Failed to start workflow", &message)
		return fmt.Errorf("start %s activity: %w", wf.def.Name, err)
	}
	// A deferred slot is awaited by the task that does the work, so a submit never blocks on it.
	if request.Queue && !request.DeferSlot {
		waitCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		defer context.AfterFunc(tracked, cancel)()
		if err := e.activities.AwaitActivitySlotBounded(waitCtx, activityID, request.EnvironmentID); err != nil && !errors.Is(context.Cause(tracked), activitylib.ErrCanceled) {
			e.untrack(activityID)
			message := err.Error()
			_, _ = e.activities.CompleteActivity(context.WithoutCancel(ctx), activityID, activitytypes.StatusFailed, "Failed to start workflow", &message)
			return err
		}
	}
	if errors.Is(context.Cause(tracked), activitylib.ErrCanceled) {
		e.untrack(activityID)
		_, _ = e.activities.CompleteActivity(context.WithoutCancel(ctx), activityID, activitytypes.StatusCancelled, activitylib.ErrCanceled.Error(), nil)
		return fmt.Errorf("start %s: %w", wf.def.Name, activitylib.ErrCanceled)
	}
	return nil
}

// untrack drops a starting activity's registration; the engine only holds it until the monitor starts.
func (e *Engine) untrack(activityID string) {
	e.mu.Lock()
	delete(e.tracked, activityID)
	e.mu.Unlock()
}

// await waits for the instance's outcome, projecting steps onto a bound run at
// most every stepWriteInterval.
func (e *Engine) await(ctx context.Context, wf *Workflow, run scheduler.Run, bound bool, instanceID string) (scheduler.Outcome, error) {
	monitor := e.monitor(wf, instanceID)
	var recorded []scheduler.StepOutcome
	var lastWrite time.Time
	for {
		steps, outcome, changed := monitor.snapshot()
		var flush <-chan time.Time
		if bound && !stepsEqual(recorded, steps) {
			if wait := stepWriteInterval - time.Since(lastWrite); outcome == nil && wait > 0 {
				flush = time.After(wait)
			} else if err := e.coordinator.RecordSteps(ctx, run, instanceID, steps); err != nil && ctx.Err() == nil {
				slog.DebugContext(ctx, "Failed to record workflow steps", "instanceId", instanceID, "error", err)
			} else {
				recorded, lastWrite = steps, time.Now()
			}
		}
		if outcome != nil {
			if bound {
				// Bound to the executor: a write lost to shutdown is redone when reconcile re-attaches.
				target := scheduler.TargetOutcome{ResourceType: scheduler.WorkflowTarget, ID: instanceID, Status: outcome.Status, ActivityID: wf.activityFor(instanceID)}
				// Run never attaches a finished instance, so a retry starts a new one.
				if !target.Status.Terminal() {
					target.Status = scheduler.Failed
				}
				if err := jobcontext.Progress(ctx, target); err != nil {
					slog.WarnContext(ctx, "Failed to record workflow completion", "instanceId", instanceID, "error", err)
				}
			}
			return *outcome, nil
		}
		select {
		case <-ctx.Done():
			return scheduler.Outcome{}, ctx.Err()
		case <-changed:
		case <-flush:
		}
	}
}

// supersede retires an instance that a run no longer follows.
func (e *Engine) supersede(ctx context.Context, wf *Workflow, target scheduler.TargetOutcome, reason string) {
	target.Status = scheduler.Failed
	target.Message = reason
	if err := jobcontext.Progress(ctx, target); err != nil {
		slog.WarnContext(ctx, "Failed to retire workflow instance", "instanceId", target.ID, "error", err)
	}
	if err := wf.svc.Cancel(ctx, target.ID, reason); err != nil && !errors.Is(err, workflow.ErrInstanceNotFound) && !errors.Is(err, workflow.ErrInstanceTerminated) {
		slog.DebugContext(ctx, "Failed to cancel superseded workflow instance", "instanceId", target.ID, "error", err)
	}
	e.mu.Lock()
	monitor := e.monitors[target.ID]
	delete(e.monitors, target.ID)
	e.mu.Unlock()
	if monitor != nil {
		monitor.stop()
	}
	e.releaseRoot(target.ID)
	if target.ActivityID != "" {
		_, _ = e.activities.CompleteActivity(ctx, target.ActivityID, activitytypes.StatusFailed, reason, new(reason))
	}
}

// admit lets the run's tasks execute until the returned func is called.
func (e *Engine) admit(runID string) func() {
	e.mu.Lock()
	e.admitted[runID] = struct{}{}
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		delete(e.admitted, runID)
		e.mu.Unlock()
	}
}

// rootContext is canceled, with the user's cause, when a root instance is canceled.
func (e *Engine) rootContext(rootID string) context.Context {
	e.mu.Lock()
	defer e.mu.Unlock()
	root, ok := e.roots[rootID]
	if !ok {
		ctx, cancel := context.WithCancelCause(e.ctx)
		root = rootCancellation{ctx: ctx, cancel: cancel}
		e.roots[rootID] = root
	}
	return root.ctx
}

// cancelRoot interrupts the running tasks of a root instance and its children.
func (e *Engine) cancelRoot(rootID string, cause error) {
	e.mu.Lock()
	root, ok := e.roots[rootID]
	e.mu.Unlock()
	if ok {
		root.cancel(cause)
	}
}

func (e *Engine) releaseRoot(rootID string) {
	e.mu.Lock()
	root, ok := e.roots[rootID]
	delete(e.roots, rootID)
	delete(e.submitted, rootID)
	e.mu.Unlock()
	if ok {
		root.cancel(errInstanceFinished)
	}
}

// activeTarget returns the run's latest instance of a workflow and whether the run may attach to it: an unfinished
// instance, or when recovering, a finished one whose attempt ended before the run saved its result.
func activeTarget(run scheduler.Run, name string, recovering bool) (scheduler.TargetOutcome, bool) {
	for _, target := range slices.Backward(run.Outcome.Targets) {
		if target.ResourceType != scheduler.WorkflowTarget || !isInstanceOf(target.ID, name) {
			continue
		}
		if !recovering || !target.Status.Terminal() {
			return target, !target.Status.Terminal()
		}
		attempt, err := strconv.Atoi(target.ID[strings.LastIndexByte(target.ID, '-')+1:])
		if err != nil {
			return target, false
		}
		// Redeliveries repeat an attempt's number; only its latest entry records whether the result was saved.
		for _, previous := range slices.Backward(run.Attempts) {
			if previous.Number == attempt {
				return target, previous.FinishedAt == nil
			}
		}
		return target, false
	}
	return scheduler.TargetOutcome{}, false
}

// isInstanceOf matches run-bound instance IDs of the form <name>-<runID>-<attempt>.
func isInstanceOf(instanceID, name string) bool {
	rest, ok := strings.CutPrefix(instanceID, name+"-")
	if !ok || len(rest) < 38 || rest[36] != '-' {
		return false
	}
	_, err := uuid.Parse(rest[:36])
	return err == nil
}

// rootWorkflow returns the workflow a root instance ID belongs to: <name>-<uuid> standalone, or <name>-<runID>-<attempt>.
func (e *Engine) rootWorkflow(rootID string) *Workflow {
	for _, wf := range e.Workflows() {
		rest, ok := strings.CutPrefix(rootID, wf.def.Name+"-")
		if !ok {
			continue
		}
		if _, err := uuid.Parse(rest); err == nil || isInstanceOf(rootID, wf.def.Name) {
			return wf
		}
	}
	return nil
}

// activityFor returns the activity an instance owns, or "" when the workflow has
// none. The instance ID doubles as the activity ID, so redelivery finds the same row.
func (w *Workflow) activityFor(instanceID string) string {
	if w.def.Activity.Type == "" {
		return ""
	}
	return instanceID
}

// stepLabel names a step for the activity.
func (w *Workflow) stepLabel(name string) string {
	return cmp.Or(w.def.Labels[name], name)
}
