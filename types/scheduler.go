package types

import (
	"context"
	"encoding/json/jsontext"
	"time"
)

type (
	// SchedulerCoordinatorState is the durable state of one environment/job actor.
	SchedulerCoordinatorState struct {
		Dispatches          map[string]SchedulerDispatchIntent
		PendingActivitySync map[string]time.Time
		ImportHash          string
		Record              SchedulerQueueRecord
		Revision            uint64
		Imported            bool
	}

	SchedulerDispatchIntent struct {
		RunID   string
		JobID   string
		Attempt int
	}

	SchedulerExecutionCommand struct {
		EnvironmentID string
		JobID         string
		RunID         string
		Attempt       int
	}

	// SchedulerExecutionClaim records whether a delivery acquired its persisted run.
	SchedulerExecutionClaim struct {
		Run       SchedulerRun
		Reconcile bool
		Claimed   bool
		Deferred  bool
	}

	SchedulerScheduleCommand struct {
		JobID    string
		Schedule string
	}

	SchedulerAdmissionKey struct {
		Scope string
		ID    string
	}

	SchedulerAdmissionState struct {
		AcquiredAt time.Time
		Epoch      string
		Token      string
	}

	SchedulerAdmissionCommand struct {
		Epoch string
		Token string
	}

	// SchedulerActivitySyncCommand acknowledges one persisted activity projection version.
	SchedulerActivitySyncCommand struct {
		UpdatedAt time.Time
		RunID     string
	}

	// SchedulerCoordinatorImport binds imported state to the exact inactive legacy row.
	SchedulerCoordinatorImport struct {
		SourceHash string
		Record     SchedulerQueueRecord
	}

	// JobRuntimeState describes the schedule currently installed in a scheduler.
	// It intentionally exposes only read-only state needed by job-management APIs.
	JobRuntimeState struct {
		NextRun   *time.Time
		Schedule  string
		Scheduled bool
	}

	SchedulerJob interface {
		Name() string
		Schedule(ctx context.Context) string
		Run(ctx context.Context) (SchedulerOutcome, error)
	}

	// ConditionalJob allows a job to opt out of cron registration when it is disabled.
	// Jobs that do not implement this interface are always scheduled.
	ConditionalJob interface {
		ShouldSchedule(ctx context.Context) bool
	}

	// BusWatcher is a continuous event consumer owned by the application scheduler lifecycle.
	BusWatcher interface {
		Name() string
		Start(ctx context.Context) error
		RunNow(ctx context.Context) error
	}

	// StoppableBusWatcher lets a continuous watcher perform explicit shutdown work
	// before its actor runner is joined.
	StoppableBusWatcher interface {
		BusWatcher
		Stop(ctx context.Context) error
	}

	// DynamicScheduler owns jobs registered and removed while the application is running.
	DynamicScheduler interface {
		Submit(ctx context.Context, request SchedulerRequest) (SchedulerRun, error)
		AddJob(ctx context.Context, job SchedulerJob) error
		RemoveJob(ctx context.Context, name string)
		HasJob(name string) bool
	}

	// JobController exposes scheduler operations used by job-management services.
	JobController interface {
		GetJob(jobID string) (SchedulerJob, bool)
		GetJobRuntimeState(jobID string) (JobRuntimeState, bool)
		RescheduleJob(ctx context.Context, job SchedulerJob) error
		RunBusWatcherNow(ctx context.Context, watcherID string) error
	}

	// WorkerController exposes continuous-worker health and recovery operations.
	WorkerController interface {
		RestartWatcher(ctx context.Context, watcherID string) error
		WatcherHealth(watcherID string) (SchedulerWorkerHealth, bool)
	}

	// JobScheduler is the shared public contract for the actor-owned scheduler.
	// Concrete cron and actor state remains private to the backend implementation.
	JobScheduler interface {
		ListRegisteredJobs() []SchedulerJob
		WorkerController
		DynamicScheduler
		JobController

		RegisterJob(job SchedulerJob) error
		RegisterBusWatcher(watcher BusWatcher, canRunManually bool) error
		StartScheduler(ctx context.Context) error
		GetLocation() *time.Location
		Stop(ctx context.Context) error
	}

	// GenericJob is a reusable Job built from closures. It lets a service register a
	// per-entity dynamic job (e.g. one per GitOps sync or one per environment) without
	// importing the scheduler package: the service constructs a GenericJob and hands it
	// to the scheduler through the SchedulerJob interface.
	//
	// JobName must be unique per logical job; per-entity jobs use a "<subsystem>:<entityID>"
	// scheme (e.g. "gitops-sync:abc123"). ShouldRunFn is optional — when nil the job is
	// always scheduled, matching the behavior of a SchedulerJob that does not implement
	// ConditionalJob.
	GenericJob struct {
		ScheduleFn  func(ctx context.Context) string
		RunFn       func(ctx context.Context) (SchedulerOutcome, error)
		ReconcileFn func(ctx context.Context, previous SchedulerRun) (SchedulerOutcome, error)
		ShouldRunFn func(ctx context.Context) bool
		JobName     string
	}

	// RunObserver projects persisted runs into operator-facing activity records.
	// ActivityID is persisted before projection and is empty for runs that should stay quiet.
	RunObserver interface {
		ActivityID(run SchedulerRun) string
		SyncRunActivity(ctx context.Context, run SchedulerRun) error
	}

	// SchedulerQueueRecord holds atomic admission, claims, and checkpoints for one job and target.
	SchedulerQueueRecord struct {
		Receipts      map[string]SchedulerRun `json:"receipts,omitempty"`
		JobID         string                  `json:"jobId"`
		EnvironmentID string                  `json:"environmentId"`
		Schedule      string                  `json:"schedule"`
		ScheduleJobID string                  `json:"scheduleJobId,omitempty"`
		Runs          []SchedulerRun          `json:"runs"`
	}

	// RunStatus describes durable execution, independently of the configured schedule.
	RunStatus string

	TargetOutcome struct {
		ResourceType string         `json:"resourceType,omitempty"`
		ID           string         `json:"id"`
		Status       RunStatus      `json:"status"`
		Message      string         `json:"message,omitempty"`
		ActivityID   string         `json:"activityId,omitempty"`
		RecoveryData jsontext.Value `json:"-" msgpack:"recoveryData,omitempty"`
	}

	// StepOutcome is one workflow step as projected onto its run.
	StepOutcome struct {
		StartedAt  *time.Time `json:"startedAt,omitempty"`
		FinishedAt *time.Time `json:"finishedAt,omitempty"`
		Name       string     `json:"name"`
		Status     RunStatus  `json:"status"`
		Message    string     `json:"message,omitempty"`
		Total      int        `json:"total,omitempty"`
		Completed  int        `json:"completed,omitempty"`
		Failed     int        `json:"failed,omitempty"`
		Attempts   int        `json:"attempts,omitempty"`
	}

	SchedulerOutcome struct {
		Status     RunStatus       `json:"status"`
		Message    string          `json:"message,omitempty"`
		ActivityID string          `json:"activityId,omitempty"`
		Targets    []TargetOutcome `json:"targets,omitempty"`
		Steps      []StepOutcome   `json:"steps,omitempty"`
	}

	SchedulerRequest struct {
		RunID            string `json:"runId,omitempty"`
		JobID            string `json:"jobId"`
		EnvironmentID    string `json:"environmentId"`
		Trigger          string `json:"trigger"`
		RequestedWithKey string `json:"requestedWithKey,omitempty"`
		RequestedBy      string `json:"requestedBy,omitempty"`
	}

	SchedulerAttempt struct {
		StartedAt  time.Time        `json:"startedAt"`
		FinishedAt *time.Time       `json:"finishedAt,omitempty"`
		Outcome    SchedulerOutcome `json:"outcome"`
		Number     int              `json:"number"`
	}

	SchedulerRun struct {
		CreatedAt               time.Time          `json:"createdAt"`
		UpdatedAt               time.Time          `json:"updatedAt"`
		StartedAt               *time.Time         `json:"startedAt,omitempty"`
		FinishedAt              *time.Time         `json:"finishedAt,omitempty"`
		NextAttempt             *time.Time         `json:"nextAttempt,omitempty"`
		RemoteOutcome           *SchedulerOutcome  `json:"remoteOutcome,omitempty"`
		LastConfirmedAt         *time.Time         `json:"lastConfirmedAt,omitempty"`
		ActivityEnvironmentID   string             `json:"activityEnvironmentId,omitempty"`
		ActivityID              string             `json:"activityId,omitempty"`
		RequestedWithKey        string             `json:"requestedWithKey,omitempty"`
		ID                      string             `json:"id"`
		JobID                   string             `json:"jobId"`
		EnvironmentID           string             `json:"environmentId"`
		Trigger                 string             `json:"trigger"`
		RequestedBy             string             `json:"requestedBy,omitempty"`
		Status                  RunStatus          `json:"status"`
		Owner                   string             `json:"owner,omitempty"`
		Outcome                 SchedulerOutcome   `json:"outcome"`
		Attempts                []SchedulerAttempt `json:"attempts,omitempty"`
		AttemptCount            int                `json:"attemptCount"`
		RemoteAttemptCount      int                `json:"remoteAttemptCount,omitempty"`
		RemoteDeliveryAttempted bool               `json:"remoteDeliveryAttempted"`
		RemoteRetryRequested    bool               `json:"remoteRetryRequested,omitempty"`
		RemoteRetryAttempted    bool               `json:"remoteRetryAttempted,omitempty"`
		RemoteAccepted          bool               `json:"remoteAccepted"`
		RemoteSettled           bool               `json:"remoteSettled"`
	}

	SchedulerRunList struct {
		Runs  []SchedulerRun `json:"runs"`
		Total int            `json:"total"`
		Page  int            `json:"page"`
		Limit int            `json:"limit"`
	}

	SchedulerWorkerHealth struct {
		UpdatedAt time.Time  `json:"updatedAt"`
		NextRetry *time.Time `json:"nextRetry,omitempty"`
		Status    string     `json:"status"`
		LastError string     `json:"lastError,omitempty"`
	}

	// SchedulerReconciler consults domain state before repeating interrupted work.
	SchedulerReconciler interface {
		Reconcile(ctx context.Context, previous SchedulerRun) (SchedulerOutcome, error)
	}

	// SchedulerOutcomeError carries partial results through error-only watcher contracts.
	SchedulerOutcomeError struct {
		Cause   error
		Outcome SchedulerOutcome
	}

	// SchedulerRetryValidator rejects retries without safe persisted target evidence.
	SchedulerRetryValidator interface {
		ValidateRetry(ctx context.Context, run SchedulerRun) error
	}
)

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusWaiting   RunStatus = "waiting"
	RunStatusRunning   RunStatus = "running"
	RunStatusRetrying  RunStatus = "retrying"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusPartial   RunStatus = "partial"
	RunStatusSkipped   RunStatus = "skipped"
	RunStatusFailed    RunStatus = "failed"
	// RunStatusNeedsAttention preserves domain recovery evidence and legacy job outcomes.
	RunStatusNeedsAttention RunStatus = "needs_attention"
	RunStatusCanceled       RunStatus = "canceled"

	// SchedulerWorkflowTarget marks the target that records a run's workflow instance.
	SchedulerWorkflowTarget = "workflow"
)

