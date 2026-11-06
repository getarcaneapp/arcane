package timeouts

import (
	"context"
	"time"

	"kit.getarcane.app"
)

const (
	DefaultDockerAPI       = 30 * time.Second
	DefaultDockerImagePull = 10 * time.Minute
	DefaultDockerDiskUsage = 10 * time.Minute
	DefaultTrivyScan       = 15 * time.Minute
	DefaultGitOperation    = 5 * time.Minute
	DefaultHTTPClient      = 30 * time.Second
	DefaultRegistry        = 30 * time.Second
	DefaultRegistryTags    = 120 * time.Second
	DefaultProxyRequest    = 60 * time.Second
	DefaultAgentProxy      = 5 * time.Minute
	DefaultBuildTimeout    = 30 * time.Minute
	DefaultDeployWait      = 10 * time.Minute
	DefaultImageUpdateScan = 15 * time.Minute
	// DefaultActivitySlotWait bounds how long a queued activity waits for a
	// per-environment concurrency slot before failing loudly instead of
	// parking forever behind hung work.
	DefaultActivitySlotWait    = 2 * time.Minute
	DefaultAutoUpdateOperation = 15 * time.Minute
	DefaultAutoUpdateApply     = 30 * time.Minute
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
