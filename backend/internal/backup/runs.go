package backup

import (
	"context"
	"log/slog"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
)

type backupRunInternal struct {
	executor *actors.Executor
	cancel   context.CancelFunc
}

// SubmitRun accepts one backup on an executor independent of repository work.
// after owns finalization only when submission succeeds.
func (e *Engine) SubmitRun(ctx context.Context, id string, work func(context.Context) error, after func(error)) error {
	if e == nil || e.runs == nil {
		return errors.New("backup engine is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return e.runs.Apply(ctx, "submit backup run", func(runs map[string]*backupRunInternal) (bool, error) {
		if e.stopping {
			return false, errors.New("backup engine is stopping")
		}
		if _, exists := runs[id]; exists {
			return false, errors.New("backup run already submitted")
		}
		runCtx, cancel := context.WithCancel(ctx)
		// Keep the mailbox alive long enough to finalize cancelled work.
		executor, err := actors.NewExecutor(context.WithoutCancel(runCtx), e.runtime, "backup-runs", id, 0)
		if err != nil {
			cancel()
			return false, err
		}
		_, err = executor.Submit(runCtx, "run backup", func(workerCtx context.Context) (actors.NoPayload, error) {
			return actors.NoPayload{}, work(workerCtx)
		}, func(_ actors.NoPayload, runErr error) {
			defer func() {
				cancel()
				// Queue termination without waiting on our own mailbox.
				if stopErr := executor.Stop(runCtx); stopErr != nil && !errors.Is(stopErr, context.Canceled) {
					slog.ErrorContext(context.WithoutCancel(ctx), "Failed to stop backup executor", "runId", id, "error", stopErr)
				}
				if _, _, removeErr := e.runs.Remove(context.WithoutCancel(ctx), "complete backup run", id); removeErr != nil {
					slog.ErrorContext(context.WithoutCancel(ctx), "Failed to remove completed backup run", "runId", id, "error", removeErr)
				}
			}()
			after(runErr)
		})
		if err != nil {
			cancel()
			stopErr := executor.Stop(runCtx)
			if errors.Is(stopErr, context.Canceled) {
				stopErr = nil // The stop was queued; only its wait was cancelled.
			}
			return false, errors.Combine(err, stopErr)
		}
		runs[id] = &backupRunInternal{executor: executor, cancel: cancel}
		return true, nil
	})
}

func (e *Engine) stopRunsInternal(ctx context.Context) error {
	var pending []*backupRunInternal
	if err := e.runs.Apply(ctx, "stop backup runs", func(runs map[string]*backupRunInternal) (bool, error) {
		e.stopping = true
		for _, run := range runs {
			pending = append(pending, run)
			run.cancel()
		}
		return false, nil
	}); err != nil {
		return err
	}
	var stopErr error
	for _, run := range pending {
		stopErr = errors.Combine(stopErr, run.executor.Stop(ctx))
	}
	return stopErr
}
