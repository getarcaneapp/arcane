package timeouts

import (
	"context"
	"time"

	"kit.getarcane.app"
)

const (
	DefaultDockerAPI       = 30 * time.Second
	DefaultDockerImagePull = 10 * time.Minute
	DefaultTrivyScan       = 15 * time.Minute
	DefaultGitOperation    = 5 * time.Minute
	DefaultHTTPClient      = 30 * time.Second
	DefaultRegistry        = 30 * time.Second
	DefaultRegistryTags    = 120 * time.Second
	DefaultProxyRequest    = 60 * time.Second
	DefaultAgentProxy      = 5 * time.Minute
	DefaultBuildTimeout    = 30 * time.Minute
	// DefaultDeployWait bounds how long compose up waits for services to become
	// healthy or complete (depends_on conditions) before the deploy fails.
	DefaultDeployWait = 10 * time.Minute
	// DefaultImageUpdateScan bounds an entire batch image update check
	// end-to-end; individual registry RPCs are separately bounded, but the
	// aggregate scan needs its own ceiling so a wedged batch cannot hold its
	// activity slot forever.
	DefaultImageUpdateScan = 15 * time.Minute
	// DefaultActivitySlotWait bounds how long a queued activity waits for a
	// per-environment concurrency slot before failing loudly instead of
	// parking forever behind hung work.
	DefaultActivitySlotWait = 2 * time.Minute
	// DefaultAutoUpdateOperation bounds each updater engine Docker mutation and
	// Compose project update. It must exceed DefaultDeployWait.
	DefaultAutoUpdateOperation = 15 * time.Minute
	// DefaultAutoUpdateApply bounds an entire auto-update (update-all) run.
	// It is a whole-run backstop on top of the per-operation bounds, keeping
	// a long batch from holding an activity slot indefinitely.
	DefaultAutoUpdateApply = 30 * time.Minute
)

func GetDuration(settingSeconds int, defaultDuration time.Duration) time.Duration {
	return kit.Ternary(settingSeconds > 0, time.Duration(settingSeconds)*time.Second, defaultDuration)
}

func WithDefault(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}