func (g *GenericJob) Name() string { return g.JobName }

func (g *GenericJob) Schedule(ctx context.Context) string { return g.ScheduleFn(ctx) }

func (g *GenericJob) Run(ctx context.Context) (SchedulerOutcome, error) { return g.RunFn(ctx) }

// ShouldSchedule satisfies ConditionalJob. A GenericJob without a ShouldRunFn is
// always scheduled; the scheduler treats a ConditionalJob returning false as "do
// not schedule", so nil must map to true rather than a nil-func panic.
func (g *GenericJob) ShouldSchedule(ctx context.Context) bool {
	if g.ShouldRunFn == nil {
		return true
	}
	return g.ShouldRunFn(ctx)
}

func (g *GenericJob) Reconcile(ctx context.Context, previous SchedulerRun) (SchedulerOutcome, error) {
	if g.ReconcileFn != nil {
		return g.ReconcileFn(ctx, previous)
	}
	return SchedulerOutcome{Status: RunStatusFailed, Message: "The interrupted operation has no confirmed completion", Targets: previous.Outcome.Targets}, nil
}

func (s RunStatus) Terminal() bool {
	return s == RunStatusSucceeded || s == RunStatusPartial || s == RunStatusSkipped || s == RunStatusFailed || s == RunStatusCanceled
}

func (e *SchedulerOutcomeError) Error() string {
	if e.Outcome.Message != "" {
		return e.Outcome.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Outcome.Status)
}

func (e *SchedulerOutcomeError) Unwrap() error { return e.Cause }
