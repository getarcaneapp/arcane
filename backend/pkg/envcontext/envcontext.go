// Package envcontext carries the environment a request is served for.
//
// The manager serves the local environment ("0") in-process and proxies every
// agent environment over its tunnel. A "docker-host" environment is also served
// in-process, so downstream services (most importantly the Docker client
// service) need a way to know which daemon the current request belongs to.
package envcontext

import "context"

type (
	// Target identifies the environment a request is served for.
	//
	// An empty DockerHost means "no explicit target": callers fall back to the
	// manager's own daemon, which is the behaviour for the local environment and
	// for background work that has no environment.
	Target struct {
		// EnvironmentID is the environment row the request is scoped to.
		EnvironmentID string
		// DockerHost is the Docker API endpoint for that environment.
		DockerHost string
	}

	targetKey struct{}
)

// WithTarget returns a copy of ctx carrying the environment target.
func WithTarget(ctx context.Context, target Target) context.Context {
	if target.EnvironmentID == "" && target.DockerHost == "" {
		return ctx
	}
	return context.WithValue(ctx, targetKey{}, target)
}

// TargetFromContext returns the environment target carried by ctx, if any.
func TargetFromContext(ctx context.Context) (Target, bool) {
	if ctx == nil {
		return Target{}, false
	}
	target, ok := ctx.Value(targetKey{}).(Target)
	return target, ok
}

// DockerHostFromContext returns the daemon endpoint the request is scoped to, or
// an empty string when the request has no explicit environment target.
func DockerHostFromContext(ctx context.Context) string {
	target, ok := TargetFromContext(ctx)
	if !ok {
		return ""
	}
	return target.DockerHost
}

// IsDockerHostTarget reports whether ctx is serving a docker-host environment,
// which the manager reaches directly instead of through an agent tunnel.
func IsDockerHostTarget(ctx context.Context) bool {
	return DockerHostFromContext(ctx) != ""
}

// EnvironmentIDFromContext returns the environment id the request is scoped to,
// or an empty string when the request has no explicit environment target.
func EnvironmentIDFromContext(ctx context.Context) string {
	target, ok := TargetFromContext(ctx)
	if !ok {
		return ""
	}
	return target.EnvironmentID
}

// EnvironmentIDOrDefault returns the environment a request is being served for: the target
// environment when the request carries one, and the local environment otherwise, because
// in-process work that has no target belongs to the manager's own daemon.
func EnvironmentIDOrDefault(ctx context.Context) string {
	if environmentID := EnvironmentIDFromContext(ctx); environmentID != "" {
		return environmentID
	}
	return "0"
}
