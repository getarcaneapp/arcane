package timeouts

import (
	"context"
	"time"
)

const (
	DefaultDockerAPI       = 30 * time.Second
	DefaultDockerImagePull = 10 * time.Minute
	DefaultTrivyScan       = 15 * time.Minute
	DefaultGitOperation    = 5 * time.Minute
	DefaultHTTPClient      = 30 * time.Second
	DefaultRegistry        = 30 * time.Second
	DefaultProxyRequest    = 60 * time.Second
	DefaultBuildTimeout    = 30 * time.Minute
	// DefaultImageUpdateScan bounds an entire batch image update check
	// end-to-end; individual registry RPCs are separately bounded, but the
	// aggregate scan needs its own ceiling so a wedged batch cannot hold its
	// activity slot forever.
	DefaultImageUpdateScan = 15 * time.Minute
	// DefaultActivitySlotWait bounds how long a queued activity waits for a
	// per-environment concurrency slot before failing loudly instead of
	// parking forever behind hung work.
	DefaultActivitySlotWait = 2 * time.Minute
	// DefaultAutoUpdateApply bounds an entire auto-update (update-all) run.
	// The updater engine's per-container docker operations carry no timeouts
	// of their own, so this is the Arcane-side backstop that keeps a hung
	// engine op from holding an activity slot indefinitely.
	DefaultAutoUpdateApply = 30 * time.Minute
)

func GetDuration(settingSeconds int, defaultDuration time.Duration) time.Duration {
	if settingSeconds > 0 {
		return time.Duration(settingSeconds) * time.Second
	}
	return defaultDuration
}

func WithTimeout(ctx context.Context, settingSeconds int, defaultDuration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, GetDuration(settingSeconds, defaultDuration))
}
