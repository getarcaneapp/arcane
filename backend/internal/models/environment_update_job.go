package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// EnvironmentUpdateJobStatus is the lifecycle status of a fleet-wide "update all
// environments" job.
type EnvironmentUpdateJobStatus string

const (
	// EnvironmentUpdateJobStatusPendingRestart means the manager has triggered its
	// own self-upgrade and is waiting to restart on the new version, after which
	// the job resumes to upgrade the remote agents.
	EnvironmentUpdateJobStatusPendingRestart EnvironmentUpdateJobStatus = "pending_restart"
	// EnvironmentUpdateJobStatusRunning means the agents phase is in progress.
	EnvironmentUpdateJobStatusRunning EnvironmentUpdateJobStatus = "running"
	// EnvironmentUpdateJobStatusCompleted means every environment has been processed.
	EnvironmentUpdateJobStatusCompleted EnvironmentUpdateJobStatus = "completed"
	// EnvironmentUpdateJobStatusFailed means the job stopped before completing.
	EnvironmentUpdateJobStatusFailed EnvironmentUpdateJobStatus = "failed"
)

// EnvironmentUpdateResultStatus is the per-environment outcome within a job.
type EnvironmentUpdateResultStatus string

const (
	// EnvironmentUpdateResultStatusPending is the initial state recorded for the
	// manager entry while its self-upgrade is in flight.
	EnvironmentUpdateResultStatusPending EnvironmentUpdateResultStatus = "pending"
	// EnvironmentUpdateResultStatusUpdated means the upgrade was triggered and the
	// new version was confirmed.
	EnvironmentUpdateResultStatusUpdated EnvironmentUpdateResultStatus = "updated"
	// EnvironmentUpdateResultStatusTriggered means the upgrade was triggered but not
	// confirmed within the wait window (still likely succeeding in the background).
	EnvironmentUpdateResultStatusTriggered EnvironmentUpdateResultStatus = "triggered"
	// EnvironmentUpdateResultStatusSkippedUpToDate means no update was available.
	EnvironmentUpdateResultStatusSkippedUpToDate EnvironmentUpdateResultStatus = "skipped_up_to_date"
	// EnvironmentUpdateResultStatusSkippedOffline means the environment was unreachable.
	EnvironmentUpdateResultStatusSkippedOffline EnvironmentUpdateResultStatus = "skipped_offline"
	// EnvironmentUpdateResultStatusFailed means the upgrade trigger failed.
	EnvironmentUpdateResultStatusFailed EnvironmentUpdateResultStatus = "failed"
)

// EnvironmentUpdateResult is the outcome for a single environment within a job.
// The manager appears as the first entry with EnvironmentID "0".
type EnvironmentUpdateResult struct {
	EnvironmentID   string                        `json:"environmentId"`
	EnvironmentName string                        `json:"environmentName"`
	Status          EnvironmentUpdateResultStatus `json:"status"`
	FromVersion     string                        `json:"fromVersion,omitempty"`
	ToVersion       string                        `json:"toVersion,omitempty"`
	Error           string                        `json:"error,omitempty"`
}

// EnvironmentUpdateResults is a JSON-serialized slice of per-environment results,
// stored in a single TEXT column.
//
//nolint:recvcheck
type EnvironmentUpdateResults []EnvironmentUpdateResult

func (r EnvironmentUpdateResults) Value() (driver.Value, error) {
	if r == nil {
		return nil, nil
	}
	return json.Marshal(r)
}

func (r *EnvironmentUpdateResults) Scan(value any) error {
	if value == nil {
		*r = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, r)
	case string:
		return json.Unmarshal([]byte(v), r)
	default:
		return fmt.Errorf("unsupported scan type for EnvironmentUpdateResults: %T", value)
	}
}

// EnvironmentUpdateJob is a persisted fleet-wide update orchestration record. It
// survives the manager's self-upgrade restart so the agents phase can resume on
// the next boot. See [EnvironmentUpdateJobStatus].
type EnvironmentUpdateJob struct {
	BaseModel

	Status                EnvironmentUpdateJobStatus `json:"status" gorm:"column:status"`
	UserID                string                     `json:"userId" gorm:"column:user_id"`
	Username              string                     `json:"username" gorm:"column:username"`
	ManagerVersionAtStart string                     `json:"managerVersionAtStart" gorm:"column:manager_version_at_start"`
	ManagerDigestAtStart  string                     `json:"managerDigestAtStart" gorm:"column:manager_digest_at_start"`
	ManagerTargetVersion  string                     `json:"managerTargetVersion" gorm:"column:manager_target_version"`
	Results               EnvironmentUpdateResults   `json:"results,omitempty" gorm:"column:results;type:text"`
	Error                 *string                    `json:"error,omitempty" gorm:"column:error"`
	CompletedAt           *time.Time                 `json:"completedAt,omitempty" gorm:"column:completed_at"`
}

func (EnvironmentUpdateJob) TableName() string {
	return "environment_update_jobs"
}
