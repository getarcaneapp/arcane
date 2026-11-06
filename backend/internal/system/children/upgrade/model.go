package upgrade

import (
	"database/sql/driver"
	"time"

	"getarcane.dev/arcane/v3/internal/database"
)

type (
	UpgradeJobStatus string

	UpgradeResultStatus string

	UpgradeStage string

	// The manager appears as the first entry with EnvironmentID "0".
	UpgradeJobResult struct {
		StageStartedAt  *time.Time          `json:"stageStartedAt,omitempty"`
		EnvironmentID   string              `json:"environmentId"`
		EnvironmentName string              `json:"environmentName"`
		Status          UpgradeResultStatus `json:"status"`
		FromVersion     string              `json:"fromVersion,omitempty"`
		FromDigest      string              `json:"fromDigest,omitempty"`
		ToVersion       string              `json:"toVersion,omitempty"`
		Stage           UpgradeStage        `json:"stage,omitempty"`
		Error           string              `json:"error,omitempty"`
		Logs            []string            `json:"logs,omitempty"`
		Percent         int                 `json:"percent"`
	}

	//nolint:recvcheck
	UpgradeJobResults []UpgradeJobResult

	UpgradeJob struct {
		database.BaseModel

		Error       *string    `json:"error,omitempty" gorm:"column:error"`
		CompletedAt *time.Time `json:"completedAt,omitempty" gorm:"column:completed_at"`

		Status                UpgradeJobStatus  `json:"status" gorm:"column:status"`
		UserID                string            `json:"userId" gorm:"column:user_id"`
		Username              string            `json:"username" gorm:"column:username"`
		ManagerVersionAtStart string            `json:"managerVersionAtStart" gorm:"column:manager_version_at_start"`
		ManagerDigestAtStart  string            `json:"managerDigestAtStart" gorm:"column:manager_digest_at_start"`
		ManagerTargetVersion  string            `json:"managerTargetVersion" gorm:"column:manager_target_version"`
		Results               UpgradeJobResults `json:"results,omitempty" gorm:"column:results;type:text"`
	}
)

const (
	UpgradeJobStatusPendingRestart    UpgradeJobStatus    = "pending_restart"
	UpgradeJobStatusRunning           UpgradeJobStatus    = "running"
	UpgradeJobStatusCompleted         UpgradeJobStatus    = "completed"
	UpgradeJobStatusFailed            UpgradeJobStatus    = "failed"
	UpgradeResultStatusPending        UpgradeResultStatus = "pending"
	UpgradeResultStatusUpdating       UpgradeResultStatus = "updating"
	UpgradeResultStatusUpdated        UpgradeResultStatus = "updated"
	UpgradeResultStatusTriggered      UpgradeResultStatus = "triggered"
	UpgradeResultStatusUpToDate       UpgradeResultStatus = "up_to_date"
	UpgradeResultStatusSkippedOffline UpgradeResultStatus = "skipped_offline"
	UpgradeResultStatusFailed         UpgradeResultStatus = "failed"

	UpgradeStageChecking     UpgradeStage = "checking"
	UpgradeStageStarting     UpgradeStage = "starting"
	UpgradeStagePulling      UpgradeStage = "pulling"
	UpgradeStageUpgrading    UpgradeStage = "upgrading"
	UpgradeStageReconnecting UpgradeStage = "reconnecting"
	UpgradeStageVerifying    UpgradeStage = "verifying"
)

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

func (r UpgradeJobResults) Value() (driver.Value, error) {
	return database.JSONValue(r == nil, r)
}

func (r *UpgradeJobResults) Scan(value any) error {
	return database.JSONScan(r, value)
}

func (UpgradeJob) TableName() string {
	return "environment_update_jobs"
}
