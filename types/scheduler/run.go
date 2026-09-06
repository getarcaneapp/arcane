package scheduler

import (
	"context"
	"time"
)

// RunStatus describes durable execution, independently of the configured schedule.
type RunStatus string

const (
	Queued         RunStatus = "queued"
	Waiting        RunStatus = "waiting"
	Running        RunStatus = "running"
	Retrying       RunStatus = "retrying"
	Succeeded      RunStatus = "succeeded"
	Partial        RunStatus = "partial"
	Skipped        RunStatus = "skipped"
	Failed         RunStatus = "failed"
	NeedsAttention RunStatus = "needs_attention"
	Canceled       RunStatus = "canceled"
)

func (s RunStatus) Terminal() bool {
	return s == Succeeded || s == Partial || s == Skipped || s == Failed || s == Canceled
}

type TargetOutcome struct {
	ResourceType string    `json:"resourceType,omitempty"`
	ID           string    `json:"id"`
	Status       RunStatus `json:"status"`
	Message      string    `json:"message,omitempty"`
	ActivityID   string    `json:"activityId,omitempty"`
}

type Outcome struct {
	Status     RunStatus       `json:"status"`
	Message    string          `json:"message,omitempty"`
	ActivityID string          `json:"activityId,omitempty"`
	Targets    []TargetOutcome `json:"targets,omitempty"`
}

type Request struct {
	RunID            string `json:"runId,omitempty"`
	JobID            string `json:"jobId"`
	EnvironmentID    string `json:"environmentId"`
	Trigger          string `json:"trigger"`
	RequestedWithKey string `json:"requestedWithKey,omitempty"`
	RequestedBy      string `json:"requestedBy,omitempty"`
}

type Attempt struct {
	Number     int        `json:"number"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Outcome    Outcome    `json:"outcome"`
}

type Run struct {
	RequestedWithKey        string     `json:"requestedWithKey,omitempty"`
	ID                      string     `json:"id"`
	JobID                   string     `json:"jobId"`
	EnvironmentID           string     `json:"environmentId"`
	Trigger                 string     `json:"trigger"`
	RequestedBy             string     `json:"requestedBy,omitempty"`
	Status                  RunStatus  `json:"status"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
	StartedAt               *time.Time `json:"startedAt,omitempty"`
	FinishedAt              *time.Time `json:"finishedAt,omitempty"`
	NextAttempt             *time.Time `json:"nextAttempt,omitempty"`
	AttemptCount            int        `json:"attemptCount"`
	Owner                   string     `json:"owner,omitempty"`
	Outcome                 Outcome    `json:"outcome"`
	Attempts                []Attempt  `json:"attempts,omitempty"`
	RemoteDeliveryAttempted bool       `json:"remoteDeliveryAttempted"`
	RemoteOutcome           *Outcome   `json:"remoteOutcome,omitempty"`
	RemoteRetryRequested    bool       `json:"remoteRetryRequested,omitempty"`
	RemoteRetryAttempted    bool       `json:"remoteRetryAttempted,omitempty"`
	RemoteAttemptCount      int        `json:"remoteAttemptCount,omitempty"`
	RemoteAccepted          bool       `json:"remoteAccepted"`
	RemoteSettled           bool       `json:"remoteSettled"`
	LastConfirmedAt         *time.Time `json:"lastConfirmedAt,omitempty"`
}

type RunList struct {
	Runs  []Run `json:"runs"`
	Total int   `json:"total"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
}

type WorkerHealth struct {
	Status    string     `json:"status"`
	LastError string     `json:"lastError,omitempty"`
	NextRetry *time.Time `json:"nextRetry,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// Dispatcher owns durable admission and execution. Cron only submits work.
type Dispatcher interface {
	Submit(ctx context.Context, request Request) (Run, error)
	Checkpoint(ctx context.Context, jobID, schedule string, nextRun time.Time) error
}

// Reconciler consults domain state before repeating interrupted work.
type Reconciler interface {
	Reconcile(ctx context.Context, previous Run) (Outcome, error)
}

// OutcomeError carries partial results through error-only watcher contracts.
type OutcomeError struct {
	Outcome Outcome
	Cause   error
}

func (e *OutcomeError) Error() string {
	if e.Outcome.Message != "" {
		return e.Outcome.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Outcome.Status)
}

func (e *OutcomeError) Unwrap() error { return e.Cause }
