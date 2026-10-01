package backup

import (
	"context"
	"encoding/json/v2"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/host/local"
)

const backupRunTypeInternal = "backup-run"

type backupRunActorInternal struct {
	id      string
	engine  *Engine
	service *actor.Service
}

// RegisterRunKind binds a domain handler before the actor host starts.
func (e *Engine) RegisterRunKind(kind string, execute func(context.Context, string, []byte, bool) error, failures ...func(context.Context, string, []byte, error) error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[kind] = execute
	if len(failures) > 0 {
		e.failures[kind] = failures[0]
	}
}

func (e *Engine) SetAuthorize(authorize func(context.Context, backuptypes.DurableRunCommand) error) {
	e.authorize = authorize
}

func (e *Engine) SetExecutionReady(ready func() bool) { e.executionReady = ready }

func (e *Engine) Register(runtime *francis.Runtime) error {
	e.service = runtime.Service()
	return runtime.RegisterActor(backupRunTypeInternal, func(id string, service *actor.Service) actor.Actor {
		return &backupRunActorInternal{id: id, engine: e, service: service}
	}, local.WithCapacityGroup("jobs", 4), local.WithCompletedJobRetention(7*24*time.Hour))
}

// SubmitDurableRun persists the accepted command before dispatching its work.
func (e *Engine) SubmitDurableRun(ctx context.Context, command backuptypes.DurableRunCommand, lease *runs.Lease) error {
	if e == nil || e.service == nil {
		return errors.New("backup actor host is unavailable")
	}
	if e.authorize != nil {
		if err := e.authorize(ctx, command); err != nil {
			return err
		}
	}
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		return errors.New("backup engine is stopping")
	}
	e.leases[command.RunID] = lease
	e.mu.Unlock()
	_, err := e.service.Invoke(context.WithoutCancel(ctx), backupRunTypeInternal, command.RunID, "submit", command)
	if err != nil {
		e.mu.Lock()
		delete(e.leases, command.RunID)
		e.mu.Unlock()
	}
	return err
}

// AcquireDurableRun keeps the original admission, or reacquires it after a restart.
func (e *Engine) AcquireDurableRun(ctx context.Context, runID, scope, resourceID string) (*runs.Lease, bool, error) {
	e.mu.Lock()
	lease := e.leases[runID]
	delete(e.leases, runID)
	e.mu.Unlock()
	if lease != nil {
		return lease, true, nil
	}
	return e.TryAcquireRun(ctx, scope, resourceID)
}

func (a *backupRunActorInternal) Invoke(ctx context.Context, _ string, data actor.Envelope) (any, error) {
	var command backuptypes.DurableRunCommand
	if err := data.Decode(&command); err != nil {
		return nil, err
	}
	var state backuptypes.DurableRunState
	err := a.service.GetState(ctx, backupRunTypeInternal, a.id, &state)
	if errors.Is(err, actor.ErrStateNotFound) {
		state = backuptypes.DurableRunState{Command: command, Status: st.Queued}
		if err := a.service.SetState(ctx, backupRunTypeInternal, a.id, state, nil); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if state.Status == st.Succeeded || state.Status == st.Failed || state.Status == st.NeedsAttention {
		return nil, nil
	}
	_, _, err = a.service.Dispatch(ctx, backupRunTypeInternal, a.id, "execute", nil, actor.WithIdempotencyKey(a.id))
	return nil, err
}

func (a *backupRunActorInternal) Job(ctx context.Context, _ string, _ actor.Envelope) error {
	if a.engine.executionReady != nil && !a.engine.executionReady() {
		return actor.ErrJobRejected
	}
	a.engine.mu.Lock()
	if a.engine.stopping {
		a.engine.mu.Unlock()
		return actor.ErrJobRejected
	}
	a.engine.workers.Add(1)
	a.engine.mu.Unlock()
	defer a.engine.workers.Done()
	jobCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.engine.lifecycleCtx, cancel) //nolint:contextcheck // Cancels the inherited job context when the engine stops.
	defer stop()
	defer cancel()
	ctx = jobCtx

	var state backuptypes.DurableRunState
	if err := a.service.GetState(ctx, backupRunTypeInternal, a.id, &state); err != nil {
		return err
	}
	if state.Status == st.Succeeded || state.Status == st.Failed || state.Status == st.NeedsAttention {
		return nil
	}
	a.engine.mu.Lock()
	pendingLease := a.engine.leases[a.id]
	a.engine.mu.Unlock()
	defer pendingLease.Release(ctx)
	interrupted := state.Started && len(state.Targets) > 0
	state.Started = true
	state.Status = st.Running
	if err := a.service.SetState(ctx, backupRunTypeInternal, a.id, state, nil); err != nil {
		return err
	}
	var progressMu sync.Mutex
	ctx = jobcontext.WithExecution(ctx, st.Run{ID: a.id, EnvironmentID: "0", Outcome: st.Outcome{Targets: state.Targets}}, func(target st.TargetOutcome) error {
		progressMu.Lock()
		defer progressMu.Unlock()
		updated := false
		for i := range state.Targets {
			if state.Targets[i].ID == target.ID {
				if len(target.RecoveryData) == 0 {
					target.RecoveryData = state.Targets[i].RecoveryData
				}
				state.Targets[i] = target
				updated = true
				break
			}
		}
		if !updated {
			state.Targets = append(state.Targets, target)
		}
		return a.service.SetState(ctx, backupRunTypeInternal, a.id, state, nil)
	})
	runErr := a.executeInternal(ctx, state.Command, interrupted)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	state.Status = st.Succeeded
	if runErr != nil {
		state.Status = st.NeedsAttention
		state.Error = runErr.Error()
	}
	if err := a.service.SetState(ctx, backupRunTypeInternal, a.id, state, nil); err != nil {
		return err
	}
	if runErr != nil {
		return errors.WrapIf(actor.ErrJobPermanentFailure, runErr.Error())
	}
	return nil
}

