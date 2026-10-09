// Package entityjobs registers one dynamic scheduler job per database row, such as a GitOps sync or a backup policy.
package entityjobs

import (
	"context"
	"fmt"
	"log/slog"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
)

// GitOpsSyncJobPrefix is shared by the GitOps registry and environment cleanup.
const GitOpsSyncJobPrefix = "gitops-sync:"

// Registry owns one job per entity ID and admits one run per entity at a time.
// Without SetScheduler, as in agent mode, every registration is a no-op.
type Registry struct {
	jobPrefix      string
	admissionScope string

	lifecycleCtx  context.Context
	scheduler     schedulertypes.DynamicScheduler
	admissionGate *runs.Admission
}

// New creates a registry whose job names are jobPrefix+entityID and whose
// admission keys are scoped to admissionScope.
func New(jobPrefix, admissionScope string) *Registry {
	return &Registry{jobPrefix: jobPrefix, admissionScope: admissionScope}
}

// SetScheduler injects the scheduler, the admission gate, and the app lifecycle context scheduled runs execute on.
//
//nolint:contextcheck // scheduled runs must capture the app lifecycle context, not request contexts
func (r *Registry) SetScheduler(ctx context.Context, scheduler schedulertypes.DynamicScheduler, admissionGate *runs.Admission) error {
	if scheduler == nil || admissionGate == nil {
		return fmt.Errorf("%s scheduler dependencies unavailable", r.admissionScope)
	}
	if ctx == nil {
		ctx = context.Background() //nolint:forbidigo // Fallback preserves the existing nil-context contract when no lifecycle was injected.
	}
	r.lifecycleCtx = ctx
	r.scheduler = scheduler
	r.admissionGate = admissionGate
	return nil
}

// Enabled reports whether a scheduler has been injected.
func (r *Registry) Enabled() bool { return r.scheduler != nil }

// Scheduler returns the injected scheduler, or nil.
func (r *Registry) Scheduler() schedulertypes.DynamicScheduler { return r.scheduler }

// Context returns the app lifecycle context, or a cancel-detached copy of ctx before one is injected.
func (r *Registry) Context(ctx context.Context) context.Context {
	if r.lifecycleCtx != nil {
		return r.lifecycleCtx
	}
	if ctx != nil {
		return context.WithoutCancel(ctx)
	}
	return context.Background() //nolint:forbidigo // Fallback preserves the existing nil-context contract when no lifecycle was injected.
}

// JobName returns the scheduler job name for entityID.
func (r *Registry) JobName(entityID string) string { return r.jobPrefix + entityID }

// Register adds (or replaces) the dynamic job for entityID.
func (r *Registry) Register(ctx context.Context, entityID string, schedule func(context.Context) string,
	run func(context.Context) (schedulertypes.Outcome, error), reconcile ...func(context.Context, schedulertypes.Run) (schedulertypes.Outcome, error),
) {
	job := &schedulertypes.GenericJob{JobName: r.JobName(entityID), ScheduleFn: schedule, RunFn: run}
	if len(reconcile) > 0 {
		job.ReconcileFn = reconcile[0]
	}
	r.Add(ctx, job)
}

// Add adds (or replaces) a prebuilt job, such as a flow.Job named with JobName.
func (r *Registry) Add(ctx context.Context, job schedulertypes.Job) {
	if r.scheduler == nil {
		return
	}
	schedulerCtx := r.Context(ctx)
	if err := r.scheduler.AddJob(schedulerCtx, job); err != nil {
		slog.ErrorContext(schedulerCtx, "failed to register scheduled job", "job", job.Name(), "error", err)
	}
}

// Unregister removes the dynamic job for entityID.
func (r *Registry) Unregister(ctx context.Context, entityID string) {
	if r.scheduler == nil {
		return
	}
	r.scheduler.RemoveJob(r.Context(ctx), r.JobName(entityID))
}

// TryAcquire admits one in-flight run per entity ID and refuses at once while one is active.
func (r *Registry) TryAcquire(ctx context.Context, entityID string) (*runs.Lease, bool, error) {
	return r.admissionGate.TryAcquire(ctx, schedulertypes.AdmissionKey{Scope: r.admissionScope, ID: entityID})
}
