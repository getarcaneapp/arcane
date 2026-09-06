package scheduler

import "time"

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
