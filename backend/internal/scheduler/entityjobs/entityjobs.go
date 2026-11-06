package entityjobs

import (
	"context"
	"log/slog"

	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/types/v3"
)

type (
	Registry struct {
		lifecycleCtx   context.Context
		scheduler      types.DynamicScheduler
		admissionGate  *runs.Admission
		jobPrefix      string
		admissionScope string
	}

	Config struct {
		Lifecycle      context.Context
		Scheduler      types.DynamicScheduler
		Admission      *runs.Admission
		JobPrefix      string
		AdmissionScope string
	}
)

const (
	GitOpsSyncJobPrefix = "gitops-sync:"
)

func New(cfg Config) *Registry {
	return &Registry{
		lifecycleCtx:   cfg.Lifecycle,
		scheduler:      cfg.Scheduler,
		admissionGate:  cfg.Admission,
		jobPrefix:      cfg.JobPrefix,
		admissionScope: cfg.AdmissionScope,
	}
}

func (r *Registry) Scheduler() types.DynamicScheduler { return r.scheduler }

func (r *Registry) Context() context.Context { return r.lifecycleCtx }

func (r *Registry) JobName(entityID string) string { return r.jobPrefix + entityID }

func (r *Registry) Register(ctx context.Context, entityID string, schedule func(context.Context) string,
	run func(context.Context) (types.SchedulerOutcome, error), reconcile ...func(context.Context, types.SchedulerRun) (types.SchedulerOutcome, error),
) {
	job := &types.GenericJob{JobName: r.JobName(entityID), ScheduleFn: schedule, RunFn: run}
	if len(reconcile) > 0 {
		job.ReconcileFn = reconcile[0]
	}
	r.Add(ctx, job)
}

func (r *Registry) Add(_ context.Context, job types.SchedulerJob) {
	if err := r.scheduler.AddJob(r.lifecycleCtx, job); err != nil {
		slog.ErrorContext(r.lifecycleCtx, "failed to register scheduled job", "job", job.Name(), "error", err)
	}
}

func (r *Registry) Unregister(_ context.Context, entityID string) {
	r.scheduler.RemoveJob(r.lifecycleCtx, r.JobName(entityID))
}

func (r *Registry) Acquire(ctx context.Context, entityID string) (*runs.Lease, error) {
	return r.admissionGate.Acquire(ctx, types.SchedulerAdmissionKey{Scope: r.admissionScope, ID: entityID})
}
