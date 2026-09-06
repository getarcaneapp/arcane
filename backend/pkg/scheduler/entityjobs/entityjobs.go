// Package entityjobs holds the per-entity dynamic-job registry shared by the
// services that schedule one job per database row (GitOps syncs, environment
// health checks). It lives beside the scheduler rather than inside it because
// package scheduler imports the services that would use this registry.
package entityjobs

import (
	"context"
	"log/slog"

	"emperror.dev/errors"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
)

// GitOpsSyncJobPrefix is shared by the GitOps registry and environment cleanup.
const GitOpsSyncJobPrefix = "gitops-sync:"

// Registry owns one dynamic scheduler job per entity ID, plus the admission
// gate that keeps a single run of any one entity in flight at a time.
//
// The scheduler and the app lifecycle context arrive post-construction via
// SetScheduler: the manager wires them during bootstrap, and agent mode leaves
// them nil so every registration becomes a no-op.
type Registry struct {
	jobPrefix      string
	admissionScope string

	lifecycleCtx  context.Context
	scheduler     schedulertypes.DynamicScheduler
	admissionGate *actors.Gate[actors.AdmissionKey]
}

// New creates a registry whose job names are jobPrefix+entityID and whose
// admission keys are scoped to admissionScope.
func New(jobPrefix, admissionScope string) *Registry {
	return &Registry{jobPrefix: jobPrefix, admissionScope: admissionScope}
}

// SetScheduler injects the job scheduler, the admission gate and the app
// lifecycle context. The lifecycle context is what scheduled runs execute on,
// so they outlive the request or bootstrap goroutine that registered them.
func (r *Registry) SetScheduler(ctx context.Context, scheduler schedulertypes.DynamicScheduler, admissionGate *actors.Gate[actors.AdmissionKey]) error { //nolint:contextcheck // scheduled runs must capture the app lifecycle context, not request contexts
	if scheduler == nil || admissionGate == nil {
		return errors.Errorf("%s scheduler dependencies unavailable", r.admissionScope)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.lifecycleCtx = ctx
	r.scheduler = scheduler
	r.admissionGate = admissionGate
	return nil
}

// Enabled reports whether a scheduler has been injected.
func (r *Registry) Enabled() bool { return r.scheduler != nil }

// Scheduler exposes the injected scheduler for the rare caller that must touch
// a job this registry does not own (nil when none was injected).
func (r *Registry) Scheduler() schedulertypes.DynamicScheduler { return r.scheduler }

// Context returns the context scheduled work should run on: the app lifecycle
// context when one was injected, otherwise a cancel-detached copy of ctx.
func (r *Registry) Context(ctx context.Context) context.Context {
	if r.lifecycleCtx != nil {
		return r.lifecycleCtx
	}
	if ctx != nil {
		return context.WithoutCancel(ctx)
	}
	return context.Background()
}

// JobName returns the scheduler job name for entityID.
func (r *Registry) JobName(entityID string) string { return r.jobPrefix + entityID }

// Register adds (or replaces) the dynamic job for entityID.
func (r *Registry) Register(ctx context.Context, entityID string, schedule func(context.Context) string, run func(context.Context) (schedulertypes.Outcome, error), reconcile ...func(context.Context, schedulertypes.Run) (schedulertypes.Outcome, error)) {
	if r.scheduler == nil {
		return
	}
	schedulerCtx := r.Context(ctx)
	job := &schedulertypes.GenericJob{
		JobName:    r.JobName(entityID),
		ScheduleFn: schedule,
		RunFn:      run,
	}
	if len(reconcile) > 0 {
		job.ReconcileFn = reconcile[0]
	}
	if err := r.scheduler.AddJob(schedulerCtx, job); err != nil {
		slog.ErrorContext(schedulerCtx, "failed to register scheduled job", "job", job.JobName, "error", err)
	}
}

// Unregister removes the dynamic job for entityID.
func (r *Registry) Unregister(ctx context.Context, entityID string) {
	if r.scheduler == nil {
		return
	}
	r.scheduler.RemoveJob(r.Context(ctx), r.JobName(entityID))
}

// TryAcquire admits at most one in-flight run per entity ID. It refuses
// immediately when a run is already active.
func (r *Registry) TryAcquire(ctx context.Context, entityID string) (*actors.Lease[actors.AdmissionKey], bool, error) {
	return r.admissionGate.TryAcquire(ctx, actors.AdmissionKey{Scope: r.admissionScope, ID: entityID})
}
