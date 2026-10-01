package concurrency

import (
	"context"
	"crypto/rand"
	"log/slog"
	"math/big"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// StartSupervised starts a background operation and returns its cancel-and-join function.
func StartSupervised(ctx context.Context, label string, run func(context.Context) error) (func(context.Context) error, error) {
	if ctx == nil || run == nil {
		return nil, errors.New("background operation dependencies unavailable")
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		delay := 5 * time.Second
		unexpected := 0
		for runCtx.Err() == nil {
			started := time.Now()
			var runErr error
			panicked := true
			func() { defer utils.RecoverToError(&runErr, label); runErr = run(runCtx); panicked = false }()
			if runCtx.Err() != nil {
				return
			}
			if panicked || runErr == nil {
				unexpected++
			}
			if runErr == nil {
				runErr = errors.New("background operation exited unexpectedly")
			}
			if time.Since(started) > 5*time.Minute {
				delay = 5 * time.Second
				unexpected = 0
			}
			if unexpected >= 3 {
				slog.ErrorContext(runCtx, "Background operation requires attention", "label", label, "error", runErr)
				return
			}
			jitter, err := rand.Int(rand.Reader, big.NewInt(int64(delay/2)))
			if err != nil {
				slog.ErrorContext(runCtx, "Failed to schedule background retry", "label", label, "error", err)
				return
			}
			wait := delay/2 + time.Duration(jitter.Int64())
			slog.WarnContext(runCtx, "Background operation failed; retrying", "label", label, "error", runErr, "retryIn", wait)
			timer := time.NewTimer(wait)
			select {
			case <-runCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			delay = min(delay*2, 5*time.Minute)
		}
	}()
	return func(stopCtx context.Context) error {
		cancel()
		select {
		case <-done:
			return nil
		case <-stopCtx.Done():
			return stopCtx.Err()
		}
	}, nil
}
