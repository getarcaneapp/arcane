package utils

import (
	"log/slog"

	"emperror.dev/emperror"
	"emperror.dev/errors"
)

// RecoverToError converts a panic in the calling goroutine into an error and
// logs it. x/sync's errgroup does not recover worker panics — they crash the
// whole process — so every worker defers this as its first statement:
//
//	g.Go(func() (workerErr error) {
//		defer utils.RecoverToError(&workerErr, "image list worker")
//		...
//	})
//
// It must be deferred directly (not wrapped in another closure) for recover()
// to observe the panic. errPtr may be nil when the caller has no error to
// report into (e.g. a fire-and-forget goroutine); the panic is still logged.
// args are extra slog attributes appended to the log record.
func RecoverToError(errPtr *error, label string, args ...any) {
	panicErr := emperror.Recover(recover())
	if panicErr == nil {
		return
	}

	err := errors.WrapIf(panicErr, label+" panicked")
	slog.Error(label+" panicked", append([]any{"error", err}, args...)...)
	if errPtr != nil {
		*errPtr = err
	}
}
