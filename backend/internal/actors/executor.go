package actors

import (
	"context"
	"sync"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// Executor serializes heterogeneous blocking tasks through one actor mailbox.
// A task must not synchronously submit another task to the same Executor.
type Executor struct {
	actor *Actor
}

// Task is one submitted executor operation whose result may be awaited with a
// context independent from the operation's own lifetime.
type Task[T any] struct {
	result *Promise[executorResultInternal[T]]
	actor  *Actor
}

type executorTaskInternal interface {
	executeInternal(actorCtx context.Context)
}

type executorResultInternal[T any] struct {
	value T
	err   error
}

type executorTaskValueInternal[T any] struct {
	ctx     context.Context
	label   string
	work    func(context.Context) (T, error)
	after   func(T, error)
	result  *Promise[executorResultInternal[T]]
	started *sync.Once
}

// NewExecutor creates a serial task executor on the shared actor runtime.
func NewExecutor(ctx context.Context, runtime *Runtime, kind, id string, maxRestarts int32) (*Executor, error) {
	if runtime == nil {
		return nil, errors.New("actor runtime unavailable")
	}
	if ctx == nil {
		return nil, errors.New("executor context unavailable")
	}
	applicationActor, err := NewActor(
		ctx,
		runtime,
		kind,
		id,
		maxRestarts,
		func() Behavior {
			return Behavior{Handle: receiveExecutorInternal}
		},
	)
	if err != nil {
		return nil, err
	}
	return &Executor{actor: applicationActor}, nil
}

// Execute queues work, waits for its typed result, and runs after only once the
// result is available to the caller. Work and after are both panic-contained.
func (e *Executor) Execute[T any](ctx context.Context, label string, work func(context.Context) (T, error), after func(T, error)) (T, error) {
	var zero T
	task, err := e.Submit(ctx, label, work, after)
	if err != nil {
		return zero, err
	}
	return task.Wait(ctx)
}

// Submit queues work synchronously and returns a handle that can be awaited
// separately. This is useful for terminal cleanup that must stay queued even
// when the shutdown wait context expires.
func (e *Executor) Submit[T any](ctx context.Context, label string, work func(context.Context) (T, error), after func(T, error)) (*Task[T], error) {
	if e == nil || e.actor == nil {
		return nil, errors.New("actor executor unavailable")
	}
	if ctx == nil {
		return nil, errors.New("task context unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if work == nil {
		return nil, errors.New("actor task unavailable")
	}

	result := NewPromise[executorResultInternal[T]]()
	if err := e.actor.Send(executorTaskValueInternal[T]{
		ctx:     ctx,
		label:   label,
		work:    work,
		after:   after,
		result:  result,
		started: &sync.Once{},
	}); err != nil {
		return nil, err
	}
	return &Task[T]{result: result, actor: e.actor}, nil
}

// Wait waits for the submitted task result without changing the task lifetime.
func (t *Task[T]) Wait(ctx context.Context) (T, error) {
	var zero T
	if t == nil || t.result == nil || t.actor == nil {
		return zero, errors.New("actor task unavailable")
	}
	if ctx == nil {
		return zero, errors.New("task wait context unavailable")
	}
	completed, err := t.actor.handle.await(ctx, t.result, errors.New("actor executor stopped"))
	if err != nil {
		return zero, err
	}
	return completed.value, completed.err
}

// Stop drains the executor mailbox and waits for the actor to terminate.
func (e *Executor) Stop(ctx context.Context) error {
	if e == nil || e.actor == nil {
		return nil
	}
	return e.actor.Stop(ctx)
}

// Done closes after the executor actor terminates.
func (e *Executor) Done() <-chan struct{} {
	if e == nil || e.actor == nil {
		return nil
	}
	return e.actor.Done()
}

func receiveExecutorInternal(ctx *Context, rawMessage any) {
	task, ok := rawMessage.(executorTaskInternal)
	if !ok {
		return
	}
	task.executeInternal(ctx.Context())
}

func (t executorTaskValueInternal[T]) executeInternal(actorCtx context.Context) {
	if t.result == nil || t.started == nil {
		return
	}
	t.started.Do(func() {
		workCtx, cancel := context.WithCancel(actorCtx)
		stopTaskCancel := context.AfterFunc(t.ctx, cancel) //nolint:contextcheck // task execution combines caller cancellation with the actor lifetime.
		defer cancel()
		defer stopTaskCancel()

		var (
			value T
			err   error
		)
		func() {
			defer utils.RecoverToError(&err, t.label)
			if err = t.ctx.Err(); err == nil {
				value, err = t.work(workCtx)
			}
		}()
		t.result.Resolve(executorResultInternal[T]{value: value, err: err})

		if t.after == nil {
			return
		}
		func() {
			var afterErr error
			defer utils.RecoverToError(&afterErr, t.label+" completion")
			t.after(value, err)
		}()
	})
}
