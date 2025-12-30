package utils

import (
	"context"
	"time"
)

// DeriveContext creates a child context based on the provided parent context `ctx`.
//   - If timeout > 0 the returned context has a deadline (WithTimeout).
//   - If timeout <= 0 the returned context is a cancellable context (WithCancel).
//   - If independent is true the returned context is created from context.Background()
//     and therefore does NOT inherit cancellation from the parent `ctx`.
//
// The returned CancelFunc must be called to release resources (or deferred).
func DeriveContext(ctx context.Context, timeout time.Duration, independent bool) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		if independent {
			return context.WithTimeout(context.Background(), timeout)
		}
		if ctx == nil {
			ctx = context.Background()
		}
		return context.WithTimeout(ctx, timeout)
	}

	if independent {
		return context.WithCancel(context.Background())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithCancel(ctx)
}

// DeriveContextOptional behaves like DeriveContext but accepts a pointer for timeout.
// If timeoutPtr == nil then no timeout is applied and a no-op CancelFunc is returned
// so callers may safely `defer cancel()` without having to guard for nil.
// If independent is true and timeoutPtr == nil the returned context will be
// context.Background() (no parent inheritance).
func DeriveContextOptional(ctx context.Context, timeoutPtr *time.Duration, independent bool) (context.Context, context.CancelFunc) {
	// No timeout requested
	if timeoutPtr == nil {
		if independent {
			// independent background context with noop cancel to allow defer cancel()
			return context.Background(), func() {}
		}
		if ctx == nil {
			ctx = context.Background()
		}
		return ctx, func() {}
	}
	return DeriveContext(ctx, *timeoutPtr, independent)
}

// TimeLeft returns the remaining time until the context's deadline. If the
// context has no deadline the second return value will be false.
func TimeLeft(ctx context.Context) (time.Duration, bool) {
	if dl, ok := ctx.Deadline(); ok {
		return time.Until(dl), true
	}
	return 0, false
}

// WithDeadlineFromNow creates a context with a deadline `d` from now.
// If independent is true the context is derived from context.Background().
func WithDeadlineFromNow(ctx context.Context, d time.Duration, independent bool) (context.Context, context.CancelFunc) {
	if independent {
		return context.WithDeadline(context.Background(), time.Now().Add(d))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithDeadline(ctx, time.Now().Add(d))
}
