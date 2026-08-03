package actors

import (
	"context"

	"emperror.dev/errors"
)

// Runner owns one long-lived background function and joins it before stopping.
type Runner struct {
	actor *Actor
}

type runnerStateInternal struct {
	run   func(context.Context) error
	label string
}

type runnerEventInternal uint8

const runnerExitedInternal runnerEventInternal = iota

// NewRunner starts a long-lived function on the shared actor runtime.
func NewRunner(ctx context.Context, runtime *Runtime, kind, id, label string, maxRestarts int32, run func(context.Context) error) (*Runner, error) {
	if ctx == nil {
		return nil, errors.New("runner context unavailable")
	}
	if runtime == nil {
		return nil, errors.New("actor runtime unavailable")
	}
	if run == nil {
		return nil, errors.New("runner function unavailable")
	}

	applicationActor, err := NewActor(
		ctx,
		runtime,
		kind,
		id,
		maxRestarts,
		func() Behavior {
			state := &runnerStateInternal{run: run, label: label}
			return Behavior{
				Initialize: state.initializeInternal,
				Handle:     state.receiveInternal,
			}
		},
	)
	if err != nil {
		return nil, err
	}
	return &Runner{actor: applicationActor}, nil
}

// Stop cancels the background function and waits for it and its actor to exit.
func (r *Runner) Stop(ctx context.Context) error {
	if r == nil || r.actor == nil {
		return nil
	}
	return r.actor.Stop(ctx)
}

func (r *runnerStateInternal) initializeInternal(ctx *Context) {
	Worker[runnerEventInternal, NoPayload]{
		Actor:          ctx,
		WorkContext:    ctx.Context(),
		Label:          r.label,
		CompletionKind: runnerExitedInternal,
	}.Start(func(workerCtx context.Context) (NoPayload, error) {
		return NoPayload{}, r.run(workerCtx)
	})
}

func (r *runnerStateInternal) receiveInternal(_ *Context, rawMessage any) {
	message, ok := rawMessage.(Message[runnerEventInternal, NoPayload])
	if !ok {
		return
	}
	defer message.Acknowledge()
	if message.Err != nil {
		panic(message.Err)
	}
	panic(errors.New(r.label + " exited unexpectedly"))
}
