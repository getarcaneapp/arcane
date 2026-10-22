package upgrade

import (
	"database/sql/driver"
	"time"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
)

// UpgradeJobStatus is the lifecycle status of an upgrade job covering one or more
// environments.
type UpgradeJobStatus string

const (
	// UpgradeJobStatusPendingRestart means the agents are done and the manager triggered its own
	// self-upgrade; the job is finalized once it restarts on the new version.
	UpgradeJobStatusPendingRestart UpgradeJobStatus = "pending_restart"
	// UpgradeJobStatusRunning means the agents phase is in progress.
	UpgradeJobStatusRunning UpgradeJobStatus = "running"
	// UpgradeJobStatusCompleted means every environment has been processed.
	UpgradeJobStatusCompleted UpgradeJobStatus = "completed"
	// UpgradeJobStatusFailed means the job stopped before completing.
	UpgradeJobStatusFailed UpgradeJobStatus = "failed"

	// UpgradeResultStatusPending is the initial state of an environment
	// seeded into the job but not yet being processed.
	UpgradeResultStatusPending UpgradeResultStatus = "pending"
	// UpgradeResultStatusUpdating means this environment is being processed
	// right now; it drives the live per-environment progress indicator in the UI.
	UpgradeResultStatusUpdating UpgradeResultStatus = "updating"
	// UpgradeResultStatusUpdated means the upgrade was triggered and the
	// new version was confirmed.
	UpgradeResultStatusUpdated UpgradeResultStatus = "updated"
	// UpgradeResultStatusTriggered means the upgrade was triggered but not
	// confirmed within the wait window (still likely succeeding in the background).
	UpgradeResultStatusTriggered UpgradeResultStatus = "triggered"
	// UpgradeResultStatusUpToDate means the environment already ran the
	// target image, so the pull found nothing new and no container was recreated.
	UpgradeResultStatusUpToDate UpgradeResultStatus = "up_to_date"
	// UpgradeResultStatusSkippedOffline means the environment was unreachable.
	UpgradeResultStatusSkippedOffline UpgradeResultStatus = "skipped_offline"
	// UpgradeResultStatusFailed means the upgrade trigger failed.
	UpgradeResultStatusFailed UpgradeResultStatus = "failed"
)

// UpgradeResultStatus is the per-environment outcome within a job.
type UpgradeResultStatus string

// UpgradeStage is the live step an updating environment is in.
type UpgradeStage string

const (
	UpgradeStageChecking     UpgradeStage = "checking"
	UpgradeStageStarting     UpgradeStage = "starting"
	UpgradeStagePulling      UpgradeStage = "pulling"
	UpgradeStageUpgrading    UpgradeStage = "upgrading"
	UpgradeStageReconnecting UpgradeStage = "reconnecting"
	UpgradeStageVerifying    UpgradeStage = "verifying"
)

// UpgradeJobResult is the outcome for a single environment within a job.
// The manager appears as the first entry with EnvironmentID "0".
type UpgradeJobResult struct {
	EnvironmentID   string              `json:"environmentId"`
	EnvironmentName string              `json:"environmentName"`
	Status          UpgradeResultStatus `json:"status"`
	FromVersion     string              `json:"fromVersion,omitempty"`
	FromDigest      string              `json:"fromDigest,omitempty"`
	ToVersion       string              `json:"toVersion,omitempty"`
	Stage           UpgradeStage        `json:"stage,omitempty"`
	StageStartedAt  *time.Time          `json:"stageStartedAt,omitempty"`
	Percent         int                 `json:"percent"`
	Logs            []string            `json:"logs,omitempty"`
	Error           string              `json:"error,omitempty"`
}

// clearStage drops live-progress fields once the row leaves updating.
func (r UpgradeJobResult) clearStage() UpgradeJobResult {
	return UpgradeJobResult{ //exhaustruct:enforce
		EnvironmentID:   r.EnvironmentID,
		EnvironmentName: r.EnvironmentName,
		Status:          r.Status,
		FromVersion:     r.FromVersion,
		FromDigest:      r.FromDigest,
		ToVersion:       r.ToVersion,
		Stage:           "",
		StageStartedAt:  nil,
		Percent:         r.Percent,
		Logs:            r.Logs,
		Error:           r.Error,
	}
}

// UpgradeJobResults is a JSON-serialized slice of per-environment results,
// stored in a single TEXT column.
//
//nolint:recvcheck
type UpgradeJobResults []UpgradeJobResult

func (r UpgradeJobResults) Value() (driver.Value, error) {
	return database.JSONValue(r == nil, r)
}

func (r *UpgradeJobResults) Scan(value any) error {
	return database.JSONScan(r, value)
}

// UpgradeJob is a persisted upgrade job record. It survives the manager's
// self-upgrade restart so the next boot can finalize the manager result.
type UpgradeJob struct {
	database.BaseModel

	Status                UpgradeJobStatus  `json:"status" gorm:"column:status"`
	UserID                string            `json:"userId" gorm:"column:user_id"`
	Username              string            `json:"username" gorm:"column:username"`
	ManagerVersionAtStart string            `json:"managerVersionAtStart" gorm:"column:manager_version_at_start"`
	ManagerDigestAtStart  string            `json:"managerDigestAtStart" gorm:"column:manager_digest_at_start"`
	ManagerTargetVersion  string            `json:"managerTargetVersion" gorm:"column:manager_target_version"`
	Results               UpgradeJobResults `json:"results,omitempty" gorm:"column:results;type:text"`
	Error                 *string           `json:"error,omitempty" gorm:"column:error"`
	CompletedAt           *time.Time        `json:"completedAt,omitempty" gorm:"column:completed_at"`
}

func (UpgradeJob) TableName() string {
	return "environment_update_jobs"
}