func (a *backupRunActorInternal) executeInternal(ctx context.Context, command backuptypes.DurableRunCommand, interrupted bool) (err error) {
	defer utils.RecoverToError(&err, "durable backup")
	a.engine.mu.Lock()
	handler := a.engine.handlers[command.Kind]
	failure := a.engine.failures[command.Kind]
	a.engine.mu.Unlock()
	if handler == nil {
		return errors.New("backup run kind is unavailable")
	}
	if a.engine.authorize != nil {
		if err := a.engine.authorize(ctx, command); err != nil {
			a.engine.mu.Lock()
			lease := a.engine.leases[a.id]
			delete(a.engine.leases, a.id)
			a.engine.mu.Unlock()
			defer lease.Release(ctx)
			if failure != nil {
				err = errors.Combine(err, failure(ctx, a.id, command.Payload, err))
			}
			return err
		}
	}
	return handler(ctx, a.id, command.Payload, interrupted)
}

func (e *Engine) activeRunsInternal(ctx context.Context) ([]backuptypes.DurableRunState, error) {
	result := []backuptypes.DurableRunState{}
	for cursor := ""; ; {
		page, err := e.service.ListStates(ctx, backupRunTypeInternal, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, item := range page.States {
			if item.Data == nil {
				continue
			}
			var state backuptypes.DurableRunState
			if err := item.Data.Decode(&state); err != nil {
				return nil, err
			}
			if state.Status == st.Queued || state.Status == st.Running || state.Status == st.NeedsAttention {
				result = append(result, state)
			}
		}
		cursor = page.AfterID()
		if cursor == "" {
			break
		}
	}
	return result, nil
}

func (e *Engine) ActiveRunIDs(ctx context.Context) ([]string, error) {
	states, err := e.activeRunsInternal(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(states))
	for _, state := range states {
		ids = append(ids, state.Command.RunID)
		for _, target := range state.Targets {
			var checkpoint struct {
				BackupID string `json:"backupId"`
			}
			if len(target.RecoveryData) > 0 && json.Unmarshal(target.RecoveryData, &checkpoint) == nil && checkpoint.BackupID != "" {
				ids = append(ids, checkpoint.BackupID)
			}
		}
	}
	return ids, nil
}

func (e *Engine) ActiveActivityIDs(ctx context.Context) ([]string, error) {
	states, err := e.activeRunsInternal(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(states))
	for _, state := range states {
		if state.Command.ActivityID != "" {
			ids = append(ids, state.Command.ActivityID)
		}
	}
	return ids, nil
}

// ReconcileDispatches repairs accepted commands whose dispatch was interrupted.
func (e *Engine) ReconcileDispatches(ctx context.Context) error {
	states, err := e.activeRunsInternal(ctx)
	if err != nil {
		return err
	}
	for _, state := range states {
		if state.Status == st.NeedsAttention {
			continue
		}
		jobs, err := e.service.ListJobs(ctx, backupRunTypeInternal, state.Command.RunID)
		if err != nil {
			return err
		}
		live := false
		for _, job := range jobs {
			if !job.Status.IsTerminal() {
				live = true
				break
			}
			if job.Status == actor.JobStatusDeadLettered {
				if _, err := e.service.RetryJob(ctx, job.JobID); err != nil {
					return err
				}
				live = true
				break
			}
			if err := e.service.DeleteJob(ctx, backupRunTypeInternal, state.Command.RunID, job.JobID); err != nil && !errors.Is(err, actor.ErrJobNotFound) {
				return err
			}
		}
		if live {
			continue
		}
		if _, _, err := e.service.Dispatch(ctx, backupRunTypeInternal, state.Command.RunID, "execute", nil, actor.WithIdempotencyKey(state.Command.RunID)); err != nil {
			return err
		}
	}
	return nil
}
