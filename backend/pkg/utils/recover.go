package utils

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	panicErr := PanicToError(recover())
	if panicErr == nil {
		return
	}

	err := fmt.Errorf("%s: %w", label+" panicked", panicErr)
	slog.Error(label+" panicked", append([]any{"error", err}, args...)...)
	if errPtr != nil {
		*errPtr = err
	}
}

// PanicToError converts a recovered panic value into an error.
func PanicToError(r any) error {
	switch value := r.(type) {
	case nil:
		return nil
	case error:
		return value
	case string:
		return errors.New(value)
	default:
		return fmt.Errorf("unknown panic, received: %v", r)
	}
}

// WaitGroup waits for all work or returns when ctx ends. After a timeout, its
// waiter goroutine remains until the group eventually completes.
func WaitGroup(ctx context.Context, group *sync.WaitGroup) error {
	if ctx == nil || group == nil {
		return errors.New("wait group context unavailable")
	}
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
