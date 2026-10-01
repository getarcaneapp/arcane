package scheduler

import "time"

// CoordinatorState is the durable state of one environment/job actor.
type CoordinatorState struct {
	Revision            uint64
	Record              QueueRecord
	Imported            bool
	ImportHash          string
	Dispatches          map[string]DispatchIntent
	PendingActivitySync map[string]time.Time
}

type DispatchIntent struct {
	RunID   string
	Attempt int
	JobID   string
}

type ExecutionCommand struct {
	EnvironmentID string
	JobID         string
	RunID         string
	Attempt       int
}

// ExecutionClaim records whether a delivery acquired its persisted run.
type ExecutionClaim struct {
	Run       Run
	Reconcile bool
	Claimed   bool
	Deferred  bool
}

type ScheduleOccurrence struct {
	Generation uint64
	Sequence   uint64
}

type AdmissionKey struct {
	Scope string
	ID    string
}
type AdmissionState struct {
	Epoch      string
	Token      string
	AcquiredAt time.Time
}
type AdmissionCommand struct {
	Epoch string
	Token string
}

// ActivitySyncCommand acknowledges one persisted activity projection version.
type ActivitySyncCommand struct {
	RunID     string
	UpdatedAt time.Time
}

// CoordinatorImport binds imported state to the exact inactive legacy row.
type CoordinatorImport struct {
	Record     QueueRecord
	SourceHash string
}
