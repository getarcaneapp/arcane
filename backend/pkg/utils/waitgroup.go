package utils

import (
	"context"
	"sync"

	"emperror.dev/errors"
)

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
