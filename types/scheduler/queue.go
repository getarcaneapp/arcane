package scheduler

import (
	"context"
	"time"
)

// RunObserver projects persisted runs into operator-facing activity records.
// ActivityID must be deterministic and empty for runs that should stay quiet.
type RunObserver interface {
	ActivityID(run Run) string
	SyncRunActivity(ctx context.Context, run Run) error
}

// QueueRecord holds atomic admission, claims, and checkpoints for one job and target.
type QueueRecord struct {
	JobID          string         `json:"jobId"`
	EnvironmentID  string         `json:"environmentId"`
	Schedule       string         `json:"schedule"`
	LastEnqueuedAt time.Time      `json:"lastEnqueuedAt"`
	NextRun        time.Time      `json:"nextRun"`
	Runs           []Run          `json:"runs"`
	Receipts       map[string]Run `json:"receipts,omitempty"`
}
