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
